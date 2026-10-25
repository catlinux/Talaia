package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/go-chi/chi/v5"
)

type silenceJSON struct {
	ID        string    `json:"id"`
	ScopeType string    `json:"scope_type"`
	ServerID  *string   `json:"server_id"`
	GroupID   *string   `json:"group_id"`
	Name      *string   `json:"name"`
	RuleType  *string   `json:"rule_type"`
	Reason    string    `json:"reason"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	EndsAt    time.Time `json:"ends_at"`
}

func silenceEntry(s store.Silence) silenceJSON {
	return silenceJSON{s.ID, s.ScopeType, s.ScopeServerID, s.ScopeGroupID, s.ScopeName, s.RuleType, s.Reason, s.CreatedBy, s.CreatedAt, s.EndsAt}
}

func NewSilenceListHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e, ok := effectiveFromRequest(r)
		if !ok {
			writeError(w, 500, "internal")
			return
		}
		rows, err := d.Store.ListActiveSilences(r.Context(), d.now())
		if err != nil {
			internal(w, "list silences", err)
			return
		}
		servers, err := alertServers(d, r)
		if err != nil {
			internal(w, "list servers", err)
			return
		}
		out := []silenceJSON{}
		for _, s := range rows {
			t := silenceTarget(s, silenceGroup(s, servers))
			if e.Can(rbac.PermAlertsView, t) {
				out = append(out, silenceEntry(s))
			}
		}
		writeJSON(w, 200, out)
	})
}

type silenceRequest struct {
	ScopeType string  `json:"scope_type"`
	ServerID  string  `json:"server_id"`
	GroupID   string  `json:"group_id"`
	Name      string  `json:"name"`
	RuleType  *string `json:"rule_type"`
	Duration  int     `json:"duration_minutes"`
	Reason    string  `json:"reason"`
}

func NewSilenceCreateHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in silenceRequest
		if decodeJSON(w, r, &in) != nil {
			writeError(w, 400, "invalid_request")
			return
		}
		if in.Duration < 1 || in.Duration > 43200 || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 200 {
			writeError(w, 400, "invalid_silence")
			return
		}
		v := store.Silence{ScopeType: in.ScopeType, Reason: in.Reason, RuleType: in.RuleType, EndsAt: d.now().Add(time.Duration(in.Duration) * time.Minute)}
		t := rbac.Target{}
		switch in.ScopeType {
		case "server":
			if in.ServerID == "" {
				writeError(w, 400, "invalid_scope")
				return
			}
			v.ScopeServerID = &in.ServerID
			t.ServerID = in.ServerID
		case "group":
			if in.GroupID == "" {
				writeError(w, 400, "invalid_scope")
				return
			}
			v.ScopeGroupID = &in.GroupID
			t.GroupID = in.GroupID
		case "stack":
			if in.ServerID == "" || in.Name == "" {
				writeError(w, 400, "invalid_scope")
				return
			}
			v.ScopeServerID = &in.ServerID
			v.ScopeName = &in.Name
			t.ServerID = in.ServerID
			t.Stack = in.Name
		case "container":
			if in.ServerID == "" || in.Name == "" {
				writeError(w, 400, "invalid_scope")
				return
			}
			v.ScopeServerID = &in.ServerID
			v.ScopeName = &in.Name
			t.ServerID = in.ServerID
			t.Container = in.Name
		case "organization":
		default:
			writeError(w, 400, "invalid_scope")
			return
		}
		if in.ServerID != "" {
			server, err := d.Store.GetServer(r.Context(), in.ServerID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					writeError(w, 400, "invalid_scope")
				} else {
					internal(w, "get silence server", err)
				}
				return
			}
			t.GroupID = server.GroupID
		}
		if !authorize(r, rbac.PermAlertsSilence, t) {
			denyRequest(w, r, rbac.PermAlertsSilence, t, 403, "forbidden")
			return
		}
		s, _ := sessionFromContext(r.Context())
		v.CreatedBy = s.User.Username
		created, err := d.Store.CreateSilence(r.Context(), v)
		if err != nil {
			internal(w, "create silence", err)
			return
		}
		auditAlert(d, r, "silence_create", t, map[string]any{"silence_id": created.ID, "scope_type": v.ScopeType, "reason": v.Reason})
		writeJSON(w, 201, silenceEntry(created))
	})
}
func NewSilenceDeleteHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "silenceID")
		rows, err := d.Store.ListActiveSilences(r.Context(), d.now())
		if err != nil {
			internal(w, "list silences", err)
			return
		}
		var found *store.Silence
		for i := range rows {
			if rows[i].ID == id {
				found = &rows[i]
				break
			}
		}
		if found == nil {
			writeError(w, 404, "not_found")
			return
		}
		groupID := ""
		if found.ScopeServerID != nil {
			server, serverErr := d.Store.GetServer(r.Context(), *found.ScopeServerID)
			if serverErr != nil {
				if errors.Is(serverErr, store.ErrNotFound) {
					writeError(w, 404, "not_found")
				} else {
					internal(w, "get silence server", serverErr)
				}
				return
			}
			groupID = server.GroupID
		}
		t := silenceTarget(*found, groupID)
		if !authorize(r, rbac.PermAlertsSilence, t) {
			denyRequest(w, r, rbac.PermAlertsSilence, t, 403, "forbidden")
			return
		}
		if err = d.Store.DeleteSilence(r.Context(), id); err != nil {
			internal(w, "delete silence", err)
			return
		}
		auditAlert(d, r, "silence_delete", t, map[string]any{"silence_id": id})
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
}
func silenceTarget(s store.Silence, groupID string) rbac.Target {
	t := rbac.Target{}
	if s.ScopeServerID != nil {
		t.ServerID = *s.ScopeServerID
	}
	if s.ScopeGroupID != nil {
		t.GroupID = *s.ScopeGroupID
	} else if s.ScopeServerID != nil {
		t.GroupID = groupID
	}
	if s.ScopeType == "stack" && s.ScopeName != nil {
		t.Stack = *s.ScopeName
	}
	if s.ScopeType == "container" && s.ScopeName != nil {
		t.Container = *s.ScopeName
	}
	return t
}

func silenceGroup(s store.Silence, servers map[string]alertServer) string {
	if s.ScopeGroupID != nil || s.ScopeServerID == nil {
		return ""
	}
	return servers[*s.ScopeServerID].groupID
}
