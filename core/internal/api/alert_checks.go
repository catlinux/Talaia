package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/checks"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/go-chi/chi/v5"
)

type alertCheckResultJSON struct {
	OK           bool       `json:"ok"`
	LatencyMS    int        `json:"latency_ms"`
	StatusCode   *int       `json:"status_code"`
	Error        string     `json:"error"`
	CertNotAfter *time.Time `json:"cert_not_after"`
	Time         *time.Time `json:"time"`
}

type alertCheckJSON struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Kind            string                `json:"kind"`
	Target          string                `json:"target"`
	Config          json.RawMessage       `json:"config"`
	IntervalSeconds int                   `json:"interval_seconds"`
	Enabled         bool                  `json:"enabled"`
	ServerID        *string               `json:"server_id"`
	LatestResult    *alertCheckResultJSON `json:"latest_result"`
}

func alertCheckEntry(v store.AlertCheck, result *store.AlertCheckResult) alertCheckJSON {
	out := alertCheckJSON{ID: v.ID, Name: v.Name, Kind: v.Kind, Target: v.Target, Config: v.Config, IntervalSeconds: v.IntervalSeconds, Enabled: v.Enabled, ServerID: v.ServerID}
	if result != nil {
		out.LatestResult = &alertCheckResultJSON{result.OK, result.LatencyMS, result.StatusCode, result.Error, result.CertNotAfter, &result.Time}
	}
	return out
}

func NewAlertChecksHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.Store.ListAlertChecks(r.Context())
		if err != nil {
			internal(w, "list checks", err)
			return
		}
		out := make([]alertCheckJSON, 0, len(rows))
		for _, row := range rows {
			result, resultErr := d.Store.LatestAlertCheckResult(r.Context(), row.ID)
			if resultErr != nil && !errors.Is(resultErr, store.ErrNotFound) {
				internal(w, "get latest check result", resultErr)
				return
			}
			if errors.Is(resultErr, store.ErrNotFound) {
				out = append(out, alertCheckEntry(row, nil))
			} else {
				out = append(out, alertCheckEntry(row, &result))
			}
		}
		writeJSON(w, http.StatusOK, out)
	})
}

type alertCheckRequest struct {
	Name            *string         `json:"name"`
	Kind            *string         `json:"kind"`
	Target          *string         `json:"target"`
	Config          json.RawMessage `json:"config"`
	IntervalSeconds *int            `json:"interval_seconds"`
	Enabled         *bool           `json:"enabled"`
	ServerID        *string         `json:"server_id"`
}

func NewAlertCheckCreateHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in alertCheckRequest
		if decodeJSON(w, r, &in) != nil {
			writeError(w, 400, "invalid_request")
			return
		}
		v := store.AlertCheck{Enabled: true, Config: json.RawMessage(`{}`)}
		applyAlertCheckRequest(&v, in)
		if !validAlertCheck(v) {
			writeError(w, 400, "invalid_check")
			return
		}
		if !validateAlertCheckServer(w, r, d, v.ServerID) {
			return
		}
		s, _ := sessionFromContext(r.Context())
		v.UpdatedBy = s.User.Username
		created, err := d.Store.CreateAlertCheck(r.Context(), v)
		if err != nil {
			internal(w, "create check", err)
			return
		}
		if d.Checks != nil {
			d.Checks.Reload()
		}
		auditAlert(d, r, "check_create", rbac.Target{}, map[string]any{"name": created.Name, "kind": created.Kind})
		writeJSON(w, 201, alertCheckEntry(created, nil))
	})
}

func NewAlertCheckUpdateHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "checkID")
		v, err := d.Store.GetAlertCheck(r.Context(), id)
		if err != nil {
			alertCheckStoreError(w, err)
			return
		}
		var in alertCheckRequest
		if decodeJSON(w, r, &in) != nil {
			writeError(w, 400, "invalid_request")
			return
		}
		applyAlertCheckRequest(&v, in)
		if !validAlertCheck(v) {
			writeError(w, 400, "invalid_check")
			return
		}
		if !validateAlertCheckServer(w, r, d, v.ServerID) {
			return
		}
		s, _ := sessionFromContext(r.Context())
		v.UpdatedBy = s.User.Username
		if err = d.Store.UpdateAlertCheck(r.Context(), v); err != nil {
			alertCheckStoreError(w, err)
			return
		}
		if d.Checks != nil {
			d.Checks.Reload()
		}
		auditAlert(d, r, "check_update", rbac.Target{}, map[string]any{"name": v.Name, "kind": v.Kind})
		writeJSON(w, 200, alertCheckEntry(v, nil))
	})
}

func NewAlertCheckDeleteHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "checkID")
		v, err := d.Store.GetAlertCheck(r.Context(), id)
		if err != nil {
			alertCheckStoreError(w, err)
			return
		}
		if err = d.Store.DeleteAlertCheck(r.Context(), id); err != nil {
			alertCheckStoreError(w, err)
			return
		}
		if d.Checks != nil {
			d.Checks.Reload()
		}
		auditAlert(d, r, "check_delete", rbac.Target{}, map[string]any{"name": v.Name, "kind": v.Kind})
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
}

func NewAlertCheckRunHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.Checks == nil {
			writeError(w, 500, "internal")
			return
		}
		v, err := d.Store.GetAlertCheck(r.Context(), chi.URLParam(r, "checkID"))
		if err != nil {
			alertCheckStoreError(w, err)
			return
		}
		result := d.Checks.RunOnce(r.Context(), v)
		auditAlert(d, r, "check_run", rbac.Target{}, map[string]any{"name": v.Name, "kind": v.Kind})
		writeJSON(w, 200, alertCheckResultJSON{result.OK, result.LatencyMS, result.StatusCode, result.Error, result.CertNotAfter, &result.Time})
	})
}

func applyAlertCheckRequest(v *store.AlertCheck, in alertCheckRequest) {
	if in.Name != nil {
		v.Name = *in.Name
	}
	if in.Kind != nil {
		v.Kind = *in.Kind
	}
	if in.Target != nil {
		v.Target = *in.Target
	}
	if len(in.Config) > 0 {
		v.Config = in.Config
	}
	if in.IntervalSeconds != nil {
		v.IntervalSeconds = *in.IntervalSeconds
	}
	if in.Enabled != nil {
		v.Enabled = *in.Enabled
	}
	if in.ServerID != nil {
		if *in.ServerID == "" {
			v.ServerID = nil
		} else {
			v.ServerID = in.ServerID
		}
	}
}

func validAlertCheck(v store.AlertCheck) bool {
	return strings.TrimSpace(v.Name) != "" && len(v.Name) <= 200 && v.IntervalSeconds >= 30 && v.IntervalSeconds <= 86400 && checks.ValidateCheck(v.Kind, v.Target, v.Config) == nil
}

func validateAlertCheckServer(w http.ResponseWriter, r *http.Request, d AlertsDeps, id *string) bool {
	if id == nil {
		return true
	}
	if _, err := d.Store.GetServer(r.Context(), *id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, 400, "invalid_server")
		} else {
			internal(w, "get check server", err)
		}
		return false
	}
	return true
}

func alertCheckStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, 404, "not_found")
		return
	}
	internal(w, "alert check", err)
}
