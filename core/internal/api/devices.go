package api

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

const (
	// DeviceCookieName is the long-lived cookie of a trusted device.
	DeviceCookieName = "__Host-talaia_device"
	// BrowserCookieName marks the browser session. It has no Max-Age, so it
	// disappears when the browser closes.
	BrowserCookieName = "__Host-talaia_browser"

	// deviceCookieMaxAge is the 180 days of the trusted device cookie.
	deviceCookieMaxAge = 180 * 24 * 60 * 60

	// maxPINFailures is how many wrong PINs disable a device.
	maxPINFailures = 5
	// maxDeviceNameLength bounds the name shown in the panel, in runes.
	maxDeviceNameLength = 100
	// defaultDeviceMFAHours is used when the configuration is not set.
	defaultDeviceMFAHours = 12
)

// deviceCookie builds the trusted device cookie. An empty token clears it.
func deviceCookie(token string) *http.Cookie {
	cookie := &http.Cookie{
		Name:     DeviceCookieName,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
	if token == "" {
		cookie.MaxAge = -1
		return cookie
	}
	cookie.Value = token
	cookie.MaxAge = deviceCookieMaxAge
	return cookie
}

// browserCookie builds the browser-session marker. An empty token clears it.
func browserCookie(token string) *http.Cookie {
	cookie := &http.Cookie{
		Name:     BrowserCookieName,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
	if token == "" {
		cookie.MaxAge = -1
		return cookie
	}
	cookie.Value = token
	return cookie
}

// cookieHash returns the hash of a cookie value, or nil when it is absent.
func cookieHash(r *http.Request, name string) []byte {
	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" {
		return nil
	}
	return auth.HashToken(cookie.Value)
}

// deviceMFAWindow returns how long a trusted device avoids the second factor.
func (h *authHandler) deviceMFAWindow() time.Duration {
	hours := h.deps.DeviceMFAHours
	if hours <= 0 {
		hours = defaultDeviceMFAHours
	}
	return time.Duration(hours) * time.Hour
}

// deviceFromCookie returns the active device of the request, when it belongs to
// userID. It is used after a successful second factor to refresh that device.
func (h *authHandler) deviceFromCookie(r *http.Request, userID string) (store.TrustedDevice, bool) {
	hash := cookieHash(r, DeviceCookieName)
	if hash == nil {
		return store.TrustedDevice{}, false
	}
	device, err := h.deps.Store.GetTrustedDeviceByTokenHash(r.Context(), hash)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Warn("load trusted device", "error", err)
		}
		return store.TrustedDevice{}, false
	}
	if device.UserID != userID {
		return store.TrustedDevice{}, false
	}
	return device, true
}

// setBrowserMarker rotates the browser marker after a successful second factor
// and binds any trusted device of this browser to it.
func (h *authHandler) setBrowserMarker(r *http.Request, w http.ResponseWriter, userID string) {
	token := auth.NewSessionToken()
	if device, ok := h.deviceFromCookie(r, userID); ok {
		if err := h.deps.Store.CompleteDeviceMFA(r.Context(), device.ID, auth.HashToken(token), h.now()); err != nil {
			slog.Warn("complete trusted device mfa", "device_id", device.ID, "error", err)
		}
	}
	http.SetCookie(w, browserCookie(token))
}

type trustedDeviceResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Current    bool       `json:"current"`
}

type trustedDevicesResponse struct {
	Devices []trustedDeviceResponse `json:"devices"`
}

func deviceResponseOf(device store.TrustedDevice, current bool) trustedDeviceResponse {
	var lastUsed *time.Time
	if !device.LastUsedAt.IsZero() {
		value := device.LastUsedAt
		lastUsed = &value
	}
	return trustedDeviceResponse{
		ID:         device.ID,
		Name:       device.Name,
		CreatedAt:  device.CreatedAt,
		LastUsedAt: lastUsed,
		Current:    current,
	}
}

type createDeviceRequest struct {
	Password string `json:"password"`
	PIN      string `json:"pin"`
	Name     string `json:"name"`
}

