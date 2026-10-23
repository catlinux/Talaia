package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

// SessionCookieName is the cookie that carries the session token. The __Host-
// prefix makes the browser require Secure, Path=/ and no Domain.
const SessionCookieName = "__Host-talaia_session"

const (
	// AuthKindSession is a normal cookie session.
	AuthKindSession = "session"
	// AuthKindAPIKey is a request authenticated with a bearer API key.
	AuthKindAPIKey = "api_key"
	// apiKeyRateLimit is the requests-per-minute allowance of one API key.
	apiKeyRateLimit = 600
)

const (
	// pendingSessionTTL is how long the password-only session lives: it is only
	// good to finish the second factor.
	pendingSessionTTL = 5 * time.Minute
	// setupSessionTTL is longer for the first access: the user has to install an
	// authenticator app and scan the QR code before the first code.
	setupSessionTTL = 15 * time.Minute
	// fullSessionTTL is the absolute lifetime of a session that passed both
	// factors.
	fullSessionTTL = 7 * 24 * time.Hour
	// idleTTL is how long a session survives without activity.
	idleTTL = 12 * time.Hour
)

// Session is the authenticated session of a request.
type Session struct {
	ID       string
	User     store.User
	Stage    string
	AuthKind string
	APIKeyID string
}

// sessionKey is the type of the session context key.
type sessionKey struct{}

type sessionDeniedKey struct{}

// SessionDeps are the collaborators of the session middleware.
type SessionDeps struct {
	Store       *store.Store
	Now         func() time.Time
	IdleTimeout time.Duration
	// APIKeyLimiter bounds requests made with one API key. Nil uses 600/minute.
	APIKeyLimiter *RateLimiter
}

// Sessions reads the session cookie and puts the session in the request
// context. A request without a usable session continues anonymously, so every
// handler decides whether it needs one.
func Sessions(deps SessionDeps) func(http.Handler) http.Handler {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	idle := deps.IdleTimeout
	if idle <= 0 {
		idle = idleTTL
	}

	apiKeyLimiter := deps.APIKeyLimiter
	if apiKeyLimiter == nil {
		apiKeyLimiter = NewRateLimiterWithClock(apiKeyRateLimit, time.Minute, now)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			at := now()
			cookie, cookieErr := r.Cookie(SessionCookieName)
			if cookieErr == nil && cookie.Value != "" {
				session, denial, ok := loadSession(r, deps.Store, cookie.Value, at, idle)
				if ok {
					// The store only writes last_seen_at once a minute.
					if err := deps.Store.TouchSession(r.Context(), session.ID, at); err != nil {
						slog.Warn("touch session", "error", err)
					}
					// Refresh the cookie while the session is used: its Max-Age is
					// the idle window, the absolute limit is expires_at in the
					// database.
					http.SetCookie(w, sessionCookie(cookie.Value, session.Stage))
					next.ServeHTTP(w, r.WithContext(withSession(r.Context(), session)))
					return
				}
				// The cookie points to nothing usable: drop it so the browser
				// stops sending it.
				http.SetCookie(w, sessionCookie("", ""))
				if denial != "" {
					r = r.WithContext(context.WithValue(r.Context(), sessionDeniedKey{}, denial))
				}
			}

			if !apiKeyAuthPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			token, hasBearer := bearerToken(r)
			if !hasBearer {
				next.ServeHTTP(w, r)
				return
			}

			keyID, secret, wellFormed := splitAPIKey(token)
			if !wellFormed {
				auditAPIKeyDenied(r, deps.Store, "", "invalid_format")
				writeError(w, http.StatusUnauthorized, "invalid_api_key")
				return
			}
			// The per-IP bound comes first: a per-key bound alone would let anyone
			// who knows a key id lock the real owner out with bad secrets.
			if !apiKeyLimiter.Allow("api-key-ip:" + clientIP(r)) {
				auditAPIKeyDenied(r, deps.Store, keyID, "rate_limited")
				writeError(w, http.StatusTooManyRequests, "rate_limited")
				return
			}
			session, reason, err := loadAPIKeySession(r.Context(), deps.Store, keyID, secret, at)
			if err != nil || reason != "ok" {
				slog.Error("authenticate api key", "key_id", keyID, "reason", reason, "error", err)
				auditAPIKeyDenied(r, deps.Store, keyID, reason)
				writeError(w, http.StatusUnauthorized, "invalid_api_key")
				return
			}
			if !apiKeyLimiter.Allow("api-key:" + keyID) {
				auditAPIKeyDenied(r, deps.Store, keyID, "rate_limited")
				writeError(w, http.StatusTooManyRequests, "rate_limited")
				return
			}
			next.ServeHTTP(w, r.WithContext(withSession(r.Context(), session)))
		})
	}
}

// apiKeyAuthPath reports whether a path may be authenticated with a bearer key.
// Authentication endpoints never accept keys.
func apiKeyAuthPath(path string) bool {
	if path != "/api/v1" && !strings.HasPrefix(path, "/api/v1/") {
		return false
	}
	return path != "/api/v1/auth" && !strings.HasPrefix(path, "/api/v1/auth/")
}

func bearerToken(r *http.Request) (string, bool) {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.EqualFold(value, "Bearer") {
		return "", true
	}
	if len(value) < len("Bearer ") || !strings.EqualFold(value[:len("Bearer ")], "Bearer ") {
		return "", false
	}
	return strings.TrimSpace(value[len("Bearer "):]), true
}

