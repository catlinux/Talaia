package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

// UserAdminDeps are the collaborators of the user and binding API.
type UserAdminDeps struct {
	Store   *store.Store
	Audit   Auditor
	TOTP    TOTPVerifier
	Limiter *RateLimiter
	Now     func() time.Time
}

type userAdminHandler struct {
	deps UserAdminDeps
}

type adminScope struct {
	Type     string `json:"type"`
	ServerID string `json:"server_id,omitempty"`
	GroupID  string `json:"group_id,omitempty"`
	Name     string `json:"name,omitempty"`
}

type adminBinding struct {
	ID    string     `json:"id"`
	Role  string     `json:"role"`
	Scope adminScope `json:"scope"`
}

type adminUser struct {
	ID          string         `json:"id"`
	Username    string         `json:"username"`
	DisplayName string         `json:"display_name"`
	Email       string         `json:"email"`
	Disabled    bool           `json:"disabled"`
	TOTPEnabled bool           `json:"totp_enabled"`
	Bindings    []adminBinding `json:"bindings"`
}

type usersResponse struct {
	Users []adminUser `json:"users"`
}

type userBindingsResponse struct {
	Bindings []adminBinding `json:"bindings"`
}

type userUpdateRequest struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

type bindingRequest struct {
	Role  string     `json:"role"`
	Scope adminScope `json:"scope"`
}

type bindingRoleRequest struct {
	Role string `json:"role"`
}

var scopeNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func validScopeName(name string) bool { return scopeNamePattern.MatchString(name) }

func newUserAdminHandler(deps UserAdminDeps) *userAdminHandler {
	if deps.Audit == nil {
		deps.Audit = deps.Store
	}
	return &userAdminHandler{deps: deps}
}

func scopeOf(binding store.RoleBinding) adminScope {
	return adminScope{
		Type:     binding.ScopeType,
		ServerID: binding.ScopeServerID,
		GroupID:  binding.ScopeGroupID,
		Name:     binding.ScopeName,
	}
}

func adminBindingOf(binding store.RoleBinding) adminBinding {
	return adminBinding{ID: binding.ID, Role: binding.Role, Scope: scopeOf(binding)}
}

func adminUserOf(user store.User, bindings []store.RoleBinding) adminUser {
	result := adminUser{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Disabled:    !user.DisabledAt.IsZero(),
		TOTPEnabled: !user.TOTPEnabledAt.IsZero(),
		Bindings:    make([]adminBinding, 0, len(bindings)),
	}
	for _, binding := range bindings {
		result.Bindings = append(result.Bindings, adminBindingOf(binding))
	}
	return result
}

func (h *userAdminHandler) now() time.Time {
	if h.deps.Now != nil {
		return h.deps.Now()
	}
	return time.Now()
}

func (h *userAdminHandler) storeOrError(w http.ResponseWriter) (*store.Store, bool) {
	if h.deps.Store == nil {
		internal(w, "user admin without a store", errors.New("nil store"))
		return nil, false
	}
	return h.deps.Store, true
}

