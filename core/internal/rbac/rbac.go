// Package rbac resolves role bindings into effective permissions.
package rbac

import "sort"

// Permission is one operation the core can authorise.
type Permission string

const (
	PermContainerView     Permission = "container.view"
	PermContainerLogs     Permission = "container.logs"
	PermContainerControl  Permission = "container.control"
	PermContainerTerminal Permission = "container.terminal"

	PermStackView       Permission = "stack.view"
	PermStackDeploy     Permission = "stack.deploy"
	PermStackEdit       Permission = "stack.edit"
	PermContainerRemove Permission = "container.remove"

	PermServerView          Permission = "server.view"
	PermResourcesManage     Permission = "resources.manage"
	PermHostServicesView    Permission = "host.services.view"
	PermHostServicesControl Permission = "host.services.control"
	PermHostJournal         Permission = "host.journal"
	PermHostAptView         Permission = "host.apt.view"
	PermHostAptUpgrade      Permission = "host.apt.upgrade"
	PermHostReboot          Permission = "host.reboot"
	PermHostTerminal        Permission = "host.terminal"
	PermServerAgentUpdate   Permission = "server.agent_update"

	PermServerRemove   Permission = "server.remove"
	PermServerEnroll   Permission = "server.enroll"
	PermUsersManage    Permission = "users.manage"
	PermSettingsManage Permission = "settings.manage"
	PermAuditView      Permission = "audit.view"
	PermRecordingsView Permission = "recordings.view"
	PermAPIKeysManage  Permission = "apikeys.manage"
	PermAlertsView     Permission = "alerts.view"
	PermAlertsSilence  Permission = "alerts.silence"
	PermAlertsManage   Permission = "alerts.manage"
)

// AllPermissions lists every permission understood by this package.
var AllPermissions = []Permission{
	PermContainerView, PermContainerLogs, PermContainerControl, PermContainerTerminal,
	PermStackView, PermStackDeploy, PermStackEdit, PermContainerRemove,
	PermServerView, PermResourcesManage, PermHostServicesView, PermHostServicesControl,
	PermHostJournal, PermHostAptView, PermHostAptUpgrade, PermHostReboot,
	PermHostTerminal, PermServerAgentUpdate,
	PermServerRemove, PermServerEnroll, PermUsersManage, PermSettingsManage,
	PermAuditView, PermRecordingsView, PermAPIKeysManage,
	PermAlertsView, PermAlertsSilence, PermAlertsManage,
}

// ScopeType is the level at which a binding applies.
type ScopeType string

const (
	ScopeContainer    ScopeType = "container"
	ScopeStack        ScopeType = "stack"
	ScopeServer       ScopeType = "server"
	ScopeGroup        ScopeType = "group"
	ScopeOrganization ScopeType = "organization"
)

// Level returns the scope ordering used by the minimum-level rule. Unknown
// scopes are below every permission.
func (s ScopeType) Level() int {
	switch s {
	case ScopeContainer:
		return 0
	case ScopeStack:
		return 1
	case ScopeServer:
		return 2
	case ScopeGroup:
		return 3
	case ScopeOrganization:
		return 4
	default:
		return -1
	}
}

// Role is one of the fixed roles of phase 4.
type Role string

const (
	RoleAdmin          Role = "admin"
	RoleServerOperator Role = "server_operator"
	RoleAppOperator    Role = "app_operator"
	RoleViewer         Role = "viewer"
	RoleMetricsOnly    Role = "metrics_only"
)

// Scope is the object a binding is attached to. For stack and container
// bindings Name is the stack or container name and ServerID is required.
type Scope struct {
	Type     ScopeType `json:"type"`
	ServerID string    `json:"server_id,omitempty"`
	GroupID  string    `json:"group_id,omitempty"`
	Name     string    `json:"name,omitempty"`
}

// Binding grants a role on a scope.
type Binding struct {
	Scope Scope `json:"scope"`
	Role  Role  `json:"role"`
}

// Target is the object being accessed. A container of a compose stack carries
// both Stack and Container; a container without a compose project carries only
// Container.
type Target struct {
	ServerID  string
	GroupID   string
	Stack     string
	Container string
}

// Effective is the set of bindings that apply to one user or API key.
type Effective struct {
	Bindings []Binding
	apiKey   bool
	owner    *Effective
}

// Resolve copies bindings into an Effective value.
func Resolve(bindings []Binding) Effective {
	copied := make([]Binding, len(bindings))
	copy(copied, bindings)
	return Effective{Bindings: copied}
}

