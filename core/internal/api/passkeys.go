package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

const (
	// maxPasskeyNameLength bounds the name shown in the panel, in runes.
	maxPasskeyNameLength = 100
)

// newPasskeyWebAuthn builds the WebAuthn relying party. It returns nil when the
// deployment has no DNS name for the relying-party ID, which disables passkeys.
func newPasskeyWebAuthn(deps AuthDeps) *webauthn.WebAuthn {
	rpID := strings.TrimSpace(deps.PasskeyRPID)
	if rpID == "" || len(deps.PasskeyOrigins) == 0 {
		return nil
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:                  rpID,
		RPDisplayName:         auth.Issuer,
		RPOrigins:             append([]string(nil), deps.PasskeyOrigins...),
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification:   protocol.VerificationRequired,
			ResidentKey:        protocol.ResidentKeyRequirementPreferred,
			RequireResidentKey: protocol.ResidentKeyNotRequired(),
		},
	})
	if err != nil {
		slog.Warn("configure passkeys", "error", err)
		return nil
	}
	return wa
}

// passkeyUser is the webauthn.User of an application user. The WebAuthn ID is
// the stable application UUID, which is also the discoverable user handle.
type passkeyUser struct {
	account     store.User
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte          { return []byte(u.account.ID) }
func (u passkeyUser) WebAuthnName() string        { return u.account.Username }
func (u passkeyUser) WebAuthnDisplayName() string { return u.account.Username }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

// transportsOf converts the stored strings to the library's transport type.
func transportsOf(values []string) []protocol.AuthenticatorTransport {
	transports := make([]protocol.AuthenticatorTransport, 0, len(values))
	for _, value := range values {
		transports = append(transports, protocol.AuthenticatorTransport(value))
	}
	return transports
}

// passkeyCredential reconstructs the verification record of a stored passkey.
func passkeyCredential(passkey store.Passkey) webauthn.Credential {
	return webauthn.Credential{
		ID:        passkey.CredentialID,
		PublicKey: passkey.PublicKey,
		Transport: transportsOf(passkey.Transports),
		Flags: webauthn.CredentialFlags{
			BackupEligible: passkey.BackupEligible,
			BackupState:    passkey.BackupState,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:    passkey.AAGUID,
			SignCount: passkey.SignCount,
		},
	}
}

type passkeyEntry struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	BackupState bool       `json:"backup_state"`
}

type passkeysResponse struct {
	Available bool           `json:"available"`
	Passkeys  []passkeyEntry `json:"passkeys"`
}

func passkeyEntryOf(passkey store.Passkey) passkeyEntry {
	var lastUsed *time.Time
	if !passkey.LastUsedAt.IsZero() {
		value := passkey.LastUsedAt
		lastUsed = &value
	}
	return passkeyEntry{
		ID:          passkey.ID,
		Name:        passkey.Name,
		CreatedAt:   passkey.CreatedAt,
		LastUsedAt:  lastUsed,
		BackupState: passkey.BackupState,
	}
}

// passkeysUnavailable answers the endpoints that need a relying party.
func passkeysUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "passkeys_unavailable")
}

// passkeyRegisterBegin re-verifies the password and starts a registration.
func (h *authHandler) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if h.passkeys == nil {
		passkeysUnavailable(w)
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}

	var request struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &request); err != nil || request.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, err := h.deps.Store.GetUser(r.Context(), session.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	if err != nil {
		internal(w, "get user to register a passkey", err)
		return
	}
	valid, err := auth.VerifyPassword(user.PasswordHash, request.Password)
	if err != nil {
		internal(w, "verify the passkey password", err)
		return
	}
	if !valid {
		if err := h.deps.Store.RecordFailure(r.Context(), user.ID, maxFailures, h.lockDuration()); err != nil {
			slog.Warn("record failure", "user_id", user.ID, "error", err)
		}
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	passkeys, err := h.deps.Store.ListPasskeys(r.Context(), user.ID)
	if err != nil {
		internal(w, "list passkeys to register", err)
		return
	}
	if len(passkeys) >= store.MaxPasskeys {
		writeError(w, http.StatusConflict, "passkey_limit")
		return
	}
	exclusions := make([]protocol.CredentialDescriptor, 0, len(passkeys))
	for _, passkey := range passkeys {
		credential := passkeyCredential(passkey)
		exclusions = append(exclusions, credential.Descriptor())
	}

	creation, sessionData, err := h.passkeys.BeginRegistration(
		passkeyUser{account: user},
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification:   protocol.VerificationRequired,
			ResidentKey:        protocol.ResidentKeyRequirementPreferred,
			RequireResidentKey: protocol.ResidentKeyNotRequired(),
		}),
		webauthn.WithExclusions(exclusions),
	)
	if err != nil {
		internal(w, "begin passkey registration", err)
		return
	}
	data, err := json.Marshal(sessionData)
	if err != nil {
		internal(w, "encode passkey registration ceremony", err)
		return
	}
	if _, err := h.deps.Store.CreateWebAuthnCeremony(r.Context(), user.ID, "register", data,
		h.now().Add(store.WebAuthnCeremonyTTL)); err != nil {
		internal(w, "store passkey registration ceremony", err)
		return
	}
	if _, err := h.deps.Store.PurgeExpiredWebAuthnCeremonies(r.Context()); err != nil {
		slog.Warn("purge expired webauthn ceremonies", "error", err)
	}
	writeJSON(w, http.StatusOK, creation)
}

