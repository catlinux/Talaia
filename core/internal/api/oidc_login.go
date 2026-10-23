package api

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	coreoidc "github.com/catlinux/Talaia/core/internal/oidc"
	"github.com/catlinux/Talaia/core/internal/store"
)

const (
	// OIDCCookieName holds the sealed state, nonce and PKCE verifier of one
	// authorization attempt.
	OIDCCookieName = "__Host-talaia_oidc"
	// oidcStateTTL bounds a started login flow.
	oidcStateTTL = 10 * time.Minute
	// oidcFailureURL is the panel login page with its deliberately generic error.
	oidcFailureURL = "/login?oidc_error=oidc_failed"
)

type oidcLoginConfigResponse struct {
	Enabled bool   `json:"enabled"`
	Label   string `json:"label"`
}

type oidcFlowState struct {
	State     string `json:"state"`
	Nonce     string `json:"nonce"`
	Verifier  string `json:"verifier"`
	ExpiresAt int64  `json:"expires_at"`
}

func oidcStateCookie(value string) *http.Cookie {
	cookie := &http.Cookie{
		Name:     OIDCCookieName,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		// The callback is a top-level cross-site navigation from the provider.
		SameSite: http.SameSiteLaxMode,
	}
	if value == "" {
		cookie.MaxAge = -1
		return cookie
	}
	cookie.Value = value
	cookie.MaxAge = int(oidcStateTTL / time.Second)
	return cookie
}

// oidcConfig tells the login page whether to show the provider and under which
// button label. It deliberately exposes no issuer or client id.
func (h *authHandler) oidcConfig(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	if h.oidc == nil || h.oidc.deps.Store == nil {
		internal(w, "read oidc login config", errors.New("nil oidc store"))
		return
	}
	config, err := h.oidc.deps.Store.GetOIDCConfig(r.Context())
	if err != nil {
		internal(w, "read oidc login config", err)
		return
	}
	writeJSON(w, http.StatusOK, oidcLoginConfigResponse{
		Enabled: config.Enabled,
		Label:   config.ButtonLabel,
	})
}

// oidcStart begins the authorization code flow. The state, nonce and PKCE
// verifier are sealed in a short-lived cookie so the callback can trust them.
func (h *authHandler) oidcStart(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	provider, config, err := h.oidcProvider(r)
	if err != nil {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}
	if !config.Enabled {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if provider == nil {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}

	state, err := coreoidc.NewState()
	if err != nil {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}
	nonce, err := coreoidc.NewNonce()
	if err != nil {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}
	verifier, err := coreoidc.NewPKCEVerifier()
	if err != nil {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}
	authURL := provider.AuthURL(state, nonce, verifier)
	if authURL == "" {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}

	payload, err := json.Marshal(oidcFlowState{
		State: state, Nonce: nonce, Verifier: verifier,
		ExpiresAt: h.now().Add(oidcStateTTL).Unix(),
	})
	if err != nil {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}
	if h.deps.Box == nil {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}
	sealed := h.deps.Box.Seal(payload)
	http.SetCookie(w, oidcStateCookie(base64.RawURLEncoding.EncodeToString(sealed)))
	http.Redirect(w, r, authURL, http.StatusFound)
}

// oidcCallback completes the flow and leaves exactly the same pending session
// as a correct password, so the normal second factor still follows.
func (h *authHandler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}

	cookie, cookieErr := r.Cookie(OIDCCookieName)
	http.SetCookie(w, oidcStateCookie(""))
	if cookieErr != nil || cookie.Value == "" {
		h.oidcFailure(w, r, "state_mismatch", nil, nil)
		return
	}
	flow, ok := h.openOIDCState(cookie.Value)
	if !ok {
		h.oidcFailure(w, r, "state_mismatch", nil, nil)
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(flow.State)) != 1 {
		h.oidcFailure(w, r, "state_mismatch", nil, nil)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}

	provider, config, err := h.oidcProvider(r)
	if err != nil || !config.Enabled || provider == nil {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}
	claims, err := provider.Exchange(r.Context(), code, flow.Verifier, flow.Nonce)
	if err != nil {
		h.oidcFailure(w, r, "exchange_failed", nil, nil)
		return
	}
	if !claims.EmailVerified {
		h.oidcFailure(w, r, "email_unverified", &claims, nil)
		return
	}
	if strings.TrimSpace(claims.Subject) == "" {
		h.oidcFailure(w, r, "exchange_failed", &claims, nil)
		return
	}

	user, reason, err := h.findOIDCUser(r.Context(), claims, h.now())
	if err != nil {
		h.oidcFailure(w, r, "exchange_failed", &claims, nil)
		return
	}
	if reason != "" {
		h.oidcFailure(w, r, reason, &claims, &user)
		return
	}

	if err := h.deps.Store.ResetFailures(r.Context(), user.ID); err != nil {
		slog.Warn("reset failures after oidc login", "user_id", user.ID, "error", err)
	}
	ttl := pendingSessionTTL
	if user.TOTPEnabledAt.IsZero() {
		ttl = setupSessionTTL
	}
	token := auth.NewSessionToken()
	if _, err := h.deps.Store.CreateSession(r.Context(), user.ID, auth.HashToken(token),
		store.StageMFAPending, h.now().Add(ttl), clientIP(r), r.UserAgent()); err != nil {
		h.oidcFailure(w, r, "exchange_failed", &claims, &user)
		return
	}
	http.SetCookie(w, sessionCookie(token, store.StageMFAPending))
	h.audit(r, user.Username, "oidc_login", "success", oidcAuditClaims(claims))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (h *authHandler) oidcProvider(r *http.Request) (*coreoidc.Provider, store.OIDCConfig, error) {
	if h.oidc == nil || h.oidc.deps.Store == nil {
		return nil, store.OIDCConfig{}, errors.New("nil oidc store")
	}
	config, err := h.oidc.deps.Store.GetOIDCConfig(r.Context())
	if err != nil {
		return nil, store.OIDCConfig{}, err
	}
	if !config.Enabled {
		return nil, config, nil
	}
	provider, err := h.oidc.storedProvider(r.Context(), config, h.oidc.redirectURL(r))
	if err != nil {
		return nil, config, err
	}
	return provider, config, nil
}

func (h *authHandler) openOIDCState(value string) (oidcFlowState, bool) {
	if h.deps.Box == nil {
		return oidcFlowState{}, false
	}
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return oidcFlowState{}, false
	}
	raw, err := h.deps.Box.Open(sealed)
	if err != nil {
		return oidcFlowState{}, false
	}
	var flow oidcFlowState
	if err := json.Unmarshal(raw, &flow); err != nil {
		return oidcFlowState{}, false
	}
	if flow.State == "" || flow.Nonce == "" || flow.Verifier == "" ||
		flow.ExpiresAt <= h.now().Unix() {
		return oidcFlowState{}, false
	}
	return flow, true
}

