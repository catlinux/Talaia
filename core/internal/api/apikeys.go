package api

import (
	"crypto/rand"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

const (
	defaultAPIKeyDays = 90
	maxAPIKeyDays     = 365
	keyAlphabet       = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

var errDuplicateKeyBinding = errors.New("duplicate api key binding")

type apiKeyCreateRequest struct {
	Name          string           `json:"name"`
	ExpiresInDays int              `json:"expires_in_days"`
	Bindings      []bindingRequest `json:"bindings"`
	TOTPCode      string           `json:"totp_code"`
}

type apiKeyCreatedResponse struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
}

type apiKeyView struct {
	ID         string         `json:"id"`
	KeyID      string         `json:"key_id"`
	Name       string         `json:"name"`
	ExpiresAt  time.Time      `json:"expires_at"`
	LastUsedAt *time.Time     `json:"last_used_at"`
	Revoked    bool           `json:"revoked"`
	Bindings   []adminBinding `json:"bindings"`
}

type apiKeysResponse struct {
	Keys []apiKeyView `json:"keys"`
}

func randomKeyPart(length int) (string, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, length)
	for i, value := range raw {
		out[i] = keyAlphabet[int(value)%len(keyAlphabet)]
	}
	return string(out), nil
}

func newAPIKeyToken() (keyID, secret, token string, err error) {
	keyID, err = randomKeyPart(12)
	if err != nil {
		return "", "", "", err
	}
	secret, err = randomKeyPart(32)
	if err != nil {
		return "", "", "", err
	}
	return keyID, secret, "tlk_" + keyID + "_" + secret, nil
}

func (h *userAdminHandler) validateKeyBindings(r *http.Request, bindings []bindingRequest) ([]bindingRequest, error) {
	if len(bindings) < 1 || len(bindings) > 10 {
		return nil, fieldError{"bindings", "out_of_range"}
	}
	seen := map[string]bool{}
	validated := make([]bindingRequest, 0, len(bindings))
	for i, binding := range bindings {
		binding.Role = strings.TrimSpace(binding.Role)
		if !validAdminRole(binding.Role) {
			return nil, fieldError{"bindings[" + strconv.Itoa(i) + "].role", "invalid_value"}
		}
		scope, err := h.validScope(r, binding.Scope)
		if err != nil {
			var fieldErr fieldError
			if errors.As(err, &fieldErr) {
				fieldErr.Field = "bindings[" + strconv.Itoa(i) + "]." + fieldErr.Field
				return nil, fieldErr
			}
			return nil, err
		}
		binding.Scope = scope
		key := scope.Type + "\x00" + scope.ServerID + "\x00" + scope.GroupID + "\x00" + scope.Name
		if seen[key] {
			return nil, errDuplicateKeyBinding
		}
		seen[key] = true
		validated = append(validated, binding)
	}
	return validated, nil
}

