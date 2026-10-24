package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

// BindingLister returns the direct and team bindings of a user.
type BindingLister interface {
	ListRoleBindingsForUser(ctx context.Context, userID string) ([]store.RoleBinding, error)
}

type keyBindingLister interface {
	ListRoleBindings(ctx context.Context, subjectType, subjectID string) ([]store.RoleBinding, error)
}

// ServerGetter returns a server and its group.
type ServerGetter interface {
	GetServer(ctx context.Context, id string) (store.Server, error)
}

// AuthorizerDeps are the collaborators of the permission checks.
type AuthorizerDeps struct {
	Bindings BindingLister
	Servers  ServerGetter
	Agents   LiveAgents
	Audit    Auditor
}

// Authorizer resolves a request's effective permissions and enforces them.
type Authorizer struct {
	bindings BindingLister
	servers  ServerGetter
	agents   LiveAgents
	audit    Auditor
}

// NewAuthorizer builds an authorizer. Missing collaborators fail closed.
func NewAuthorizer(deps AuthorizerDeps) *Authorizer {
	return &Authorizer{
		bindings: deps.Bindings,
		servers:  deps.Servers,
		agents:   deps.Agents,
		audit:    deps.Audit,
	}
}

type authorizerKey struct{}
type effectiveKey struct{}

type effectiveState struct {
	effective rbac.Effective
	err       error
	loaded    bool
}

func withAuthorizer(ctx context.Context, authorizer *Authorizer) context.Context {
	return context.WithValue(ctx, authorizerKey{}, authorizer)
}

func authorizerFromContext(ctx context.Context) *Authorizer {
	authorizer, _ := ctx.Value(authorizerKey{}).(*Authorizer)
	return authorizer
}

// targetKind says which parts of an rbac.Target a route needs.
type targetKind uint8

const (
	targetNone targetKind = iota
	targetOrganization
	targetServer
	targetStack
	targetContainer
)

type permissionMode uint8

const (
	permExact permissionMode = iota
	permAny
	permSeeServer
)

type routeKey struct {
	Method  string
	Pattern string
}

type routePermission struct {
	mode       permissionMode
	permission rbac.Permission
	target     targetKind
	noAPIKey   bool
}

