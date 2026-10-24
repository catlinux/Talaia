package rbac

import (
	"encoding/json"
	"testing"
)

type scopeCase struct {
	name   string
	scope  Scope
	target Target
}

func scopeCases() []scopeCase {
	target := Target{ServerID: "s1", GroupID: "g1", Stack: "st1", Container: "ct1"}
	return []scopeCase{
		{name: "organization", scope: Scope{Type: ScopeOrganization}, target: target},
		{name: "group", scope: Scope{Type: ScopeGroup, GroupID: "g1"}, target: target},
		{name: "server", scope: Scope{Type: ScopeServer, ServerID: "s1"}, target: target},
		{name: "stack", scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "st1"}, target: target},
		{name: "container", scope: Scope{Type: ScopeContainer, ServerID: "s1", Name: "ct1"}, target: target},
	}
}

func roleGrants(role Role, permission Permission) bool {
	for _, granted := range role.Permissions() {
		if granted == permission {
			return true
		}
	}
	return false
}

func TestEveryRoleScopeAndPermission(t *testing.T) {
	roles := []Role{RoleAdmin, RoleServerOperator, RoleAppOperator, RoleViewer, RoleMetricsOnly, Role("unknown")}
	for _, role := range roles {
		for _, scenario := range scopeCases() {
			effective := Resolve([]Binding{{Scope: scenario.scope, Role: role}})
			for _, permission := range AllPermissions {
				min := permission.MinScope()
				want := roleGrants(role, permission) &&
					scenario.scope.Type.Level() >= min.Level() &&
					effective.Bindings[0].appliesTo(scenario.target)
				if got := effective.Can(permission, scenario.target); got != want {
					t.Errorf("role=%s scope=%s permission=%s: Can=%v want %v (min=%s)",
						role, scenario.name, permission, got, want, min)
				}
			}
		}
	}
}

func TestMinimumLevelExamples(t *testing.T) {
	target := Target{ServerID: "s1", GroupID: "g1", Stack: "st1", Container: "ct1"}

	stackOperator := Resolve([]Binding{{
		Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "st1"},
		Role:  RoleServerOperator,
	}})
	for _, permission := range []Permission{PermHostServicesView, PermHostAptView, PermHostJournal,
		PermHostAptUpgrade, PermHostReboot, PermHostTerminal, PermResourcesManage} {
		if stackOperator.Can(permission, target) {
			t.Errorf("stack-bound server_operator got %s", permission)
		}
	}
	if !stackOperator.Can(PermContainerView, target) || !stackOperator.Can(PermStackEdit, target) {
		t.Error("stack-bound server_operator lost a stack or container permission")
	}

	containerOperator := Resolve([]Binding{{
		Scope: Scope{Type: ScopeContainer, ServerID: "s1", Name: "ct1"},
		Role:  RoleServerOperator,
	}})
	if containerOperator.Can(PermStackView, target) || containerOperator.Can(PermContainerRemove, target) {
		t.Error("container-bound operator got a stack permission")
	}
	if !containerOperator.Can(PermContainerTerminal, target) {
		t.Error("container-bound operator lost container.terminal")
	}

	app := Resolve([]Binding{{Scope: Scope{Type: ScopeOrganization}, Role: RoleAppOperator}})
	if app.Can(PermStackEdit, target) {
		t.Error("app_operator got stack.edit")
	}
	if app.Can(PermContainerTerminal, target) {
		t.Error("app_operator got container.terminal")
	}
	if !app.Can(PermStackDeploy, target) || !app.Can(PermContainerControl, target) {
		t.Error("app_operator lost a deploy or control permission")
	}

	viewer := Resolve([]Binding{{Scope: Scope{Type: ScopeServer, ServerID: "s1"}, Role: RoleViewer}})
	if !viewer.Can(PermHostJournal, target) || viewer.Can(PermHostServicesControl, target) {
		t.Error("viewer host permissions are wrong")
	}
	if viewer.Can(PermRecordingsView, target) {
		// Recordings are organization-level; a server binding must not reach them.
		t.Error("viewer reached recordings.view at server scope")
	}
}