func (h *userAdminHandler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	var request apiKeyCreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if !validAdminName(request.Name) {
		writeValidationError(w, validateAdminName("name", request.Name))
		return
	}
	if request.ExpiresInDays < 0 || request.ExpiresInDays > maxAPIKeyDays {
		writeInvalid(w, "expires_in_days", "out_of_range")
		return
	}
	if request.ExpiresInDays == 0 {
		request.ExpiresInDays = defaultAPIKeyDays
	}
	bindings, err := h.validateKeyBindings(r, request.Bindings)
	if err != nil {
		if errors.Is(err, errDuplicateKeyBinding) {
			writeError(w, http.StatusConflict, "binding_exists")
			return
		}
		writeValidationError(w, err)
		return
	}
	if h.deps.Limiter != nil && !h.deps.Limiter.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if h.deps.TOTP == nil {
		internal(w, "create api key without a totp verifier", errors.New("missing totp verifier"))
		return
	}
	if err := h.deps.TOTP.VerifyCode(r.Context(), session.User, request.TOTPCode); err != nil {
		if errors.Is(err, errInvalidCode) {
			if h.deps.Audit != nil {
				_ = h.deps.Audit.AppendAudit(r.Context(), store.AuditEvent{
					Actor: session.User.Username, ActorID: session.User.ID, Action: "apikey_use_denied",
					IP: clientIP(r), Result: "denied", Details: map[string]any{"reason": "code_invalid"},
				})
			}
			writeError(w, http.StatusForbidden, "code_invalid")
			return
		}
		writeValidationError(w, err)
		return
	}

	keyID, secret, token, err := newAPIKeyToken()
	if err != nil {
		internal(w, "generate api key", err)
		return
	}
	expiresAt := h.now().Add(time.Duration(request.ExpiresInDays) * 24 * time.Hour)
	key, err := st.CreateAPIKey(r.Context(), keyID, auth.HashToken(secret), request.Name, session.User.ID, expiresAt)
	if err != nil {
		if errors.Is(err, store.ErrAPIKeyLifetime) {
			writeInvalid(w, "expires_in_days", "out_of_range")
			return
		}
		internal(w, "create api key", err)
		return
	}
	for _, binding := range bindings {
		if _, err := st.CreateRoleBinding(r.Context(), store.RoleBinding{
			SubjectType: store.SubjectAPIKey, SubjectID: key.ID,
			ScopeType: binding.Scope.Type, ScopeServerID: binding.Scope.ServerID,
			ScopeGroupID: binding.Scope.GroupID, ScopeName: binding.Scope.Name,
			Role: binding.Role, CreatedBy: session.User.Username,
		}); err != nil {
			_ = st.RevokeAPIKey(r.Context(), key.ID)
			if errors.Is(err, store.ErrBindingExists) {
				writeError(w, http.StatusConflict, "binding_exists")
				return
			}
			internal(w, "create api key binding", err)
			return
		}
	}

	h.audit(r, "apikey_create", apiKeyScope(key.ID), map[string]any{
		"api_key_id": key.ID, "key_id": key.KeyID, "name": key.Name, "bindings": len(bindings),
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, apiKeyCreatedResponse{
		ID: key.ID, Key: token, Name: key.Name, ExpiresAt: key.ExpiresAt,
	})
}

func (h *userAdminHandler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	effective, _ := effectiveFromRequest(r)
	all := r.URL.Query().Get("all") == "1"
	if all && !effective.Can(rbac.PermUsersManage, rbac.Target{}) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var (
		keys []store.APIKey
		err  error
	)
	if all {
		keys, err = st.ListAPIKeys(r.Context())
	} else {
		keys, err = st.ListAPIKeysByOwner(r.Context(), session.User.ID)
	}
	if err != nil {
		internal(w, "list api keys", err)
		return
	}
	response := apiKeysResponse{Keys: make([]apiKeyView, 0, len(keys))}
	for _, key := range keys {
		bindings, err := st.ListRoleBindings(r.Context(), store.SubjectAPIKey, key.ID)
		if err != nil {
			internal(w, "list api key bindings", err)
			return
		}
		view := apiKeyView{
			ID: key.ID, KeyID: key.KeyID, Name: key.Name, ExpiresAt: key.ExpiresAt,
			Revoked: !key.RevokedAt.IsZero(), Bindings: make([]adminBinding, 0, len(bindings)),
		}
		if !key.LastUsedAt.IsZero() {
			last := key.LastUsedAt
			view.LastUsedAt = &last
		}
		for _, binding := range bindings {
			view.Bindings = append(view.Bindings, adminBindingOf(binding))
		}
		response.Keys = append(response.Keys, view)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *userAdminHandler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	id := chi.URLParam(r, "id")
	effective, _ := effectiveFromRequest(r)
	ownerKeys, err := st.ListAPIKeysByOwner(r.Context(), session.User.ID)
	if err != nil {
		internal(w, "list api keys for revoke", err)
		return
	}
	var key *store.APIKey
	for i := range ownerKeys {
		if ownerKeys[i].ID == id || ownerKeys[i].KeyID == id {
			key = &ownerKeys[i]
			break
		}
	}
	if key == nil && effective.Can(rbac.PermUsersManage, rbac.Target{}) {
		allKeys, err := st.ListAPIKeys(r.Context())
		if err != nil {
			internal(w, "list all api keys for revoke", err)
			return
		}
		for i := range allKeys {
			if allKeys[i].ID == id || allKeys[i].KeyID == id {
				key = &allKeys[i]
				break
			}
		}
	}
	if key == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err := st.RevokeAPIKey(r.Context(), key.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		internal(w, "revoke api key", err)
		return
	}
	h.audit(r, "apikey_revoke", apiKeyScope(key.ID), map[string]any{
		"api_key_id": key.ID, "key_id": key.KeyID, "name": key.Name,
	})
	w.WriteHeader(http.StatusNoContent)
}

func apiKeyScope(keyID string) map[string]any {
	return map[string]any{"type": "api_key", "api_key_id": keyID}
}
