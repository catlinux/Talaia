package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/go-chi/chi/v5"
)

type alertRuleJSON struct {
	ID                    string          `json:"id"`
	Name                  string          `json:"name"`
	Type                  string          `json:"type"`
	Enabled               bool            `json:"enabled"`
	Severity              string          `json:"severity"`
	Params                json.RawMessage `json:"params"`
	ScopeType             string          `json:"scope_type"`
	ServerID              *string         `json:"server_id"`
	GroupID               *string         `json:"group_id"`
	ScopeName             *string         `json:"scope_name"`
	ChannelIDs            []string        `json:"channel_ids"`
	RepeatIntervalSeconds int             `json:"repeat_interval_seconds"`
	Builtin               bool            `json:"builtin"`
}

func alertRuleEntry(v store.AlertRule) alertRuleJSON {
	return alertRuleJSON{v.ID, v.Name, v.Type, v.Enabled, v.Severity, v.Params, v.ScopeType, v.ScopeServerID, v.ScopeGroupID, v.ScopeName, v.ChannelIDs, v.RepeatIntervalSeconds, v.Builtin}
}

type alertRuleRequest struct {
	Name                  *string         `json:"name"`
	Type                  *string         `json:"type"`
	Enabled               *bool           `json:"enabled"`
	Severity              *string         `json:"severity"`
	Params                json.RawMessage `json:"params"`
	ScopeType             *string         `json:"scope_type"`
	ServerID              *string         `json:"server_id"`
	GroupID               *string         `json:"group_id"`
	ScopeName             *string         `json:"scope_name"`
	ChannelIDs            *[]string       `json:"channel_ids"`
	RepeatIntervalSeconds *int            `json:"repeat_interval_seconds"`
}

func NewAlertRulesHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.Store.ListAlertRules(r.Context())
		if err != nil {
			internal(w, "list alert rules", err)
			return
		}
		out := make([]alertRuleJSON, 0, len(rows))
		for _, row := range rows {
			out = append(out, alertRuleEntry(row))
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func NewAlertRuleCreateHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in alertRuleRequest
		if decodeJSON(w, r, &in) != nil {
			writeError(w, 400, "invalid_request")
			return
		}
		v := store.AlertRule{Enabled: true, ScopeType: "organization", Params: json.RawMessage(`{}`), ChannelIDs: []string{}}
		applyAlertRuleRequest(&v, in, true)
		if !validAlertRule(v) {
			writeError(w, 400, "invalid_alert_rule")
			return
		}
		if !validateAlertRuleScope(w, r, d, &v) || !validateAlertRuleChannels(w, r, d, v.ChannelIDs) {
			return
		}
		s, _ := sessionFromContext(r.Context())
		v.UpdatedBy = s.User.Username
		created, err := d.Store.CreateAlertRule(r.Context(), v)
		if err != nil {
			internal(w, "create alert rule", err)
			return
		}
		auditAlert(d, r, "alert_rule_create", rbac.Target{}, map[string]any{"name": created.Name, "type": created.Type})
		writeJSON(w, 201, alertRuleEntry(created))
	})
}

func NewAlertRuleUpdateHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "ruleID")
		v, err := d.Store.GetAlertRule(r.Context(), id)
		if err != nil {
			alertRuleStoreError(w, "get alert rule", err)
			return
		}
		var in alertRuleRequest
		if decodeJSON(w, r, &in) != nil {
			writeError(w, 400, "invalid_request")
			return
		}
		applyAlertRuleRequest(&v, in, false)
		if !validAlertRule(v) {
			writeError(w, 400, "invalid_alert_rule")
			return
		}
		if !validateAlertRuleScope(w, r, d, &v) || !validateAlertRuleChannels(w, r, d, v.ChannelIDs) {
			return
		}
		s, _ := sessionFromContext(r.Context())
		v.UpdatedBy = s.User.Username
		if err = d.Store.UpdateAlertRule(r.Context(), v); err != nil {
			alertRuleStoreError(w, "update alert rule", err)
			return
		}
		auditAlert(d, r, "alert_rule_update", rbac.Target{}, map[string]any{"name": v.Name, "type": v.Type})
		writeJSON(w, 200, alertRuleEntry(v))
	})
}

func NewAlertRuleDeleteHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "ruleID")
		v, err := d.Store.GetAlertRule(r.Context(), id)
		if err != nil {
			alertRuleStoreError(w, "get alert rule", err)
			return
		}
		if v.Builtin {
			writeError(w, 400, "builtin_rule")
			return
		}
		if err = d.Store.DeleteAlertRule(r.Context(), id); err != nil {
			alertRuleStoreError(w, "delete alert rule", err)
			return
		}
		auditAlert(d, r, "alert_rule_delete", rbac.Target{}, map[string]any{"name": v.Name, "type": v.Type})
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
}

