package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

const (
	// recoveryCodeCount is how many single-use codes a user receives.
	recoveryCodeCount = 10
	// authRateLimit and authRateWindow bound the login and TOTP attempts of one
	// address, as the design asks.
	authRateLimit  = 10
	authRateWindow = 15 * time.Minute
	// maxFailures is how many wrong passwords or codes lock the user.
	maxFailures = 5
	// defaultLockDuration is how long that lock lasts.
	defaultLockDuration = 15 * time.Minute
	// maxMFAFailures is how many wrong codes revoke a pending session.
	maxMFAFailures = 5
)

// errInvalidCode marks a code, or a replay of one, that does not count.
var errInvalidCode = errors.New("invalid code")

// AuthDeps are the collaborators of the authentication endpoints.
type AuthDeps struct {
	Store   *store.Store
	Box     *auth.SecretBox
	Limiter *RateLimiter
	Now     func() time.Time
	// LockDuration is how long a user stays locked after the last failure.
	LockDuration time.Duration
	// DeviceMFAHours is how long a trusted device avoids the second factor.
	// Zero means the default of 12 hours.
	DeviceMFAHours int
	// PasskeyRPID is the WebAuthn relying-party ID. Empty disables passkeys.
	PasskeyRPID string
	// PasskeyOrigins are the browser origins allowed for WebAuthn ceremonies.
	PasskeyOrigins []string
}

// NewAuthRateLimiter returns the limiter shared by the login and TOTP
// endpoints: ten attempts per address and fifteen minutes.
func NewAuthRateLimiter() *RateLimiter {
	return NewRateLimiterWithClock(authRateLimit, authRateWindow, time.Now)
}

// NewAuthHandler returns the handler of the authentication API. It is mounted
// under /api/v1/auth.
func NewAuthHandler(deps AuthDeps) http.Handler {
	return newAuthHandler(deps).routes()
}

// newAuthHandler builds the handler; the router keeps the same value to share
// the TOTP replay guard and the rate limiter with the host terminal endpoint.
func newAuthHandler(deps AuthDeps) *authHandler {
	if deps.Limiter == nil {
		deps.Limiter = NewAuthRateLimiter()
	}
	return &authHandler{deps: deps, passkeys: newPasskeyWebAuthn(deps)}
}

func (h *authHandler) routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/login", h.login)
	r.Post("/totp/setup", h.totpSetup)
	r.Post("/totp/confirm", h.totpConfirm)
	r.Post("/totp", h.totpVerify)
	r.Get("/session", h.session)
	r.Post("/logout", h.logout)
	r.Post("/password", h.password)
	r.Post("/pin/login", h.pinLogin)
	r.Post("/passkey/login/begin", h.passkeyLoginBegin)
	r.Post("/passkey/login/finish", h.passkeyLoginFinish)
	r.Group(func(r chi.Router) {
		r.Use(RequireFull)
		r.Post("/devices", h.createDevice)
		r.Get("/devices", h.listDevices)
		r.Delete("/devices/{id}", h.deleteDevice)
		r.Post("/passkeys/register/begin", h.passkeyRegisterBegin)
		r.Post("/passkeys/register/finish", h.passkeyRegisterFinish)
		r.Get("/passkeys", h.listPasskeys)
		r.Delete("/passkeys/{id}", h.deletePasskey)
	})
	return r
}

type authHandler struct {
	deps     AuthDeps
	passkeys *webauthn.WebAuthn
}

// now returns the clock of the handler.
func (h *authHandler) now() time.Time {
	if h.deps.Now != nil {
		return h.deps.Now()
	}
	return time.Now()
}

// lockDuration returns how long a lock lasts.
func (h *authHandler) lockDuration() time.Duration {
	if h.deps.LockDuration > 0 {
		return h.deps.LockDuration
	}
	return defaultLockDuration
}

// limit applies the per-address rate limit shared by login and the TOTP
// endpoints.
func (h *authHandler) limit(w http.ResponseWriter, r *http.Request) bool {
	if h.deps.Limiter != nil && !h.deps.Limiter.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	return true
}

// audit appends an audit event. A failure to write it is logged and never
// reported to the client.
func (h *authHandler) audit(r *http.Request, actor, action, result string, details map[string]any) {
	event := store.AuditEvent{
		Actor:   actor,
		Action:  action,
		IP:      clientIP(r),
		Result:  result,
		Details: details,
	}
	if err := h.deps.Store.AppendAudit(r.Context(), event); err != nil {
		slog.Error("append audit event", "action", action, "error", err)
	}
}