func TestBindingsDoNotEscapeTheirScope(t *testing.T) {
	effective := Resolve([]Binding{
		{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "st1"}, Role: RoleServerOperator},
		{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "st2"}, Role: RoleViewer},
	})

	if !effective.Can(PermContainerView, Target{ServerID: "s1", Stack: "st1", Container: "ct1"}) {
		t.Fatal("the stack binding did not reach its own container")
	}
	for _, target := range []Target{
		{ServerID: "s2", Stack: "st1", Container: "ct1"},
		{ServerID: "s1", Stack: "other", Container: "ct1"},
		{ServerID: "s1"},
	} {
		if effective.Can(PermContainerView, target) || effective.Can(PermStackView, target) {
			t.Fatalf("a binding reached an object outside its scope: %+v", target)
		}
	}
	if effective.Can(PermHostJournal, Target{ServerID: "s1", Stack: "st1", Container: "ct1"}) {
		t.Fatal("a stack binding reached a server permission")
	}
}

func TestOrganizationAdminCanDoEverything(t *testing.T) {
	effective := Resolve([]Binding{{Scope: Scope{Type: ScopeOrganization}, Role: RoleAdmin}})
	target := Target{ServerID: "s1", GroupID: "g1", Stack: "st1", Container: "ct1"}
	for _, permission := range AllPermissions {
		if !effective.Can(permission, target) {
			t.Errorf("organization admin cannot %s", permission)
		}
	}
	if effective.Can(Permission("unknown.permission"), target) {
		t.Fatal("unknown permission was granted")
	}
}

func TestGroupBindingAppliesOnlyToItsGroup(t *testing.T) {
	effective := Resolve([]Binding{{
		Scope: Scope{Type: ScopeGroup, GroupID: "g1"},
		Role:  RoleViewer,
	}})
	target := Target{ServerID: "s1", GroupID: "g1", Stack: "st1", Container: "ct1"}
	if !effective.Can(PermServerView, target) || !effective.Can(PermHostJournal, target) {
		t.Fatal("group binding did not apply inside its group")
	}
	if effective.Can(PermServerRemove, target) {
		t.Fatal("group binding reached an organization permission")
	}
	if effective.Can(PermServerView, Target{ServerID: "s2", GroupID: "g2"}) {
		t.Fatal("group binding applied to another group")
	}
}

func TestCanSeeServer(t *testing.T) {
	effective := Resolve([]Binding{
		{Scope: Scope{Type: ScopeGroup, GroupID: "g1"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeServer, ServerID: "s2"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeStack, ServerID: "s3", Name: "st1"}, Role: RoleAppOperator},
		{Scope: Scope{Type: ScopeContainer, ServerID: "s4", Name: "ct1"}, Role: RoleMetricsOnly},
		{Scope: Scope{Type: ScopeServer, ServerID: "s5"}, Role: Role("unknown")},
	})

	cases := []struct {
		server string
		group  string
		want   bool
	}{
		{"s1", "g1", true},
		{"s1", "g2", false},
		{"s2", "g9", true},
		{"s3", "g9", true},
		{"s4", "g9", true},
		{"s5", "g2", false},
		{"s9", "g2", false},
	}
	for _, test := range cases {
		if got := effective.CanSeeServer(test.server, test.group); got != test.want {
			t.Errorf("CanSeeServer(%s, %s) = %v, want %v", test.server, test.group, got, test.want)
		}
	}

	organization := Resolve([]Binding{{Scope: Scope{Type: ScopeOrganization}, Role: RoleMetricsOnly}})
	if !organization.CanSeeServer("anything", "") {
		t.Fatal("organization binding did not reveal every server")
	}
}

