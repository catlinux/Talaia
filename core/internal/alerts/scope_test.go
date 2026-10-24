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
func ptr(v string) *string { return &v }
