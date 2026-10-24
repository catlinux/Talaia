package api

import (
	"net/http"

	"github.com/catlinux/Talaia/core/internal/rbac"
)

type alertSettingsJSON struct {
	AllowPrivateTargets *bool `json:"allow_private_targets"`
}

func NewAlertSettingsHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, err := d.Store.GetAllowPrivateTargets(r.Context())
		if err != nil {
			internal(w, "get alert settings", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"allow_private_targets": allowed})
	})
}

func NewAlertSettingsUpdateHandler(d AlertsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in alertSettingsJSON
		if decodeJSON(w, r, &in) != nil {
			writeInvalid(w, "", "body")
			return
		}
		if in.AllowPrivateTargets == nil {
			writeInvalid(w, "allow_private_targets", "required")
			return
		}
		if err := d.Store.SetAllowPrivateTargets(r.Context(), *in.AllowPrivateTargets); err != nil {
			internal(w, "update alert settings", err)
			return
		}
		// Cloud metadata addresses stay blocked regardless of this setting.
		auditAlert(d, r, "alert_settings_update", rbac.Target{}, map[string]any{"allow_private_targets": *in.AllowPrivateTargets})
		writeJSON(w, http.StatusOK, map[string]bool{"allow_private_targets": *in.AllowPrivateTargets})
	})
}
