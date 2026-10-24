package alerts

import "github.com/catlinux/Talaia/core/internal/store"

// Subject is the live object evaluated by an alert rule.
type Subject struct {
	ServerID, GroupID, Stack, Container, CheckID string
	Kind, Name                                   string
}

func matchesScope(scopeType string, serverID, groupID, name *string, s Subject) bool {
	switch scopeType {
	case "organization", "":
		return true
	case "server":
		return serverID != nil && *serverID == s.ServerID
	case "group":
		return groupID != nil && *groupID == s.GroupID
	case "stack":
		return name != nil && *name == s.Stack
	case "container":
		return name != nil && *name == s.Container
	default:
		return false
	}
}

func ruleMatches(r store.AlertRule, s Subject) bool {
	return r.Enabled && matchesScope(r.ScopeType, r.ScopeServerID, r.ScopeGroupID, r.ScopeName, s)
}

func silenceMatches(v store.Silence, r store.AlertRule, s Subject) bool {
	if v.RuleType != nil && *v.RuleType != r.Type {
		return false
	}
	return matchesScope(v.ScopeType, v.ScopeServerID, v.ScopeGroupID, v.ScopeName, s)
}
