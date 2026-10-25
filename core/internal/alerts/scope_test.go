package alerts

import (
	"github.com/catlinux/Talaia/core/internal/store"
	"testing"
)

func TestRuleScopeMatching(t *testing.T) {
	group, server, name := "group-1", "server-1", "shop"
	sub := Subject{ServerID: server, GroupID: group, Stack: name, Container: "web"}
	for _, tt := range []struct {
		name, scope     string
		sid, gid, sname *string
		want            bool
	}{
		{"group match", "group", nil, &group, nil, true}, {"group miss", "group", nil, ptr("other"), nil, false},
		{"server match", "server", &server, nil, nil, true}, {"stack match", "stack", nil, nil, &name, true},
		{"container match", "container", nil, nil, ptr("web"), true}, {"disabled", "organization", nil, nil, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := store.AlertRule{Enabled: tt.name != "disabled", ScopeType: tt.scope, ScopeServerID: tt.sid, ScopeGroupID: tt.gid, ScopeName: tt.sname}
			if got := ruleMatches(r, sub); got != tt.want {
				t.Fatalf("matches=%v want %v", got, tt.want)
			}
		})
	}
}

func TestRuleAppliesShadowing(t *testing.T) {
	server, otherServer, group, stack, container, check := "server-1", "server-2", "group-1", "shop", "web", "check-1"
	orgCPU := store.AlertRule{ID: "org", Type: "cpu_high", Severity: "warning", Enabled: true, ScopeType: "organization"}
	orgDiskWarning := store.AlertRule{ID: "org-disk", Type: "disk_high", Severity: "warning", Enabled: true, ScopeType: "organization"}
	groupCPU := store.AlertRule{ID: "group", Type: "cpu_high", Severity: "warning", Enabled: true, ScopeType: "group", ScopeGroupID: &group}
	serverCPU := store.AlertRule{ID: "server", Type: "cpu_high", Severity: "warning", Enabled: true, ScopeType: "server", ScopeServerID: &server}
	serverCPU2 := store.AlertRule{ID: "server-2", Type: "cpu_high", Severity: "warning", Enabled: true, ScopeType: "server", ScopeServerID: &server}
	disabledServerCPU := store.AlertRule{ID: "server-off", Type: "cpu_high", Severity: "warning", Enabled: false, ScopeType: "server", ScopeServerID: &server}
	otherServerCPU := store.AlertRule{ID: "other-server", Type: "cpu_high", Severity: "warning", Enabled: true, ScopeType: "server", ScopeServerID: &otherServer}
	containerCPU := store.AlertRule{ID: "container", Type: "cpu_high", Severity: "warning", Enabled: true, ScopeType: "container", ScopeServerID: &server, ScopeName: &container}
	serverDiskCritical := store.AlertRule{ID: "server-disk", Type: "disk_high", Severity: "critical", Enabled: true, ScopeType: "server", ScopeServerID: &server}
	orgCheck := store.AlertRule{ID: "org-check", Type: "check_failed", Severity: "critical", Enabled: true, ScopeType: "organization"}
	serverCheck := store.AlertRule{ID: "server-check", Type: "check_failed", Severity: "critical", Enabled: true, ScopeType: "server", ScopeServerID: &server}
	serverSubject := Subject{ServerID: server, GroupID: group, Kind: "server", Name: server}
	otherSubject := Subject{ServerID: otherServer, GroupID: group, Kind: "server", Name: otherServer}
	containerSubject := Subject{ServerID: server, GroupID: group, Stack: stack, Container: container, Kind: "container", Name: container}
	checkSubject := Subject{ServerID: server, GroupID: group, CheckID: check, Kind: "check", Name: check}

	tests := []struct {
		name    string
		rules   []store.AlertRule
		ruleID  string
		subject Subject
		want    bool
	}{
		{"organization fires without override", []store.AlertRule{orgCPU}, "org", serverSubject, true},
		{"server replaces organization", []store.AlertRule{orgCPU, serverCPU}, "org", serverSubject, false},
		{"server rule fires", []store.AlertRule{orgCPU, serverCPU}, "server", serverSubject, true},
		{"server override leaves other servers", []store.AlertRule{orgCPU, serverCPU}, "org", otherSubject, true},
		{"disabled server override shadows organization", []store.AlertRule{orgCPU, disabledServerCPU}, "org", serverSubject, false},
		{"disabled server override leaves other servers", []store.AlertRule{orgCPU, disabledServerCPU}, "org", otherSubject, true},
		{"server replaces group", []store.AlertRule{orgCPU, groupCPU, serverCPU}, "group", serverSubject, false},
		{"server rule wins over group", []store.AlertRule{orgCPU, groupCPU, serverCPU}, "server", serverSubject, true},
		{"container replaces server", []store.AlertRule{orgCPU, serverCPU, containerCPU}, "server", containerSubject, false},
		{"container rule fires", []store.AlertRule{orgCPU, serverCPU, containerCPU}, "container", containerSubject, true},
		{"different severities coexist", []store.AlertRule{orgDiskWarning, serverDiskCritical}, "org-disk", serverSubject, true},
		{"same specificity does not shadow", []store.AlertRule{serverCPU, serverCPU2}, "server", serverSubject, true},
		{"second same-specificity rule also fires", []store.AlertRule{serverCPU, serverCPU2}, "server-2", serverSubject, true},
		{"non-matching server override does not shadow", []store.AlertRule{orgCPU, otherServerCPU}, "org", serverSubject, true},
		{"server replaces organization for a check", []store.AlertRule{orgCheck, serverCheck}, "org-check", checkSubject, false},
		{"server check rule fires", []store.AlertRule{orgCheck, serverCheck}, "server-check", checkSubject, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rule store.AlertRule
			for _, candidate := range tt.rules {
				if candidate.ID == tt.ruleID {
					rule = candidate
					break
				}
			}
			if got := ruleApplies(tt.rules, rule, tt.subject); got != tt.want {
				t.Fatalf("applies=%v want %v", got, tt.want)
			}
		})
	}
}

func ptr(v string) *string { return &v }