func TestVisibleStacks(t *testing.T) {
	effective := Resolve([]Binding{
		{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "st2"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "st1"}, Role: RoleAppOperator},
		{Scope: Scope{Type: ScopeContainer, ServerID: "s1", Name: "ct1"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeStack, ServerID: "s2", Name: "other"}, Role: RoleViewer},
	})
	all, names := effective.VisibleStacks("s1")
	if all {
		t.Fatal("partial stack visibility reported as all")
	}
	if len(names) != 2 || names[0] != "st1" || names[1] != "st2" {
		t.Fatalf("visible stacks = %v, want [st1 st2]", names)
	}

	for _, binding := range []Binding{
		{Scope: Scope{Type: ScopeOrganization}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeServer, ServerID: "s1"}, Role: RoleViewer},
	} {
		all, names = Resolve([]Binding{binding}).VisibleStacks("s1")
		if !all || names != nil {
			t.Fatalf("binding %+v: all=%v names=%v, want all", binding, all, names)
		}
	}
}

func TestVisibleContainers(t *testing.T) {
	effective := Resolve([]Binding{
		{Scope: Scope{Type: ScopeContainer, ServerID: "s1", Name: "ct2"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeContainer, ServerID: "s1", Name: "ct1"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "st1"}, Role: RoleViewer},
	})
	all, names := effective.VisibleContainers("s1")
	if all {
		t.Fatal("partial container visibility reported as all")
	}
	if len(names) != 2 || names[0] != "ct1" || names[1] != "ct2" {
		t.Fatalf("visible containers = %v, want [ct1 ct2]", names)
	}

	for _, binding := range []Binding{
		{Scope: Scope{Type: ScopeOrganization}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeServer, ServerID: "s1"}, Role: RoleViewer},
	} {
		all, names = Resolve([]Binding{binding}).VisibleContainers("s1")
		if !all || names != nil {
			t.Fatalf("binding %+v: all=%v names=%v, want all", binding, all, names)
		}
	}
}

func TestAllowedForAPIKey(t *testing.T) {
	for _, permission := range []Permission{
		PermContainerTerminal, PermHostTerminal, PermUsersManage, PermSettingsManage,
		PermHostReboot, PermAPIKeysManage,
	} {
		if AllowedForAPIKey(permission) {
			t.Errorf("api key allowed %s", permission)
		}
	}
	for _, permission := range []Permission{
		PermContainerView, PermContainerLogs, PermContainerControl, PermStackDeploy,
		PermServerView, PermHostAptView, PermAuditView,
	} {
		if !AllowedForAPIKey(permission) {
			t.Errorf("api key denied %s", permission)
		}
	}
}

func TestSummaryAndUnknownRole(t *testing.T) {
	effective := Resolve([]Binding{
		{Scope: Scope{Type: ScopeServer, ServerID: "s1"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "st1"}, Role: Role("unknown")},
	})
	summary := effective.Summary()
	if len(summary.Bindings) != 2 {
		t.Fatalf("summary bindings = %d, want 2", len(summary.Bindings))
	}
	if summary.Bindings[0].Role != RoleViewer || summary.Bindings[0].Scope.ServerID != "s1" {
		t.Fatalf("summary binding = %+v", summary.Bindings[0])
	}
	if _, err := json.Marshal(summary); err != nil {
		t.Fatalf("summary is not JSON friendly: %v", err)
	}

	unknown := Resolve([]Binding{{Scope: Scope{Type: ScopeOrganization}, Role: Role("unknown")}})
	if unknown.Can(PermServerView, Target{ServerID: "s1"}) {
		t.Fatal("unknown role granted a permission")
	}
	if unknown.CanSeeServer("s1", "g1") {
		t.Fatal("unknown role revealed a server")
	}
}