// internal answers a storage failure. The client never learns the detail.
func internal(w http.ResponseWriter, action string, err error) {
	slog.Error(action, "error", err)
	writeError(w, http.StatusInternalServerError, "internal")
}

// requirePending returns the session of a request that must be waiting for the
// second factor.
func requirePending(w http.ResponseWriter, r *http.Request) (Session, bool) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return Session{}, false
	}
	if session.Stage != store.StageMFAPending {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return Session{}, false
	}
	return session, true
}

// relaySecret opens the sealed TOTP secret of a user.
func (h *authHandler) relaySecret(user store.User) ([]byte, error) {
	if len(user.TOTPSecretEnc) == 0 {
		return nil, errors.New("the user has no totp secret")
	}
	return h.deps.Box.Open(user.TOTPSecretEnc)
}

// verifyCode checks a TOTP code and advances the replay guard, so the same code
// cannot be used twice.
func (h *authHandler) verifyCode(ctx context.Context, user store.User, code string) error {
	if user.TOTPEnabledAt.IsZero() {
		return errors.New("totp is not enabled")
	}
	secret, err := h.relaySecret(user)
	if err != nil {
		return err
	}

	step, ok := auth.Verify(secret, code, h.now(), user.TOTPLastStep)
	if !ok {
		return errInvalidCode
	}
	advanced, err := h.deps.Store.UpdateTOTPStep(ctx, user.ID, step)
	if err != nil {
		return err
	}
	if !advanced {
		return errInvalidCode
	}
	return nil
}

// VerifyCode verifies a TOTP code with the same replay guard as login and
// password changes. It implements TOTPVerifier.
func (h *authHandler) VerifyCode(ctx context.Context, user store.User, code string) error {
	return h.verifyCode(ctx, user, code)
}

// upgrade replaces the session with a full one and returns the new token.
func (h *authHandler) upgrade(r *http.Request, session Session) (string, error) {
	token := auth.NewSessionToken()
	_, err := h.deps.Store.UpgradeSession(r.Context(), session.ID, auth.HashToken(token),
		store.StageFull, h.now().Add(fullSessionTTL))
	if err != nil {
		return "", err
	}
	return token, nil
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Next string `json:"next"`
}