// createDevice registers the current browser as a trusted device. The route is
// behind RequireFull and the password is checked again.
func (h *authHandler) createDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}

	var request createDeviceRequest
	if err := decodeJSON(w, r, &request); err != nil || request.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || utf8.RuneCountInString(name) > maxDeviceNameLength {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := auth.ValidatePIN(request.PIN); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, err := h.deps.Store.GetUser(r.Context(), session.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	if err != nil {
		internal(w, "get user to trust a device", err)
		return
	}
	valid, err := auth.VerifyPassword(user.PasswordHash, request.Password)
	if err != nil {
		internal(w, "verify the device password", err)
		return
	}
	if !valid {
		if err := h.deps.Store.RecordFailure(r.Context(), user.ID, maxFailures, h.lockDuration()); err != nil {
			slog.Warn("record failure", "user_id", user.ID, "error", err)
		}
		h.audit(r, user.Username, "device_trusted", "failure", nil)
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	deviceToken := auth.NewSessionToken()
	browserToken := auth.NewSessionToken()
	device, err := h.deps.Store.CreateTrustedDevice(r.Context(), user.ID,
		auth.HashToken(deviceToken), auth.HashPassword(request.PIN), name,
		auth.HashToken(browserToken), h.now())
	if errors.Is(err, store.ErrDeviceLimit) {
		writeError(w, http.StatusConflict, "device_limit")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	if err != nil {
		internal(w, "create trusted device", err)
		return
	}

	http.SetCookie(w, deviceCookie(deviceToken))
	http.SetCookie(w, browserCookie(browserToken))
	h.audit(r, user.Username, "device_trusted", "success", map[string]any{"device_name": device.Name})
	writeJSON(w, http.StatusCreated, deviceResponseOf(device, true))
}

// listDevices returns the active trusted devices of the signed-in user.
func (h *authHandler) listDevices(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	devices, err := h.deps.Store.ListTrustedDevices(r.Context(), session.User.ID)
	if err != nil {
		internal(w, "list trusted devices", err)
		return
	}

	currentHash := cookieHash(r, DeviceCookieName)
	entries := make([]trustedDeviceResponse, 0, len(devices))
	for _, device := range devices {
		current := len(currentHash) > 0 && subtle.ConstantTimeCompare(device.TokenHash, currentHash) == 1
		entries = append(entries, deviceResponseOf(device, current))
	}
	writeJSON(w, http.StatusOK, trustedDevicesResponse{Devices: entries})
}

// deleteDevice removes one of the signed-in user's devices. A device of another
// user is indistinguishable from an unknown one.
func (h *authHandler) deleteDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	id := chi.URLParam(r, "id")
	device, err := h.deps.Store.GetTrustedDevice(r.Context(), session.User.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		internal(w, "get trusted device to delete", err)
		return
	}

	current := false
	if currentHash := cookieHash(r, DeviceCookieName); len(currentHash) > 0 {
		current = subtle.ConstantTimeCompare(device.TokenHash, currentHash) == 1
	}
	if err := h.deps.Store.DeleteTrustedDevice(r.Context(), session.User.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		internal(w, "delete trusted device", err)
		return
	}

	h.audit(r, session.User.Username, "device_removed", "success", map[string]any{"device_name": device.Name})
	if current {
		http.SetCookie(w, deviceCookie(""))
		http.SetCookie(w, browserCookie(""))
	}
	w.WriteHeader(http.StatusNoContent)
}

type pinLoginRequest struct {
	PIN string `json:"pin"`
}

// pinLogin signs in with the PIN of the trusted device cookie. A correct PIN
// either opens a full session, when the browser marker is still fresh, or a
// pending session that the existing TOTP endpoint completes.
func (h *authHandler) pinLogin(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}

	var request pinLoginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	deviceTokenHash := cookieHash(r, DeviceCookieName)
	if deviceTokenHash == nil {
		auth.DummyVerify(request.PIN)
		h.audit(r, "", "login_pin_failed", "failure", map[string]any{"reason": "unknown_device"})
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	device, err := h.deps.Store.GetTrustedDeviceByTokenHash(r.Context(), deviceTokenHash)
	if errors.Is(err, store.ErrNotFound) {
		auth.DummyVerify(request.PIN)
		h.audit(r, "", "login_pin_failed", "failure", map[string]any{"reason": "unknown_device"})
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if err != nil {
		internal(w, "get trusted device for pin login", err)
		return
	}

	user, err := h.deps.Store.GetUser(r.Context(), device.UserID)
	if errors.Is(err, store.ErrNotFound) {
		auth.DummyVerify(request.PIN)
		h.audit(r, "", "login_pin_failed", "failure", map[string]any{"reason": "unknown_device"})
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if err != nil {
		internal(w, "get user for pin login", err)
		return
	}

	valid, err := auth.VerifyPassword(device.PinHash, request.PIN)
	if err != nil {
		slog.Error("verify pin", "device_id", device.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}

	now := h.now()
	locked := user.LockedUntil.After(now)
	if !valid {
		if !locked {
			if _, err := h.deps.Store.RecordPINFail(r.Context(), device.ID, maxPINFailures); err != nil &&
				!errors.Is(err, store.ErrNotFound) {
				slog.Warn("record pin failure", "device_id", device.ID, "error", err)
			}
			h.audit(r, user.Username, "login_pin_failed", "failure", map[string]any{"reason": "wrong_pin"})
		} else {
			h.audit(r, user.Username, "login_pin_failed", "failure", map[string]any{"reason": "locked"})
		}
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if locked {
		h.audit(r, user.Username, "login_pin_failed", "failure", map[string]any{"reason": "locked"})
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	if err := h.deps.Store.ResetFailures(r.Context(), user.ID); err != nil {
		slog.Warn("reset failures", "user_id", user.ID, "error", err)
	}
	if err := h.deps.Store.TouchTrustedDevice(r.Context(), device.ID, now); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.audit(r, user.Username, "login_pin_failed", "failure", map[string]any{"reason": "removed_device"})
			writeError(w, http.StatusUnauthorized, "invalid_credentials")
			return
		}
		internal(w, "touch trusted device", err)
		return
	}

	browserHash := cookieHash(r, BrowserCookieName)
	full := !user.TOTPEnabledAt.IsZero() && browserHash != nil && len(device.BrowserHash) > 0 &&
		subtle.ConstantTimeCompare(browserHash, device.BrowserHash) == 1 &&
		!device.LastMFAAt.IsZero() &&
		now.Sub(device.LastMFAAt) < h.deviceMFAWindow()

	token := auth.NewSessionToken()
	stage := store.StageMFAPending
	ttl := pendingSessionTTL
	if full {
		stage = store.StageFull
		ttl = fullSessionTTL
	}
	if _, err := h.deps.Store.CreateSession(r.Context(), user.ID, auth.HashToken(token),
		stage, now.Add(ttl), clientIP(r), r.UserAgent()); err != nil {
		internal(w, "create pin session", err)
		return
	}
	http.SetCookie(w, sessionCookie(token, stage))

	if !full {
		h.audit(r, user.Username, "login_pin", "success", map[string]any{"mfa": "required"})
		writeError(w, http.StatusUnauthorized, "mfa_required")
		return
	}
	h.audit(r, user.Username, "login_pin", "success", nil)
	w.WriteHeader(http.StatusNoContent)
}
