package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/go-chi/chi/v5"
)

type alertJSON struct {
	ID          string  `json:"id"`
	RuleID      *string `json:"rule_id"`
	RuleType    string  `json:"rule_type"`
	Severity    string  `json:"severity"`
	State       string  `json:"state"`
	ServerID    *string `json:"server_id"`
	ServerName  string  `json:"server_name"`
	SubjectKind string  `json:"subject_kind"`
	SubjectName string  `json:"subject_name"`
	Stack       string  `json:"stack"`
	StartedAt   any     `json:"started_at"`
	ResolvedAt  any     `json:"resolved_at"`
	AckedAt     any     `json:"acked_at"`
	AckedBy     string  `json:"acked_by"`
	Silenced    bool    `json:"silenced"`
	Summary     any     `json:"summary"`
}

func alertEntry(a store.Alert, name string) alertJSON {
	return alertJSON{a.ID, a.RuleID, a.RuleType, a.Severity, a.State, a.ServerID, name, a.SubjectKind, a.SubjectName, a.Stack, a.StartedAt, a.ResolvedAt, a.AckedAt, a.AckedBy, a.Silenced, a.Summary}
}
func visibleAlert(e rbac.Effective, a store.Alert, groups map[string]string) bool {
	return e.Can(rbac.PermAlertsView, alertTarget(a, alertGroup(a, groups)))
}
func alertGroup(a store.Alert, groups map[string]string) string {
	if a.ServerID == nil {
		return ""
	}
	return groups[*a.ServerID]
}

type alertServer struct{ name, groupID string }

func alertServers(d AlertsDeps, r *http.Request) (map[string]alertServer, error) {
	ss, err := d.Store.ListServers(r.Context())
	if err != nil {
		return nil, err
	}
	out := map[string]alertServer{}
	for _, s := range ss {
		out[s.ID] = alertServer{name: s.Name, groupID: s.GroupID}
	}
	return out, nil
}

func NewAlertsHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e, ok := effectiveFromRequest(r)
		if !ok {
			writeError(w, 500, "internal")
			return
		}
		state := r.URL.Query().Get("state")
		if state == "" {
			state = "active"
		}
		var rows []store.Alert
		var err error
		switch state {
		case "active":
			rows, err = d.Store.ListActiveAlerts(r.Context())
		case "history":
			limit := 50
			if r.URL.Query().Has("limit") {
				limit, err = queryInt(r, "limit")
			}
			if err != nil {
				writeError(w, 400, "invalid_limit")
				return
			}
			if limit < 1 || limit > 200 {
				writeError(w, 400, "invalid_limit")
				return
			}
			offset, parseErr := queryInt(r, "offset")
			if parseErr != nil || offset < 0 {
				writeError(w, 400, "invalid_offset")
				return
			}
			var sid *string
			if v := r.URL.Query().Get("server_id"); v != "" {
				sid = &v
			}
			rows, err = d.Store.ListHistory(r.Context(), limit, offset, sid)
		default:
			writeError(w, 400, "invalid_state")
			return
		}
		if err != nil {
			internal(w, "list alerts", err)
			return
		}
		servers, err := alertServers(d, r)
		if err != nil {
			internal(w, "list servers", err)
			return
		}
		out := []alertJSON{}
		groups := alertGroupMap(servers)
		for _, a := range rows {
			if !visibleAlert(e, a, groups) {
				continue
			}
			if filter := r.URL.Query().Get("server_id"); filter != "" && (a.ServerID == nil || *a.ServerID != filter) {
				continue
			}
			n := ""
			if a.ServerID != nil {
				n = servers[*a.ServerID].name
			}
			out = append(out, alertEntry(a, n))
		}
		writeJSON(w, 200, out)
	})
}
func queryInt(r *http.Request, key string) (int, error) {
	if r.URL.Query().Get(key) == "" {
		return 0, nil
	}
	return strconv.Atoi(r.URL.Query().Get(key))
}
func alertGroupMap(servers map[string]alertServer) map[string]string {
	groups := make(map[string]string, len(servers))
	for id, server := range servers {
		groups[id] = server.groupID
	}
	return groups
}

func NewAlertSummaryHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e, ok := effectiveFromRequest(r)
		if !ok {
			writeError(w, 500, "internal")
			return
		}
		rows, err := d.Store.ListActiveAlerts(r.Context())
		if err != nil {
			internal(w, "list alerts", err)
			return
		}
		servers, err := alertServers(d, r)
		if err != nil {
			internal(w, "list servers", err)
			return
		}
		groups := alertGroupMap(servers)
		counts := map[string]int{}
		highest := map[string]string{}
		rank := map[string]int{"info": 1, "warning": 2, "critical": 3}
		for _, a := range rows {
			if !visibleAlert(e, a, groups) {
				continue
			}
			counts[a.Severity]++
			if a.ServerID == nil {
				continue
			}
			id := *a.ServerID
			if rank[a.Severity] > rank[highest[id]] {
				highest[id] = a.Severity
			}
		}
		writeJSON(w, 200, map[string]any{"counts": counts, "servers": highest})
	})
}

func NewAlertAckHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := loadAlert(w, r, d)
		if !ok {
			return
		}
		t := alertTarget(a, "")
		if a.ServerID != nil {
			server, err := d.Store.GetServer(r.Context(), *a.ServerID)
			if err != nil {
				internal(w, "get alert server", err)
				return
			}
			t = alertTarget(a, server.GroupID)
		}
		if !authorize(r, rbac.PermAlertsSilence, t) {
			denyRequest(w, r, rbac.PermAlertsSilence, t, 403, "forbidden")
			return
		}
		s, _ := sessionFromContext(r.Context())
		if err := d.Store.AckAlert(r.Context(), a.ID, s.User.Username, d.now()); err != nil {
			internal(w, "ack alert", err)
			return
		}
		auditAlert(d, r, "alert_ack", t, map[string]any{"alert_id": a.ID})
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
}
func loadAlert(w http.ResponseWriter, r *http.Request, d AlertsDeps) (store.Alert, bool) {
	a, err := d.Store.GetAlert(r.Context(), chi.URLParam(r, "alertID"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, 404, "not_found")
		} else {
			internal(w, "get alert", err)
		}
		return a, false
	}
	e, ok := effectiveFromRequest(r)
	groups := map[string]string{}
	if a.ServerID != nil {
		server, err := d.Store.GetServer(r.Context(), *a.ServerID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, 404, "not_found")
			} else {
				internal(w, "get alert server", err)
			}
			return a, false
		}
		groups[*a.ServerID] = server.GroupID
	}
	if !ok || !visibleAlert(e, a, groups) {
		writeError(w, 404, "not_found")
		return a, false
	}
	return a, true
}
func NewAlertUnwatchHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := loadAlert(w, r, d)
		if !ok {
			return
		}
		if a.SubjectKind != "container" {
			writeError(w, 400, "invalid_alert_target")
			return
		}
		t := alertTarget(a, "")
		if a.ServerID != nil {
			server, err := d.Store.GetServer(r.Context(), *a.ServerID)
			if err != nil {
				internal(w, "get alert server", err)
				return
			}
			t = alertTarget(a, server.GroupID)
		}
		if !authorize(r, rbac.PermAlertsSilence, t) {
			denyRequest(w, r, rbac.PermAlertsSilence, t, 403, "forbidden")
			return
		}
		if d.Hooks != nil {
			d.Hooks.Unwatch(r.Context(), t.ServerID, a.SubjectName)
		}
		if err := d.Store.ResolveAlert(r.Context(), a.ID, d.now()); err != nil {
			internal(w, "resolve alert", err)
			return
		}
		auditAlert(d, r, "alert_unwatch", t, map[string]any{"alert_id": a.ID})
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
}
func auditAlert(d AlertsDeps, r *http.Request, action string, t rbac.Target, details map[string]any) {
	if d.Audit == nil {
		return
	}
	s, _ := sessionFromContext(r.Context())
	_ = d.Audit.AppendAudit(r.Context(), store.AuditEvent{Actor: s.User.Username, ActorID: s.User.ID, Action: action, IP: clientIP(r), Result: "success", Scope: map[string]any{"server_id": t.ServerID, "stack": t.Stack}, Details: details})
}