// routePerms is the single permission decision for every protected route.
// Patterns are relative to /api/v1, matching the registration in api.go.
var routePerms = map[routeKey]routePermission{
	{http.MethodGet, "/servers"}:                                             {mode: permAny},
	{http.MethodGet, "/me"}:                                                  {mode: permAny},
	{http.MethodGet, "/stacks"}:                                              {mode: permAny},
	{http.MethodGet, "/users"}:                                               {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/users/invites"}:                                      {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodGet, "/users/invites"}:                                       {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodDelete, "/users/invites/{inviteID}"}:                         {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodGet, "/users/{userID}/bindings"}:                             {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/users/{userID}/bindings"}:                            {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPatch, "/users/{userID}"}:                                    {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodDelete, "/users/{userID}"}:                                   {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/users/{userID}/disable"}:                             {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/users/{userID}/enable"}:                              {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPatch, "/bindings/{bindingID}"}:                              {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodDelete, "/bindings/{bindingID}"}:                             {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodGet, "/groups"}:                                              {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/groups"}:                                             {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPatch, "/groups/{groupID}"}:                                  {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodDelete, "/groups/{groupID}"}:                                 {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPut, "/servers/{serverID}/group"}:                            {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodGet, "/teams"}:                                               {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/teams"}:                                              {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodGet, "/teams/{teamID}"}:                                      {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPatch, "/teams/{teamID}"}:                                    {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodDelete, "/teams/{teamID}"}:                                   {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPut, "/teams/{teamID}/members/{userID}"}:                     {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodDelete, "/teams/{teamID}/members/{userID}"}:                  {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodGet, "/teams/{teamID}/bindings"}:                             {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/teams/{teamID}/bindings"}:                            {mode: permExact, permission: rbac.PermUsersManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/apikeys"}:                                            {mode: permAny, noAPIKey: true},
	{http.MethodGet, "/apikeys"}:                                             {mode: permAny, noAPIKey: true},
	{http.MethodDelete, "/apikeys/{id}"}:                                     {mode: permAny, noAPIKey: true},
	{http.MethodGet, "/audit"}:                                               {mode: permExact, permission: rbac.PermAuditView, target: targetOrganization},
	{http.MethodGet, "/recordings"}:                                          {mode: permExact, permission: rbac.PermRecordingsView, target: targetOrganization, noAPIKey: true},
	{http.MethodGet, "/recordings/{sessionID}/download"}:                     {mode: permExact, permission: rbac.PermRecordingsView, target: targetOrganization, noAPIKey: true},
	{http.MethodGet, "/settings/oidc"}:                                       {mode: permExact, permission: rbac.PermSettingsManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPut, "/settings/oidc"}:                                       {mode: permExact, permission: rbac.PermSettingsManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/servers/{serverID}/agent/update"}:                    {mode: permExact, permission: rbac.PermServerAgentUpdate, target: targetServer},
	{http.MethodPost, "/agents/update-all"}:                                  {mode: permExact, permission: rbac.PermServerAgentUpdate, target: targetOrganization},
	{http.MethodDelete, "/servers/{serverID}"}:                               {mode: permExact, permission: rbac.PermServerRemove, target: targetServer},
	{http.MethodGet, "/servers/{serverID}/containers"}:                       {mode: permSeeServer, permission: rbac.PermServerView, target: targetServer},
	{http.MethodGet, "/servers/{serverID}/metrics"}:                          {mode: permSeeServer, permission: rbac.PermServerView, target: targetServer},
	{http.MethodGet, "/servers/{serverID}/containers/{container}/metrics"}:   {mode: permExact, permission: rbac.PermContainerView, target: targetContainer},
	{http.MethodPost, "/servers/{serverID}/containers/{container}/actions"}:  {mode: permExact, permission: rbac.PermContainerControl, target: targetContainer},
	{http.MethodPost, "/servers/{serverID}/containers/{container}/logs"}:     {mode: permExact, permission: rbac.PermContainerLogs, target: targetContainer},
	{http.MethodPost, "/servers/{serverID}/journal"}:                         {mode: permExact, permission: rbac.PermHostJournal, target: targetServer},
	{http.MethodPost, "/servers/{serverID}/apt/status"}:                      {mode: permExact, permission: rbac.PermHostAptView, target: targetServer},
	{http.MethodPost, "/servers/{serverID}/apt/upgrade"}:                     {mode: permExact, permission: rbac.PermHostAptUpgrade, target: targetServer},
	{http.MethodPost, "/servers/{serverID}/reboot"}:                          {mode: permExact, permission: rbac.PermHostReboot, target: targetServer, noAPIKey: true},
	{http.MethodGet, "/servers/{serverID}/services"}:                         {mode: permExact, permission: rbac.PermHostServicesView, target: targetServer},
	{http.MethodPost, "/servers/{serverID}/services/{unit}/actions"}:         {mode: permExact, permission: rbac.PermHostServicesControl, target: targetServer},
	{http.MethodGet, "/jobs/{jobID}"}:                                        {mode: permAny},
	{http.MethodPost, "/servers/{serverID}/containers/{container}/terminal"}: {mode: permExact, permission: rbac.PermContainerTerminal, target: targetContainer, noAPIKey: true},
	{http.MethodPost, "/servers/{serverID}/terminal"}:                        {mode: permExact, permission: rbac.PermHostTerminal, target: targetServer, noAPIKey: true},
	{http.MethodGet, "/terminal/{sessionID}/ws"}:                             {mode: permAny, noAPIKey: true},
	{http.MethodPost, "/servers/{serverID}/resources/list"}:                  {mode: permExact, permission: rbac.PermResourcesManage, target: targetServer},
	{http.MethodPost, "/servers/{serverID}/resources/remove"}:                {mode: permExact, permission: rbac.PermResourcesManage, target: targetServer},
	{http.MethodGet, "/servers/{serverID}/stacks"}:                           {mode: permSeeServer, permission: rbac.PermServerView, target: targetServer},
	{http.MethodPut, "/servers/{serverID}/stacks/{name}"}:                    {mode: permExact, permission: rbac.PermStackEdit, target: targetStack},
	{http.MethodPost, "/servers/{serverID}/stacks/{name}/read"}:              {mode: permExact, permission: rbac.PermStackEdit, target: targetStack},
	{http.MethodPost, "/servers/{serverID}/stacks/{name}/adopt"}:             {mode: permExact, permission: rbac.PermStackEdit, target: targetStack},
	{http.MethodPost, "/servers/{serverID}/stacks/{name}/actions"}:           {mode: permExact, permission: rbac.PermStackDeploy, target: targetStack},
	{http.MethodPost, "/servers/enrollment-tokens"}:                          {mode: permExact, permission: rbac.PermServerEnroll, target: targetOrganization},
	{http.MethodGet, "/system/version"}:                                      {mode: permExact, permission: rbac.PermSettingsManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/system/check"}:                                       {mode: permExact, permission: rbac.PermSettingsManage, target: targetOrganization, noAPIKey: true},
	{http.MethodPost, "/system/update"}:                                      {mode: permExact, permission: rbac.PermSettingsManage, target: targetOrganization, noAPIKey: true},
}

// registerRoute wraps handler with the permission declared in routePerms. A
// missing declaration or authorizer answers 403, so a new route is never open
// by accident.
func registerRoute(r chi.Router, authorizer *Authorizer, method, pattern string, handler http.Handler) {
	permission, ok := routePerms[routeKey{Method: method, Pattern: pattern}]
	if !ok {
		r.Method(method, pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusForbidden, "forbidden")
		}))
		return
	}
	r.Method(method, pattern, authorizer.requireRoute(permission, handler))
}