func TestAPIKeyLimitIntersectsOwnerAndBlacklist(t *testing.T) {
	keyOrgAdmin := Resolve([]Binding{{Scope: Scope{Type: ScopeOrganization}, Role: RoleAdmin}})
	ownerStack := Resolve([]Binding{{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "web"}, Role: RoleAppOperator}})
	limited := keyOrgAdmin.Limit(ownerStack)
	target := Target{ServerID: "s1", GroupID: "g1", Stack: "web", Container: "web-ct"}

	if !limited.Can(PermContainerView, target) {
		t.Fatal("key lost a permission both sides grant")
	}
	if limited.Can(PermStackEdit, target) {
		t.Fatal("key gained stack.edit from its admin binding, which the owner lacks")
	}
	if limited.Can(PermHostTerminal, target) || limited.Can(PermUsersManage, Target{}) ||
		limited.Can(PermSettingsManage, Target{}) || limited.Can(PermAPIKeysManage, Target{}) ||
		limited.Can(PermHostReboot, target) {
		t.Fatal("key gained a forbidden permission")
	}
	if limited.Can(PermServerView, target) {
		t.Fatal("key reached a server permission the stack owner lacks")
	}

	// An API key never gains a terminal permission even when both sides are
	// organization admins.
	bothAdmins := keyOrgAdmin.Limit(keyOrgAdmin)
	if bothAdmins.Can(PermContainerTerminal, target) || bothAdmins.Can(PermHostTerminal, target) {
		t.Fatal("admin API key got a terminal permission")
	}

	// The owner's current bindings are read on every check.
	ownerAll := Resolve([]Binding{{Scope: Scope{Type: ScopeServer, ServerID: "s1"}, Role: RoleServerOperator}})
	limited = keyOrgAdmin.Limit(ownerAll)
	if !limited.Can(PermHostServicesControl, target) {
		t.Fatal("key lost a permission the owner has")
	}
	if !limited.CanSeeServer("s1", "g1") {
		t.Fatal("key cannot see a server the owner can see")
	}
	if limited.CanSeeServer("s2", "g2") {
		t.Fatal("key saw a server the owner cannot see")
	}
}

func TestAPIKeyLimitVisibilityIntersection(t *testing.T) {
	key := Resolve([]Binding{
		{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "web"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "db"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeContainer, ServerID: "s1", Name: "one"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeContainer, ServerID: "s1", Name: "two"}, Role: RoleViewer},
	}).Limit(Resolve([]Binding{
		{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "db"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeContainer, ServerID: "s1", Name: "two"}, Role: RoleViewer},
		{Scope: Scope{Type: ScopeContainer, ServerID: "s1", Name: "three"}, Role: RoleViewer},
	}))
	all, names := key.VisibleStacks("s1")
	if all || len(names) != 1 || names[0] != "db" {
		t.Fatalf("limited stacks = %v %v, want only db", all, names)
	}
	all, names = key.VisibleContainers("s1")
	if all || len(names) != 1 || names[0] != "two" {
		t.Fatalf("limited containers = %v %v, want only two", all, names)
	}

	keyAll := Resolve([]Binding{{Scope: Scope{Type: ScopeServer, ServerID: "s1"}, Role: RoleViewer}}).Limit(
		Resolve([]Binding{{Scope: Scope{Type: ScopeStack, ServerID: "s1", Name: "web"}, Role: RoleViewer}}))
	all, names = keyAll.VisibleStacks("s1")
	if all || len(names) != 1 || names[0] != "web" {
		t.Fatalf("owner-limited stacks = %v %v", all, names)
	}
}

func TestRecordingsAreAdminOnly(t *testing.T) {
	for _, role := range []Role{RoleServerOperator, RoleAppOperator, RoleViewer, RoleMetricsOnly} {
		effective := Resolve([]Binding{{Scope: Scope{Type: ScopeOrganization}, Role: role}})
		if effective.Can(PermRecordingsView, Target{}) {
			t.Errorf("%s on the organization can read terminal recordings", role)
		}
	}
	admin := Resolve([]Binding{{Scope: Scope{Type: ScopeOrganization}, Role: RoleAdmin}})
	if !admin.Can(PermRecordingsView, Target{}) {
		t.Error("the administrator cannot read terminal recordings")
	}
}
