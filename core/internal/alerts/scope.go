package alerts

import "github.com/catlinux/Talaia/core/internal/store"

// Subject is the live object evaluated by an alert rule.
type Subject struct {
	ServerID, GroupID, Stack, Container, CheckID string
	Kind, Name                                   string
	// Label is the name people know the subject by when Name is an id (a check).
	Label string
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

// scopeSpecificity orders scopes from broadest to narrowest.
func scopeSpecificity(scopeType string) int {
	switch scopeType {
	case "organization", "":
		return 0
	case "group":
		return 1
	case "server":
		return 2
	case "stack":
		return 3
	case "container":
		return 4
	default:
		return -1
	}
}

// ruleApplies reports whether r applies to s. A rule is shadowed by a matching
// rule of the same type and severity with a narrower scope, even when that rule
// is disabled: disabling an override is how one subject turns a rule off.
func ruleApplies(rules []store.AlertRule, r store.AlertRule, s Subject) bool {
	if !ruleMatches(r, s) {
		return false
	}
	specificity := scopeSpecificity(r.ScopeType)
	for _, other := range rules {
		if other.ID == r.ID ||
			other.Type != r.Type ||
			other.Severity != r.Severity ||
			scopeSpecificity(other.ScopeType) <= specificity {
			continue
		}
		if matchesScope(other.ScopeType, other.ScopeServerID, other.ScopeGroupID, other.ScopeName, s) {
			return false
		}
	}
	return true
}

func silenceMatches(v store.Silence, r store.AlertRule, s Subject) bool {
	if v.RuleType != nil && *v.RuleType != r.Type {
		return false
	}
	return matchesScope(v.ScopeType, v.ScopeServerID, v.ScopeGroupID, v.ScopeName, s)
}