// requireRoute applies the permission mode declared in routePerms.
func (a *Authorizer) requireRoute(route routePermission, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := sessionFromContext(r.Context())
		if !ok || session.Stage != store.StageFull {
			writeError(w, http.StatusUnauthorized, sessionDeniedCode(r.Context()))
			return
		}
		if a == nil {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if session.AuthKind == AuthKindAPIKey && route.noAPIKey {
			a.deny(w, r, session, route.permission, targetNone, rbac.Target{}, http.StatusForbidden, "forbidden")
			return
		}

		r = r.WithContext(withAuthorizer(r.Context(), a))
		effective, r, err := a.resolveRequest(r)
		if err != nil {
			slog.Error("resolve permissions", "user_id", session.User.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if route.mode == permAny {
			next.ServeHTTP(w, r)
			return
		}

		target, err := a.targetFor(r, route.target)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				a.deny(w, r, session, route.permission, route.target, target, http.StatusNotFound, "not_found")
				return
			}
			slog.Error("build authorization target", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if !a.visible(effective, route.target, target) {
			a.deny(w, r, session, route.permission, route.target, target, http.StatusNotFound, "not_found")
			return
		}
		if route.mode == permSeeServer {
			next.ServeHTTP(w, r)
			return
		}
		if !effective.Can(route.permission, target) {
			a.deny(w, r, session, route.permission, route.target, target, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// effectiveFromRequest returns the permissions already resolved by requireRoute.
func effectiveFromRequest(r *http.Request) (rbac.Effective, bool) {
	state, ok := r.Context().Value(effectiveKey{}).(*effectiveState)
	if !ok || !state.loaded || state.err != nil {
		return rbac.Effective{}, false
	}
	return state.effective, true
}

// withEffective is used by handler tests that bypass the router middleware.
func withEffective(ctx context.Context, effective rbac.Effective) context.Context {
	return context.WithValue(ctx, effectiveKey{}, &effectiveState{effective: effective, loaded: true})
}

// authorize is the body-dependent check handlers use after requireRoute.
func authorize(r *http.Request, permission rbac.Permission, target rbac.Target) bool {
	authorizer := authorizerFromContext(r.Context())
	if authorizer == nil {
		return false
	}
	effective, _, err := authorizer.resolveRequest(r)
	if err != nil {
		return false
	}
	return effective.Can(permission, target)
}

// denyRequest writes and audits a denial from a handler's body-dependent check.
func denyRequest(w http.ResponseWriter, r *http.Request, permission rbac.Permission, target rbac.Target, status int, code string) {
	authorizer := authorizerFromContext(r.Context())
	session, _ := sessionFromContext(r.Context())
	if authorizer == nil {
		writeError(w, status, code)
		return
	}
	authorizer.deny(w, r, session, permission, targetKindForTarget(target), target, status, code)
}

func (a *Authorizer) resolveRequest(r *http.Request) (rbac.Effective, *http.Request, error) {
	if state, ok := r.Context().Value(effectiveKey{}).(*effectiveState); ok && state.loaded {
		return state.effective, r, state.err
	}

	state := &effectiveState{loaded: true}
	session, ok := sessionFromContext(r.Context())
	switch {
	case !ok:
		state.err = errors.New("no session")
	case a.bindings == nil:
		state.effective = rbac.Resolve(nil)
	case session.AuthKind == AuthKindAPIKey:
		ownerRows, err := a.bindings.ListRoleBindingsForUser(r.Context(), session.User.ID)
		if err != nil {
			state.err = err
		} else if keyLister, ok := a.bindings.(keyBindingLister); !ok {
			state.err = errors.New("api key bindings are not available")
		} else {
			keyRows, err := keyLister.ListRoleBindings(r.Context(), store.SubjectAPIKey, session.APIKeyID)
			if err != nil {
				state.err = err
			} else {
				owner := rbac.Resolve(toRBACBindings(ownerRows))
				key := rbac.Resolve(toRBACBindings(keyRows))
				state.effective = key.Limit(owner)
			}
		}
	default:
		bindings, err := a.bindings.ListRoleBindingsForUser(r.Context(), session.User.ID)
		if err != nil {
			state.err = err
		} else {
			state.effective = rbac.Resolve(toRBACBindings(bindings))
		}
	}

	r = r.WithContext(context.WithValue(r.Context(), effectiveKey{}, state))
	return state.effective, r, state.err
}

func toRBACBindings(bindings []store.RoleBinding) []rbac.Binding {
	result := make([]rbac.Binding, 0, len(bindings))
	for _, binding := range bindings {
		result = append(result, rbac.Binding{
			Scope: rbac.Scope{
				Type:     rbac.ScopeType(binding.ScopeType),
				ServerID: binding.ScopeServerID,
				GroupID:  binding.ScopeGroupID,
				Name:     binding.ScopeName,
			},
			Role: rbac.Role(binding.Role),
		})
	}
	return result
}

func (a *Authorizer) targetFor(r *http.Request, kind targetKind) (rbac.Target, error) {
	target := rbac.Target{}
	if kind == targetNone || kind == targetOrganization {
		return target, nil
	}

	target.ServerID = chi.URLParam(r, "serverID")
	if a.servers != nil && target.ServerID != "" {
		server, err := a.servers.GetServer(r.Context(), target.ServerID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return target, err
		}
		if err == nil {
			target.GroupID = server.GroupID
		}
	}

	switch kind {
	case targetStack:
		target.Stack = chi.URLParam(r, "name")
	case targetContainer:
		target.Container = chi.URLParam(r, "container")
		target.Stack = containerProject(a.agents, target.ServerID, target.Container)
	}
	return target, nil
}

// targetFromRequest rebuilds the target of a container route for a
// body-dependent check.
func targetFromRequest(r *http.Request, kind targetKind) (rbac.Target, error) {
	authorizer := authorizerFromContext(r.Context())
	if authorizer == nil {
		return rbac.Target{}, errors.New("no authorizer")
	}
	return authorizer.targetFor(r, kind)
}

func containerProject(agents LiveAgents, serverID, reference string) string {
	if agents == nil || serverID == "" || reference == "" {
		return ""
	}
	agent, ok := connectedAgent(agents, serverID)
	if !ok {
		return ""
	}
	for _, container := range agent.ContainerList {
		id := container.GetId()
		if container.GetName() == reference || id == reference {
			return container.GetComposeProject()
		}
	}
	return ""
}

func (a *Authorizer) visible(effective rbac.Effective, kind targetKind, target rbac.Target) bool {
	switch kind {
	case targetNone, targetOrganization:
		return true
	case targetServer:
		return effective.CanSeeServer(target.ServerID, target.GroupID)
	case targetStack:
		return effective.Can(rbac.PermStackView, target)
	case targetContainer:
		return effective.Can(rbac.PermContainerView, target)
	default:
		return false
	}
}

func (a *Authorizer) deny(w http.ResponseWriter, r *http.Request, session Session, permission rbac.Permission, kind targetKind, target rbac.Target, status int, code string) {
	details := map[string]any{
		"permission": string(permission),
		"method":     r.Method,
		"path":       r.URL.Path,
	}
	if session.AuthKind == AuthKindAPIKey {
		details["api_key_id"] = session.APIKeyID
	}
	if a.audit != nil {
		event := store.AuditEvent{
			Actor:   session.User.Username,
			ActorID: session.User.ID,
			Action:  "authz_denied",
			IP:      clientIP(r),
			Result:  "denied",
			Details: details,
			Scope:   auditScope(kind, target),
		}
		if err := a.audit.AppendAudit(r.Context(), event); err != nil {
			slog.Error("append authz denial audit", "permission", permission, "error", err)
		}
	}
	writeError(w, status, code)
}

func auditScope(kind targetKind, target rbac.Target) map[string]any {
	if kind == targetOrganization {
		return map[string]any{"type": "organization"}
	}
	scope := map[string]any{}
	if target.ServerID != "" {
		scope["server_id"] = target.ServerID
	}
	if target.GroupID != "" {
		scope["group_id"] = target.GroupID
	}
	switch {
	case target.Container != "":
		scope["type"] = "container"
		scope["container"] = target.Container
		if target.Stack != "" {
			scope["stack"] = target.Stack
		}
	case target.Stack != "":
		scope["type"] = "stack"
		scope["stack"] = target.Stack
	case target.ServerID != "":
		scope["type"] = "server"
	default:
		return nil
	}
	return scope
}

func targetKindForTarget(target rbac.Target) targetKind {
	switch {
	case target.Container != "":
		return targetContainer
	case target.Stack != "":
		return targetStack
	case target.ServerID != "":
		return targetServer
	default:
		return targetOrganization
	}
}