// splitAPIKey parses the public key id and the secret of a tlk_ token.
func splitAPIKey(token string) (string, string, bool) {
	parts := strings.Split(token, "_")
	if len(parts) != 3 || parts[0] != "tlk" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func loadAPIKeySession(ctx context.Context, st *store.Store, keyID, secret string, at time.Time) (Session, string, error) {
	if st == nil {
		return Session{}, "missing_store", errors.New("no store")
	}
	key, err := st.LookupAPIKey(ctx, keyID)
	if errors.Is(err, store.ErrNotFound) {
		return Session{}, "unknown_key", nil
	}
	if err != nil {
		return Session{}, "lookup_failed", err
	}
	if !key.RevokedAt.IsZero() {
		return Session{}, "revoked", nil
	}
	if !key.ExpiresAt.After(at) {
		return Session{}, "expired", nil
	}
	hash := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(hash[:], key.SecretHash) != 1 {
		return Session{}, "bad_secret", nil
	}
	owner, err := st.GetUser(ctx, key.OwnerID)
	if errors.Is(err, store.ErrNotFound) {
		return Session{}, "owner_missing", nil
	}
	if err != nil {
		return Session{}, "owner_lookup_failed", err
	}
	if !owner.DisabledAt.IsZero() {
		return Session{}, "owner_disabled", nil
	}
	if key.LastUsedAt.IsZero() || at.Sub(key.LastUsedAt) >= time.Minute {
		if err := st.TouchAPIKey(ctx, key.KeyID, at); err != nil {
			slog.Warn("touch api key", "key_id", key.KeyID, "error", err)
		}
	}
	return Session{
		User: owner, Stage: store.StageFull, AuthKind: AuthKindAPIKey, APIKeyID: key.ID,
	}, "ok", nil
}

func auditAPIKeyDenied(r *http.Request, st *store.Store, keyID, reason string) {
	if st == nil {
		return
	}
	if err := st.AppendAudit(r.Context(), store.AuditEvent{
		Action: "apikey_use_denied", IP: clientIP(r), Result: "denied",
		Details: map[string]any{"api_key_id": keyID, "reason": reason},
	}); err != nil {
		slog.Error("audit api key denial", "error", err)
	}
}

// loadSession returns the live session of a token, or false when it does not
// exist, was revoked, has expired or has been idle for too long.
func loadSession(r *http.Request, st *store.Store, token string, at time.Time, idle time.Duration) (Session, string, bool) {
	session, err := st.GetSessionByTokenHash(r.Context(), auth.HashToken(token))
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("load session", "error", err)
			return Session{}, "", false
		}
		revoked, revokedErr := st.GetRevokedSessionByTokenHash(r.Context(), auth.HashToken(token))
		if revokedErr != nil {
			if !errors.Is(revokedErr, store.ErrNotFound) {
				slog.Error("load revoked session", "error", revokedErr)
			}
			return Session{}, "", false
		}
		if at.Sub(revoked.LastSeenAt) > idle {
			return Session{}, "", false
		}
		user, userErr := st.GetUser(r.Context(), revoked.UserID)
		if userErr != nil {
			if !errors.Is(userErr, store.ErrNotFound) {
				slog.Error("load revoked session user", "user_id", revoked.UserID, "error", userErr)
			}
			return Session{}, "", false
		}
		if !user.DisabledAt.IsZero() {
			return Session{}, "account_disabled", false
		}
		return Session{}, "session_revoked", false
	}
	if at.Sub(session.LastSeenAt) > idle {
		return Session{}, "", false
	}

	user, err := st.GetUser(r.Context(), session.UserID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("load the session user", "user_id", session.UserID, "error", err)
		}
		return Session{}, "", false
	}
	if !user.DisabledAt.IsZero() {
		return Session{}, "account_disabled", false
	}

	return Session{ID: session.ID, User: user, Stage: session.Stage, AuthKind: AuthKindSession}, "", true
}

func sessionDeniedCode(ctx context.Context) string {
	if code, ok := ctx.Value(sessionDeniedKey{}).(string); ok {
		return code
	}
	return "session_required"
}

func withSession(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}

// sessionFromContext returns the session of a request, if it has one.
func sessionFromContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(sessionKey{}).(Session)
	return session, ok
}

// RequireFull lets a request through only when its session passed both factors.
func RequireFull(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := sessionFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, sessionDeniedCode(r.Context()))
			return
		}
		if session.Stage != store.StageFull {
			writeError(w, http.StatusUnauthorized, "mfa_required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sessionCookie builds the session cookie. An empty token clears it.
func sessionCookie(token, stage string) *http.Cookie {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Path:     "/",
		HttpOnly: true,
		// Secure is mandatory for the __Host- prefix, and localhost counts as a
		// secure context, so development over http keeps working.
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}

	switch {
	case token == "":
		cookie.MaxAge = -1
	case stage == store.StageFull:
		cookie.MaxAge = int(fullSessionTTL / time.Second)
	default:
		// The cookie may outlive the session; the server decides when it ends.
		cookie.MaxAge = int(setupSessionTTL / time.Second)
	}
	if token != "" {
		cookie.Value = token
	}
	return cookie
}