// Permission.MinScope returns the smallest scope at which the permission can
// be granted. It returns an empty scope for an unknown permission.
func (p Permission) MinScope() ScopeType {
	switch p {
	case PermContainerView, PermContainerLogs, PermContainerControl, PermContainerTerminal, PermAlertsView:
		return ScopeContainer
	case PermStackView, PermStackDeploy, PermStackEdit, PermContainerRemove, PermAlertsSilence:
		return ScopeStack
	case PermServerView, PermResourcesManage, PermHostServicesView, PermHostServicesControl,
		PermHostJournal, PermHostAptView, PermHostAptUpgrade, PermHostReboot,
		PermHostTerminal, PermServerAgentUpdate:
		return ScopeServer
	case PermServerRemove, PermServerEnroll, PermUsersManage, PermSettingsManage,
		PermAuditView, PermRecordingsView, PermAPIKeysManage, PermAlertsManage:
		return ScopeOrganization
	default:
		return ""
	}
}

// Valid reports whether p is a known permission.
func (p Permission) Valid() bool {
	return p.MinScope() != ""
}

// Role.Permissions returns the fixed permission set of a role. The minimum
// scope rule still applies when the role is used in a binding.
func (r Role) Permissions() []Permission {
	switch r {
	case RoleAdmin:
		return append([]Permission(nil), AllPermissions...)
	case RoleServerOperator:
		return []Permission{
			PermContainerView, PermContainerLogs, PermContainerControl, PermContainerTerminal,
			PermStackView, PermStackDeploy, PermStackEdit, PermContainerRemove,
			PermServerView, PermResourcesManage, PermHostServicesView, PermHostServicesControl,
			PermHostJournal, PermHostAptView, PermHostAptUpgrade, PermHostReboot,
			PermHostTerminal, PermServerAgentUpdate, PermAlertsView, PermAlertsSilence,
		}
	case RoleAppOperator:
		return allViewPermissions(
			PermContainerLogs, PermContainerControl, PermStackDeploy, PermAlertsSilence,
		)
	case RoleViewer:
		return allViewPermissions(
			PermContainerLogs, PermHostJournal, PermHostAptView, PermHostServicesView,
		)
	case RoleMetricsOnly:
		return []Permission{PermServerView, PermStackView, PermContainerView, PermAlertsView}
	default:
		return nil
	}
}

func allViewPermissions(extra ...Permission) []Permission {
	permissions := make([]Permission, 0, 8+len(extra))
	for _, permission := range AllPermissions {
		// Recordings hold the output of root shells, so reading them stays an
		// administrator power even though the permission is named like a view.
		if permission == PermRecordingsView {
			continue
		}
		if len(permission) >= 5 && string(permission)[len(permission)-5:] == ".view" {
			permissions = append(permissions, permission)
		}
	}
	for _, permission := range extra {
		if !containsPermission(permissions, permission) {
			permissions = append(permissions, permission)
		}
	}
	return permissions
}

func containsPermission(permissions []Permission, wanted Permission) bool {
	for _, permission := range permissions {
		if permission == wanted {
			return true
		}
	}
	return false
}

func (r Role) grants(permission Permission) bool {
	for _, granted := range r.Permissions() {
		if granted == permission {
			return true
		}
	}
	return false
}

func (r Role) known() bool {
	switch r {
	case RoleAdmin, RoleServerOperator, RoleAppOperator, RoleViewer, RoleMetricsOnly:
		return true
	default:
		return false
	}
}

func (b Binding) appliesTo(t Target) bool {
	switch b.Scope.Type {
	case ScopeOrganization:
		return true
	case ScopeGroup:
		return b.Scope.GroupID != "" && t.GroupID == b.Scope.GroupID
	case ScopeServer:
		return b.Scope.ServerID != "" && t.ServerID == b.Scope.ServerID
	case ScopeStack:
		return b.Scope.ServerID != "" && b.Scope.Name != "" &&
			t.ServerID == b.Scope.ServerID && t.Stack == b.Scope.Name
	case ScopeContainer:
		return b.Scope.ServerID != "" && b.Scope.Name != "" &&
			t.ServerID == b.Scope.ServerID && t.Container == b.Scope.Name
	default:
		return false
	}
}

// Can reports whether any binding grants permission on target. A role only
// grants a permission when the binding scope is at least the permission's
// minimum scope, so a stack-bound server operator cannot reach the host.
func (e Effective) Can(permission Permission, target Target) bool {
	if e.apiKey {
		if !AllowedForAPIKey(permission) || !e.owner.Can(permission, target) {
			return false
		}
	}
	min := permission.MinScope()
	if min == "" {
		return false
	}
	for _, binding := range e.Bindings {
		if !binding.Role.known() || !binding.Role.grants(permission) {
			continue
		}
		if binding.Scope.Type.Level() < min.Level() {
			continue
		}
		if binding.appliesTo(target) {
			return true
		}
	}
	return false
}