// passkeyRegisterFinish verifies the attestation and stores the passkey.
func (h *authHandler) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if h.passkeys == nil {
		passkeysUnavailable(w)
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}

	var request struct {
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || utf8.RuneCountInString(name) > maxPasskeyNameLength || len(request.Credential) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	ceremony, err := h.deps.Store.TakeLatestWebAuthnCeremony(r.Context(), session.User.ID, "register")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err != nil {
		internal(w, "take passkey registration ceremony", err)
		return
	}
	var sessionData webauthn.SessionData
	if err := json.Unmarshal(ceremony.SessionData, &sessionData); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, err := h.deps.Store.GetUser(r.Context(), session.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	if err != nil {
		internal(w, "get user to finish a passkey registration", err)
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(request.Credential)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	credential, err := h.passkeys.CreateCredential(passkeyUser{account: user}, sessionData, parsed)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	transports := make([]string, 0, len(credential.Transport))
	for _, transport := range credential.Transport {
		transports = append(transports, string(transport))
	}
	passkey, err := h.deps.Store.CreatePasskey(r.Context(), user.ID, credential.ID,
		credential.PublicKey, credential.Authenticator.AAGUID, credential.Authenticator.SignCount,
		transports, name, credential.Flags.BackupEligible, credential.Flags.BackupState)
	if errors.Is(err, store.ErrPasskeyLimit) {
		writeError(w, http.StatusConflict, "passkey_limit")
		return
	}
	if errors.Is(err, store.ErrPasskeyExists) {
		writeError(w, http.StatusConflict, "passkey_exists")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	if err != nil {
		internal(w, "store passkey", err)
		return
	}

	h.audit(r, user.Username, "passkey_added", "success", map[string]any{"passkey_name": passkey.Name})
	writeJSON(w, http.StatusCreated, passkeyEntryOf(passkey))
}

// listPasskeys returns the signed-in user's passkeys. When the deployment has
// no relying-party DNS name the list is still readable but available is false.
func (h *authHandler) listPasskeys(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	passkeys, err := h.deps.Store.ListPasskeys(r.Context(), session.User.ID)
	if err != nil {
		internal(w, "list passkeys", err)
		return
	}
	entries := make([]passkeyEntry, 0, len(passkeys))
	for _, passkey := range passkeys {
		entries = append(entries, passkeyEntryOf(passkey))
	}
	writeJSON(w, http.StatusOK, passkeysResponse{Available: h.passkeys != nil, Passkeys: entries})
}

// deletePasskey removes one of the signed-in user's passkeys.
func (h *authHandler) deletePasskey(w http.ResponseWriter, r *http.Request) {
	if h.passkeys == nil {
		passkeysUnavailable(w)
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	passkey, err := h.deps.Store.GetPasskey(r.Context(), session.User.ID, id.String())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		internal(w, "get passkey to delete", err)
		return
	}
	if err := h.deps.Store.DeletePasskey(r.Context(), session.User.ID, passkey.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		internal(w, "delete passkey", err)
		return
	}
	h.audit(r, session.User.Username, "passkey_removed", "success", map[string]any{"passkey_name": passkey.Name})
	w.WriteHeader(http.StatusNoContent)
}

type passkeyLoginBeginResponse struct {
	CeremonyID string                        `json:"ceremony_id"`
	Options    *protocol.CredentialAssertion `json:"options"`
}

// passkeyLoginBegin starts a discoverable login. It needs no session.
func (h *authHandler) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if h.passkeys == nil {
		passkeysUnavailable(w)
		return
	}
	if !h.limit(w, r) {
		return
	}
	assertion, sessionData, err := h.passkeys.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		internal(w, "begin passkey login", err)
		return
	}
	data, err := json.Marshal(sessionData)
	if err != nil {
		internal(w, "encode passkey login ceremony", err)
		return
	}
	ceremonyID, err := h.deps.Store.CreateWebAuthnCeremony(r.Context(), "", "login", data,
		h.now().Add(store.WebAuthnCeremonyTTL))
	if err != nil {
		internal(w, "store passkey login ceremony", err)
		return
	}
	if _, err := h.deps.Store.PurgeExpiredWebAuthnCeremonies(r.Context()); err != nil {
		slog.Warn("purge expired webauthn ceremonies", "error", err)
	}
	writeJSON(w, http.StatusOK, passkeyLoginBeginResponse{CeremonyID: ceremonyID, Options: assertion})
}

// passkeyLoginFinish verifies a discoverable assertion and opens a full session.
// Every failure has the same answer so unknown users and credentials cannot be
// distinguished.
func (h *authHandler) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if h.passkeys == nil {
		passkeysUnavailable(w)
		return
	}

	var request struct {
		CeremonyID string          `json:"ceremony_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	ceremonyUUID, err := uuid.Parse(request.CeremonyID)
	if err != nil || len(request.Credential) == 0 {
		h.passkeyLoginFailure(w, r, "", "invalid_ceremony")
		return
	}
	ceremony, err := h.deps.Store.TakeWebAuthnCeremony(r.Context(), ceremonyUUID.String(), "login")
	if errors.Is(err, store.ErrNotFound) {
		h.passkeyLoginFailure(w, r, "", "invalid_ceremony")
		return
	}
	if err != nil {
		internal(w, "take passkey login ceremony", err)
		return
	}
	var sessionData webauthn.SessionData
	if err := json.Unmarshal(ceremony.SessionData, &sessionData); err != nil {
		h.passkeyLoginFailure(w, r, "", "invalid_ceremony")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
	if err != nil {
		h.passkeyLoginFailure(w, r, "", "invalid_credential")
		return
	}

	var foundUser *store.User
	handler := func(_ []byte, userHandle []byte) (webauthn.User, error) {
		id, err := uuid.Parse(string(userHandle))
		if err != nil {
			return nil, errors.New("unknown user")
		}
		user, err := h.deps.Store.GetUser(r.Context(), id.String())
		if err != nil {
			return nil, errors.New("unknown user")
		}
		passkeys, err := h.deps.Store.ListPasskeys(r.Context(), user.ID)
		if err != nil {
			return nil, errors.New("cannot load credentials")
		}
		credentials := make([]webauthn.Credential, 0, len(passkeys))
		for _, passkey := range passkeys {
			credentials = append(credentials, passkeyCredential(passkey))
		}
		foundUser = &user
		return passkeyUser{account: user, credentials: credentials}, nil
	}
	_, credential, err := h.passkeys.ValidatePasskeyLogin(handler, sessionData, parsed)
	if err != nil || foundUser == nil {
		h.passkeyLoginFailure(w, r, "", "invalid_credential")
		return
	}

	now := h.now()
	if foundUser.LockedUntil.After(now) {
		h.passkeyLoginFailure(w, r, foundUser.Username, "locked")
		return
	}
	if err := h.deps.Store.ResetFailures(r.Context(), foundUser.ID); err != nil {
		slog.Warn("reset failures", "user_id", foundUser.ID, "error", err)
	}

	stored, err := h.deps.Store.GetPasskeyByCredentialID(r.Context(), credential.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && stored.UserID != foundUser.ID) {
		h.passkeyLoginFailure(w, r, foundUser.Username, "invalid_credential")
		return
	}
	if err != nil {
		internal(w, "get passkey for login", err)
		return
	}

	if credential.Authenticator.CloneWarning {
		if err := h.deps.Store.DeletePasskey(r.Context(), foundUser.ID, stored.ID); err != nil &&
			!errors.Is(err, store.ErrNotFound) {
			slog.Warn("disable cloned passkey", "passkey_id", stored.ID, "error", err)
		}
		h.audit(r, foundUser.Username, "passkey_clone_suspected", "failure",
			map[string]any{"reason": "sign_count_regression"})
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	if err := h.deps.Store.UpdatePasskeyUse(r.Context(), foundUser.ID, stored.ID,
		credential.Authenticator.SignCount, credential.Flags.BackupState, now); err != nil {
		internal(w, "update passkey use", err)
		return
	}

	token := auth.NewSessionToken()
	if _, err := h.deps.Store.CreateSession(r.Context(), foundUser.ID, auth.HashToken(token),
		store.StageFull, now.Add(fullSessionTTL), clientIP(r), r.UserAgent()); err != nil {
		internal(w, "create passkey session", err)
		return
	}
	http.SetCookie(w, sessionCookie(token, store.StageFull))
	h.setBrowserMarker(r, w, foundUser.ID)
	h.audit(r, foundUser.Username, "login_passkey", "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

// passkeyLoginFailure audits a failed passkey login and answers uniformly.
func (h *authHandler) passkeyLoginFailure(w http.ResponseWriter, r *http.Request, actor, reason string) {
	h.audit(r, actor, "login_passkey_failed", "failure", map[string]any{"reason": reason})
	writeError(w, http.StatusUnauthorized, "invalid_credentials")
}