func (h *authHandler) findOIDCUser(ctx context.Context, claims coreoidc.Claims, now time.Time) (store.User, string, error) {
	subject := strings.TrimSpace(claims.Subject)
	user, err := h.deps.Store.GetUserByOIDCSubject(ctx, subject)
	if err == nil {
		if !user.DisabledAt.IsZero() {
			return user, "disabled", nil
		}
		if user.LockedUntil.After(now) {
			return user, "locked", nil
		}
		return user, "", nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.User{}, "", err
	}

	email := strings.TrimSpace(claims.Email)
	if email == "" {
		return store.User{}, "user_not_found", nil
	}
	user, err = h.deps.Store.GetUserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, "user_not_found", nil
	}
	if err != nil {
		return store.User{}, "", err
	}
	if !user.DisabledAt.IsZero() {
		return user, "disabled", nil
	}
	if user.LockedUntil.After(now) {
		return user, "locked", nil
	}
	if user.OIDCSubject != "" {
		if user.OIDCSubject == subject {
			return user, "", nil
		}
		return user, "subject_mismatch", nil
	}
	if err := h.deps.Store.SetOIDCSubject(ctx, user.ID, subject); err != nil {
		if errors.Is(err, store.ErrOIDCSubjectExists) {
			return user, "subject_mismatch", nil
		}
		return store.User{}, "", err
	}
	return user, "", nil
}

func oidcAuditClaims(claims coreoidc.Claims) map[string]any {
	details := map[string]any{}
	if claims.Subject != "" {
		details["subject"] = claims.Subject
	}
	if claims.Email != "" {
		details["email"] = claims.Email
	}
	return details
}

func (h *authHandler) oidcFailure(w http.ResponseWriter, r *http.Request, reason string, claims *coreoidc.Claims, user *store.User) {
	details := map[string]any{"reason": reason}
	if claims != nil {
		for key, value := range oidcAuditClaims(*claims) {
			details[key] = value
		}
	}
	actor := ""
	if user != nil {
		actor = user.Username
	}
	if h.deps.Store != nil {
		h.audit(r, actor, "oidc_login_denied", "failure", details)
	}
	http.Redirect(w, r, oidcFailureURL, http.StatusFound)
}
