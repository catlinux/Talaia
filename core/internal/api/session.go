package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

// SessionCookieName is the cookie that carries the session token. The __Host-
// prefix makes the browser require Secure, Path=/ and no Domain.
const SessionCookieName = "__Host-talaia_session"

const (
	// pendingSessionTTL is how long the password-only session lives: it is only
	// good to finish the second factor.
	pendingSessionTTL = 5 * time.Minute
	// fullSessionTTL is the absolute lifetime of a session that passed both
	// factors.
	fullSessionTTL = 7 * 24 * time.Hour
	// idleTTL is how long a session survives without activity.
	idleTTL = 12 * time.Hour
)

// Session is the authenticated session of a request.
type Session struct {
	ID    string
	User  store.User
	Stage string
}

// sessionKey is the type of the session context key.
type sessionKey struct{}

// SessionDeps are the collaborators of the session middleware.
type SessionDeps struct {
	Store       *store.Store
	Now         func() time.Time
	IdleTimeout time.Duration
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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)
				return
			}

			at := now()
			session, ok := loadSession(r, deps.Store, cookie.Value, at, idle)
			if !ok {
				// The cookie points to nothing usable: drop it so the browser
				// stops sending it.
				http.SetCookie(w, sessionCookie("", ""))
				next.ServeHTTP(w, r)
				return
			}

			// The store only writes last_seen_at once a minute.
			if err := deps.Store.TouchSession(r.Context(), session.ID, at); err != nil {
				slog.Warn("touch session", "error", err)
			}
			// Refresh the cookie while the session is used: its Max-Age is the
			// idle window, the absolute limit is expires_at in the database.
			http.SetCookie(w, sessionCookie(cookie.Value, session.Stage))

			next.ServeHTTP(w, r.WithContext(withSession(r.Context(), session)))
		})
	}
}

// loadSession returns the live session of a token, or false when it does not
// exist, was revoked, has expired or has been idle for too long.
func loadSession(r *http.Request, st *store.Store, token string, at time.Time, idle time.Duration) (Session, bool) {
	session, err := st.GetSessionByTokenHash(r.Context(), auth.HashToken(token))
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("load session", "error", err)
		}
		return Session{}, false
	}
	if at.Sub(session.LastSeenAt) > idle {
		return Session{}, false
	}

	user, err := st.GetUser(r.Context(), session.UserID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("load the session user", "user_id", session.UserID, "error", err)
		}
		return Session{}, false
	}

	return Session{ID: session.ID, User: user, Stage: session.Stage}, true
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
			writeError(w, http.StatusUnauthorized, "session_required")
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
		cookie.MaxAge = int(pendingSessionTTL / time.Second)
	}
	if token != "" {
		cookie.Value = token
	}
	return cookie
}