func (h *userAdminHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	users, err := st.ListUsers(r.Context())
	if err != nil {
		internal(w, "list users", err)
		return
	}
	response := usersResponse{Users: make([]adminUser, 0, len(users))}
	for _, user := range users {
		bindings, err := st.ListRoleBindings(r.Context(), store.SubjectUser, user.ID)
		if err != nil {
			internal(w, "list user bindings", err)
			return
		}
		response.Users = append(response.Users, adminUserOf(user, bindings))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *userAdminHandler) updateUser(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request userUpdateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	userID := chi.URLParam(r, "userID")
	if err := st.UpdateUser(r.Context(), userID, request.DisplayName, request.Email); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "user_update", userScope(userID), map[string]any{"user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) disableUser(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	userID := chi.URLParam(r, "userID")
	session, _ := sessionFromContext(r.Context())
	if userID == session.User.ID {
		writeError(w, http.StatusConflict, "cannot_disable_self")
		return
	}
	if err := st.DisableUser(r.Context(), userID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "user_disable", userScope(userID), map[string]any{"user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) enableUser(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	userID := chi.URLParam(r, "userID")
	if err := st.EnableUser(r.Context(), userID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "user_enable", userScope(userID), map[string]any{"user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) listUserBindings(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	userID := chi.URLParam(r, "userID")
	if _, err := st.GetUser(r.Context(), userID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	bindings, err := st.ListRoleBindings(r.Context(), store.SubjectUser, userID)
	if err != nil {
		internal(w, "list user bindings", err)
		return
	}
	response := userBindingsResponse{Bindings: make([]adminBinding, 0, len(bindings))}
	for _, binding := range bindings {
		response.Bindings = append(response.Bindings, adminBindingOf(binding))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *userAdminHandler) createBinding(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request bindingRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	request.Role = strings.TrimSpace(request.Role)
	userID := chi.URLParam(r, "userID")
	if _, err := st.GetUser(r.Context(), userID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	scope, err := h.validScope(r, request.Scope)
	if err != nil {
		writeValidationError(w, err)
		return
	}
	if !validAdminRole(request.Role) {
		writeInvalid(w, "role", "invalid_value")
		return
	}
	binding, err := st.CreateRoleBinding(r.Context(), store.RoleBinding{
		SubjectType:   store.SubjectUser,
		SubjectID:     userID,
		ScopeType:     scope.Type,
		ScopeServerID: scope.ServerID,
		ScopeGroupID:  scope.GroupID,
		ScopeName:     scope.Name,
		Role:          request.Role,
		CreatedBy:     sessionUsername(r),
	})
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "binding_create", userScope(userID), map[string]any{
		"user_id": userID, "binding_id": binding.ID, "role": binding.Role, "scope": scope,
	})
	writeJSON(w, http.StatusCreated, adminBindingOf(binding))
}

func (h *userAdminHandler) updateBinding(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request bindingRoleRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	request.Role = strings.TrimSpace(request.Role)
	if !validAdminRole(request.Role) {
		writeInvalid(w, "role", "invalid_value")
		return
	}
	bindingID := chi.URLParam(r, "bindingID")
	if err := st.UpdateRoleBindingRole(r.Context(), bindingID, request.Role); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "binding_update", map[string]any{"type": "binding", "binding_id": bindingID},
		map[string]any{"binding_id": bindingID, "role": request.Role})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) deleteBinding(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	bindingID := chi.URLParam(r, "bindingID")
	if err := st.DeleteRoleBinding(r.Context(), bindingID); err != nil {
		h.writeStoreError(w, err)
		return
	}
	h.audit(r, "binding_delete", map[string]any{"type": "binding", "binding_id": bindingID},
		map[string]any{"binding_id": bindingID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *userAdminHandler) validScope(r *http.Request, scope adminScope) (adminScope, error) {
	scope.Type = strings.TrimSpace(scope.Type)
	scope.ServerID = strings.TrimSpace(scope.ServerID)
	scope.GroupID = strings.TrimSpace(scope.GroupID)
	scope.Name = strings.TrimSpace(scope.Name)

	switch scope.Type {
	case string(rbac.ScopeOrganization):
		if scope.ServerID != "" || scope.GroupID != "" || scope.Name != "" {
			return adminScope{}, fieldError{"scope.type", "invalid_value"}
		}
	case string(rbac.ScopeGroup):
		if scope.GroupID == "" {
			return adminScope{}, fieldError{"scope.group_id", "required"}
		}
		if scope.ServerID != "" || scope.Name != "" {
			return adminScope{}, fieldError{"scope.group_id", "invalid_value"}
		}
		if _, err := h.deps.Store.GetGroup(r.Context(), scope.GroupID); err != nil {
			return adminScope{}, fieldError{"scope.group_id", "unknown"}
		}
	case string(rbac.ScopeServer):
		if scope.ServerID == "" {
			return adminScope{}, fieldError{"scope.server_id", "required"}
		}
		if scope.GroupID != "" || scope.Name != "" {
			return adminScope{}, fieldError{"scope.server_id", "invalid_value"}
		}
		if _, err := h.deps.Store.GetServer(r.Context(), scope.ServerID); err != nil {
			return adminScope{}, fieldError{"scope.server_id", "unknown"}
		}
	case string(rbac.ScopeStack), string(rbac.ScopeContainer):
		if scope.ServerID == "" {
			return adminScope{}, fieldError{"scope.server_id", "required"}
		}
		if scope.Name == "" {
			return adminScope{}, fieldError{"scope.name", "required"}
		}
		if scope.GroupID != "" {
			return adminScope{}, fieldError{"scope.group_id", "invalid_value"}
		}
		if len(scope.Name) > 128 || !validScopeName(scope.Name) {
			return adminScope{}, fieldError{"scope.name", "invalid_format"}
		}
		if _, err := h.deps.Store.GetServer(r.Context(), scope.ServerID); err != nil {
			return adminScope{}, fieldError{"scope.server_id", "unknown"}
		}
	default:
		return adminScope{}, fieldError{"scope.type", "invalid_value"}
	}
	return scope, nil
}

func (h *userAdminHandler) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrLastAdmin):
		writeError(w, http.StatusConflict, "last_admin")
	case errors.Is(err, store.ErrBindingExists):
		writeError(w, http.StatusConflict, "binding_exists")
	case errors.Is(err, store.ErrEmailExists):
		writeError(w, http.StatusConflict, "email_taken")
	case errors.Is(err, store.ErrGroupNameExists), errors.Is(err, store.ErrTeamNameExists):
		writeError(w, http.StatusConflict, "name_taken")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	default:
		internal(w, "user admin", err)
	}
}

func (h *userAdminHandler) audit(r *http.Request, action string, scope map[string]any, details map[string]any) {
	if h.deps.Audit == nil {
		return
	}
	session, _ := sessionFromContext(r.Context())
	event := store.AuditEvent{
		Actor:   session.User.Username,
		ActorID: session.User.ID,
		Action:  action,
		IP:      clientIP(r),
		Result:  "success",
		Details: details,
		Scope:   scope,
	}
	// Audit failures never turn a completed administration action into an
	// error for the client.
	_ = h.deps.Audit.AppendAudit(r.Context(), event)
}

func validAdminRole(role string) bool {
	return len(rbac.Role(strings.TrimSpace(role)).Permissions()) > 0
}

func sessionUsername(r *http.Request) string {
	session, _ := sessionFromContext(r.Context())
	return session.User.Username
}

func userScope(userID string) map[string]any {
	return map[string]any{"type": "user", "user_id": userID}
}