// CanSeeServer reports whether any binding reaches the server. Stack and
// container bindings also reveal their server so a stack-only user can find it
// in the server list.
func (e Effective) CanSeeServer(serverID, groupID string) bool {
	if e.apiKey && !e.owner.CanSeeServer(serverID, groupID) {
		return false
	}
	for _, binding := range e.Bindings {
		if !binding.Role.known() {
			continue
		}
		switch binding.Scope.Type {
		case ScopeOrganization:
			return true
		case ScopeGroup:
			if binding.Scope.GroupID != "" && binding.Scope.GroupID == groupID {
				return true
			}
		case ScopeServer:
			if binding.Scope.ServerID != "" && binding.Scope.ServerID == serverID {
				return true
			}
		case ScopeStack, ScopeContainer:
			if binding.Scope.ServerID != "" && binding.Scope.Name != "" && binding.Scope.ServerID == serverID {
				return true
			}
		}
	}
	return false
}

// VisibleStacks reports whether every stack on the server is visible, or the
// names of the stacks that are visible. Container bindings do not reveal the
// stack they belong to, so they contribute no stack name.
func (e Effective) VisibleStacks(serverID string) (bool, []string) {
	if e.apiKey {
		all, names := e.bindingsVisibleStacks(serverID)
		ownerAll, ownerNames := e.owner.VisibleStacks(serverID)
		return intersectVisibility(all, names, ownerAll, ownerNames)
	}
	return e.bindingsVisibleStacks(serverID)
}

func (e Effective) bindingsVisibleStacks(serverID string) (bool, []string) {
	names := make(map[string]bool)
	for _, binding := range e.Bindings {
		if !binding.Role.known() {
			continue
		}
		switch binding.Scope.Type {
		case ScopeOrganization:
			return true, nil
		case ScopeServer:
			if binding.Scope.ServerID == serverID {
				return true, nil
			}
		case ScopeStack:
			if binding.Scope.ServerID == serverID && binding.Scope.Name != "" {
				names[binding.Scope.Name] = true
			}
		}
	}
	return false, sortedNames(names)
}

// VisibleContainers reports whether every container on the server is visible,
// or the names of the containers that are visible. A stack binding covers all
// containers of that stack but cannot enumerate them here.
func (e Effective) VisibleContainers(serverID string) (bool, []string) {
	if e.apiKey {
		all, names := e.bindingsVisibleContainers(serverID)
		ownerAll, ownerNames := e.owner.VisibleContainers(serverID)
		return intersectVisibility(all, names, ownerAll, ownerNames)
	}
	return e.bindingsVisibleContainers(serverID)
}

func (e Effective) bindingsVisibleContainers(serverID string) (bool, []string) {
	names := make(map[string]bool)
	for _, binding := range e.Bindings {
		if !binding.Role.known() {
			continue
		}
		switch binding.Scope.Type {
		case ScopeOrganization:
			return true, nil
		case ScopeServer:
			if binding.Scope.ServerID == serverID {
				return true, nil
			}
		case ScopeContainer:
			if binding.Scope.ServerID == serverID && binding.Scope.Name != "" {
				names[binding.Scope.Name] = true
			}
		}
	}
	return false, sortedNames(names)
}

func sortedNames(names map[string]bool) []string {
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// Limit returns the effective permissions of an API key: the key keeps only
// permissions granted by both its own bindings and the owner's current
// bindings, and the API-key blacklist always applies.
func (e Effective) Limit(owner Effective) Effective {
	ownerCopy := owner
	return Effective{Bindings: e.Bindings, apiKey: true, owner: &ownerCopy}
}

func intersectVisibility(all bool, names []string, ownerAll bool, ownerNames []string) (bool, []string) {
	if all && ownerAll {
		return true, nil
	}
	if all {
		return false, append([]string(nil), ownerNames...)
	}
	if ownerAll {
		return false, append([]string(nil), names...)
	}
	ownerSet := make(map[string]bool, len(ownerNames))
	for _, name := range ownerNames {
		ownerSet[name] = true
	}
	result := make([]string, 0, len(names))
	for _, name := range names {
		if ownerSet[name] {
			result = append(result, name)
		}
	}
	return false, result
}

// Summary is the JSON-friendly form of Effective for a future /me endpoint.
type Summary struct {
	Bindings []Binding `json:"bindings"`
}

// Summary returns the resolved bindings in a form suitable for JSON.
func (e Effective) Summary() Summary {
	bindings := make([]Binding, len(e.Bindings))
	copy(bindings, e.Bindings)
	return Summary{Bindings: bindings}
}

// AllowedForAPIKey reports whether an API key may use the permission. API keys
// never get interactive or settings-level powers.
func AllowedForAPIKey(permission Permission) bool {
	switch permission {
	case PermContainerTerminal, PermHostTerminal, PermUsersManage, PermSettingsManage,
		PermHostReboot, PermAPIKeysManage, PermAlertsManage:
		return false
	default:
		return true
	}
}