func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}

	var request loginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	username := strings.TrimSpace(request.Username)
	if username == "" || request.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, err := h.deps.Store.GetUserByUsername(r.Context(), username)
	if errors.Is(err, store.ErrNotFound) {
		// Spend the time of a real verification: the answer must not reveal
		// whether the user exists.
		auth.DummyVerify(request.Password)
		h.audit(r, username, "login_failure", "failure", map[string]any{"reason": "unknown_user"})
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if err != nil {
		internal(w, "get user for login", err)
		return
	}

	// A locked user is verified too, so the timing is the same.
	valid, err := auth.VerifyPassword(user.PasswordHash, request.Password)
	if err != nil {
		internal(w, "verify password", err)
		return
	}

	now := h.now()
	locked := user.LockedUntil.After(now)
	if !valid && !locked {
		// Failures inside a lock are not counted, so an attacker cannot keep the
		// administrator locked out forever.
		if err := h.deps.Store.RecordFailure(r.Context(), user.ID, maxFailures, h.lockDuration()); err != nil {
			slog.Warn("record failure", "user_id", user.ID, "error", err)
		}
	}
	if !valid || locked {
		h.audit(r, user.Username, "login_failure", "failure", nil)
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	if err := h.deps.Store.ResetFailures(r.Context(), user.ID); err != nil {
		slog.Warn("reset failures", "user_id", user.ID, "error", err)
	}

	next, ttl := "totp_setup", setupSessionTTL
	if !user.TOTPEnabledAt.IsZero() {
		next, ttl = "totp", pendingSessionTTL
	}

	token := auth.NewSessionToken()
	if _, err := h.deps.Store.CreateSession(r.Context(), user.ID, auth.HashToken(token),
		store.StageMFAPending, now.Add(ttl), clientIP(r), r.UserAgent()); err != nil {
		internal(w, "create session", err)
		return
	}

	http.SetCookie(w, sessionCookie(token, store.StageMFAPending))
	writeJSON(w, http.StatusOK, loginResponse{Next: next})
}

type totpSetupResponse struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

func (h *authHandler) totpSetup(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	session, ok := requirePending(w, r)
	if !ok {
		return
	}
	if !session.User.TOTPEnabledAt.IsZero() {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	secret := auth.GenerateSecret()
	if err := h.deps.Store.SetTOTP(r.Context(), session.User.ID, h.deps.Box.Seal(secret)); err != nil {
		internal(w, "store totp secret", err)
		return
	}

	encoded := auth.EncodeBase32(secret)
	writeJSON(w, http.StatusOK, totpSetupResponse{
		Secret: encoded,
		URI:    auth.ProvisioningURI(auth.Issuer, session.User.Username, encoded),
	})
}

type totpRequest struct {
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func (h *authHandler) totpConfirm(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	session, ok := requirePending(w, r)
	if !ok {
		return
	}

	var request totpRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	// Read the user again: the setup request has just stored the secret.
	user, err := h.deps.Store.GetUser(r.Context(), session.User.ID)
	if err != nil {
		internal(w, "get user for totp confirm", err)
		return
	}
	if !user.TOTPEnabledAt.IsZero() {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	secret, err := h.relaySecret(user)
	if err != nil {
		slog.Error("open totp secret", "user_id", user.ID, "error", err)
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	step, matched := auth.Verify(secret, request.Code, h.now(), user.TOTPLastStep)
	if !matched {
		h.failMFA(w, r, session, user, "totp_setup")
		return
	}

	if err := h.deps.Store.EnableTOTP(r.Context(), user.ID, step); err != nil {
		internal(w, "enable totp", err)
		return
	}

	codes := auth.NewRecoveryCodes(recoveryCodeCount)
	hashes := make([][]byte, len(codes))
	for i, code := range codes {
		hashes[i] = auth.HashToken(auth.NormalizeRecoveryCode(code))
	}
	if err := h.deps.Store.ReplaceRecoveryCodes(r.Context(), user.ID, hashes); err != nil {
		internal(w, "store recovery codes", err)
		return
	}

	token, err := h.upgrade(r, session)
	if err != nil {
		internal(w, "upgrade session", err)
		return
	}

	h.audit(r, user.Username, "mfa_enabled", "success", nil)
	h.audit(r, user.Username, "login_success", "success", map[string]any{"via": "totp_setup"})
	h.setBrowserMarker(r, w, user.ID)
	http.SetCookie(w, sessionCookie(token, store.StageFull))
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}

func (h *authHandler) totpVerify(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	session, ok := requirePending(w, r)
	if !ok {
		return
	}

	var request totpRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, err := h.deps.Store.GetUser(r.Context(), session.User.ID)
	if err != nil {
		internal(w, "get user for totp", err)
		return
	}

	if request.RecoveryCode != "" {
		h.verifyRecoveryCode(w, r, session, user, request.RecoveryCode)
		return
	}

	if err := h.verifyCode(r.Context(), user, request.Code); err != nil {
		if errors.Is(err, errInvalidCode) {
			h.failMFA(w, r, session, user, "totp")
			return
		}
		slog.Error("verify totp code", "user_id", user.ID, "error", err)
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	h.completeMFA(w, r, session, user, "totp")
}

// verifyRecoveryCode consumes a recovery code and completes the second factor.
func (h *authHandler) verifyRecoveryCode(w http.ResponseWriter, r *http.Request, session Session, user store.User, code string) {
	hash := auth.HashToken(auth.NormalizeRecoveryCode(code))
	used, err := h.deps.Store.UseRecoveryCode(r.Context(), user.ID, hash)
	if err != nil {
		internal(w, "use recovery code", err)
		return
	}
	if !used {
		h.failMFA(w, r, session, user, "recovery_code")
		return
	}

	h.audit(r, user.Username, "recovery_code_used", "success", nil)
	h.completeMFA(w, r, session, user, "recovery_code")
}

// completeMFA upgrades the session to full and answers.
func (h *authHandler) completeMFA(w http.ResponseWriter, r *http.Request, session Session, user store.User, via string) {
	if err := h.deps.Store.ResetFailures(r.Context(), user.ID); err != nil {
		slog.Warn("reset failures", "user_id", user.ID, "error", err)
	}

	token, err := h.upgrade(r, session)
	if err != nil {
		internal(w, "upgrade session", err)
		return
	}

	h.audit(r, user.Username, "login_success", "success", map[string]any{"via": via})
	h.setBrowserMarker(r, w, user.ID)
	http.SetCookie(w, sessionCookie(token, store.StageFull))
	w.WriteHeader(http.StatusNoContent)
}

// failMFA counts a wrong second factor and answers invalid_credentials. Five
// wrong codes revoke the pending session.
func (h *authHandler) failMFA(w http.ResponseWriter, r *http.Request, session Session, user store.User, reason string) {
	if err := h.deps.Store.RecordFailure(r.Context(), user.ID, maxFailures, h.lockDuration()); err != nil {
		slog.Warn("record failure", "user_id", user.ID, "error", err)
	}

	failures, err := h.deps.Store.IncrementMFAFailures(r.Context(), session.ID)
	if err != nil {
		slog.Warn("increment mfa failures", "session_id", session.ID, "error", err)
	}
	if err == nil && failures >= maxMFAFailures {
		if err := h.deps.Store.RevokeSession(r.Context(), session.ID); err != nil {
			slog.Warn("revoke pending session", "session_id", session.ID, "error", err)
		}
		http.SetCookie(w, sessionCookie("", ""))
	}

	h.audit(r, user.Username, "mfa_failure", "failure", map[string]any{"reason": reason})
	writeError(w, http.StatusUnauthorized, "invalid_credentials")
}

type sessionResponse struct {
	Username string `json:"username"`
	Stage    string `json:"stage"`
}

func (h *authHandler) session(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{
		Username: session.User.Username,
		Stage:    session.Stage,
	})
}

func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie("", ""))

	if session, ok := sessionFromContext(r.Context()); ok {
		if err := h.deps.Store.RevokeSession(r.Context(), session.ID); err != nil {
			slog.Warn("revoke session", "session_id", session.ID, "error", err)
		}
		h.audit(r, session.User.Username, "logout", "success", nil)
	}
	w.WriteHeader(http.StatusNoContent)
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	Code            string `json:"code"`
}

func (h *authHandler) password(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	if session.Stage != store.StageFull {
		writeError(w, http.StatusUnauthorized, "mfa_required")
		return
	}

	var request passwordRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, err := h.deps.Store.GetUser(r.Context(), session.User.ID)
	if err != nil {
		internal(w, "get user for password change", err)
		return
	}

	valid, err := auth.VerifyPassword(user.PasswordHash, request.CurrentPassword)
	if err != nil {
		internal(w, "verify current password", err)
		return
	}
	if !valid {
		h.failPasswordChange(w, r, user)
		return
	}
	// The policy is checked before the code is spent, so a rejected password
	// does not cost the user a code.
	if err := auth.ValidatePasswordPolicy(request.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password")
		return
	}
	if err := h.verifyCode(r.Context(), user, request.Code); err != nil {
		if errors.Is(err, errInvalidCode) {
			h.failPasswordChange(w, r, user)
			return
		}
		slog.Error("verify code for password change", "user_id", user.ID, "error", err)
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	if err := h.deps.Store.SetPassword(r.Context(), user.ID, auth.HashPassword(request.NewPassword)); err != nil {
		internal(w, "set password", err)
		return
	}
	if _, err := h.deps.Store.RevokeUserDevices(r.Context(), user.ID); err != nil {
		internal(w, "revoke trusted devices", err)
		return
	}
	revoked, err := h.deps.Store.RevokeUserSessions(r.Context(), user.ID, session.ID)
	if err != nil {
		internal(w, "revoke the other sessions", err)
		return
	}

	h.audit(r, user.Username, "password_changed", "success", nil)
	if revoked > 0 {
		h.audit(r, user.Username, "sessions_revoked", "success", map[string]any{"count": revoked})
	}
	w.WriteHeader(http.StatusNoContent)
}

// failPasswordChange counts a failed attempt and answers invalid_credentials.
func (h *authHandler) failPasswordChange(w http.ResponseWriter, r *http.Request, user store.User) {
	if err := h.deps.Store.RecordFailure(r.Context(), user.ID, maxFailures, h.lockDuration()); err != nil {
		slog.Warn("record failure", "user_id", user.ID, "error", err)
	}
	h.audit(r, user.Username, "password_changed", "failure", nil)
	writeError(w, http.StatusUnauthorized, "invalid_credentials")
}