func applyAlertRuleRequest(v *store.AlertRule, in alertRuleRequest, creating bool) {
	if in.Name != nil {
		v.Name = *in.Name
	}
	if in.Type != nil {
		v.Type = *in.Type
	}
	if in.Enabled != nil {
		v.Enabled = *in.Enabled
	}
	if in.Severity != nil {
		v.Severity = *in.Severity
	}
	if len(in.Params) > 0 {
		v.Params = in.Params
	}
	if in.ScopeType != nil && v.ScopeType != *in.ScopeType {
		v.ScopeServerID, v.ScopeGroupID, v.ScopeName = nil, nil, nil
	}
	if in.ScopeType != nil {
		v.ScopeType = *in.ScopeType
	}
	if in.ServerID != nil {
		if *in.ServerID == "" {
			v.ScopeServerID = nil
		} else {
			v.ScopeServerID = in.ServerID
		}
	}
	if in.GroupID != nil {
		if *in.GroupID == "" {
			v.ScopeGroupID = nil
		} else {
			v.ScopeGroupID = in.GroupID
		}
	}
	if in.ScopeName != nil {
		if *in.ScopeName == "" {
			v.ScopeName = nil
		} else {
			v.ScopeName = in.ScopeName
		}
	}
	if in.ChannelIDs != nil {
		v.ChannelIDs = *in.ChannelIDs
	}
	if in.RepeatIntervalSeconds != nil {
		v.RepeatIntervalSeconds = *in.RepeatIntervalSeconds
	}
	if creating {
		v.Builtin = false
	}
}

func validAlertRule(v store.AlertRule) bool {
	if strings.TrimSpace(v.Name) == "" || len(v.Name) > 200 || (v.Severity != "warning" && v.Severity != "critical") || v.RepeatIntervalSeconds < 0 || v.RepeatIntervalSeconds > 604800 {
		return false
	}
	limits := map[string][2]int{}
	switch v.Type {
	case "server_offline", "container_down", "container_unhealthy":
		limits["seconds"] = [2]int{0, 86400}
	case "cpu_high", "mem_high", "disk_high":
		limits["threshold"] = [2]int{1, 100}
		limits["for_seconds"] = [2]int{0, 86400}
	case "check_failed":
		limits["consecutive_failures"] = [2]int{1, 20}
	case "cert_expiry":
		limits["days"] = [2]int{1, 365}
	default:
		return false
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(v.Params, &params) != nil || params == nil || len(params) != len(limits) {
		return false
	}
	for key, bounds := range limits {
		var n int
		if raw, ok := params[key]; !ok || json.Unmarshal(raw, &n) != nil || n < bounds[0] || n > bounds[1] {
			return false
		}
	}
	return true
}

func validateAlertRuleScope(w http.ResponseWriter, r *http.Request, d AlertsDeps, v *store.AlertRule) bool {
	if !validAlertRuleScopeShape(*v) {
		writeError(w, 400, "invalid_scope")
		return false
	}
	switch v.ScopeType {
	case "organization":
		if v.ScopeServerID != nil || v.ScopeGroupID != nil || v.ScopeName != nil {
			writeError(w, 400, "invalid_scope")
			return false
		}
	case "server":
		if v.ScopeServerID == nil || v.ScopeGroupID != nil || v.ScopeName != nil {
			writeError(w, 400, "invalid_scope")
			return false
		}
		if _, err := d.Store.GetServer(r.Context(), *v.ScopeServerID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, 400, "invalid_scope")
			} else {
				internal(w, "get rule server", err)
			}
			return false
		}
	case "group":
		if v.ScopeGroupID == nil || v.ScopeServerID != nil || v.ScopeName != nil {
			writeError(w, 400, "invalid_scope")
			return false
		}
		if _, err := d.Store.GetGroup(r.Context(), *v.ScopeGroupID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, 400, "invalid_scope")
			} else {
				internal(w, "get rule group", err)
			}
			return false
		}
	case "stack", "container":
		if v.ScopeServerID == nil || v.ScopeGroupID != nil || v.ScopeName == nil || strings.TrimSpace(*v.ScopeName) == "" {
			writeError(w, 400, "invalid_scope")
			return false
		}
		if _, err := d.Store.GetServer(r.Context(), *v.ScopeServerID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, 400, "invalid_scope")
			} else {
				internal(w, "get rule server", err)
			}
			return false
		}
	default:
		writeError(w, 400, "invalid_scope")
		return false
	}
	return true
}

func validAlertRuleScopeShape(v store.AlertRule) bool {
	switch v.ScopeType {
	case "organization":
		return v.ScopeServerID == nil && v.ScopeGroupID == nil && v.ScopeName == nil
	case "server":
		return v.ScopeServerID != nil && *v.ScopeServerID != "" && v.ScopeGroupID == nil && v.ScopeName == nil
	case "group":
		return v.ScopeGroupID != nil && *v.ScopeGroupID != "" && v.ScopeServerID == nil && v.ScopeName == nil
	case "stack", "container":
		return v.ScopeServerID != nil && *v.ScopeServerID != "" && v.ScopeGroupID == nil && v.ScopeName != nil && strings.TrimSpace(*v.ScopeName) != ""
	default:
		return false
	}
}

func validateAlertRuleChannels(w http.ResponseWriter, r *http.Request, d AlertsDeps, ids []string) bool {
	rows, err := d.Store.ListNotificationChannels(r.Context())
	if err != nil {
		internal(w, "list notification channels", err)
		return false
	}
	known := make(map[string]bool, len(rows))
	for _, row := range rows {
		known[row.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !known[id] || seen[id] {
			writeError(w, 400, "invalid_channel")
			return false
		}
		seen[id] = true
	}
	return true
}

func alertRuleStoreError(w http.ResponseWriter, action string, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, 404, "not_found")
		return
	}
	if strings.Contains(err.Error(), "builtin alert rule") {
		writeError(w, 400, "builtin_rule")
		return
	}
	internal(w, action, fmt.Errorf("%s: %w", action, err))
}
