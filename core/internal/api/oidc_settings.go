package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/catlinux/Talaia/core/internal/auth"
	coreoidc "github.com/catlinux/Talaia/core/internal/oidc"
	"github.com/catlinux/Talaia/core/internal/store"
)

// OIDCSettingsDeps are the collaborators of the OIDC settings API.
type OIDCSettingsDeps struct {
	Store *store.Store
	Box   *auth.SecretBox
	Audit Auditor
	// PublicURL is the configured public base URL of the panel. When empty,
	// redirect_url is derived from the request host using https.
	PublicURL string
}

type oidcSettingsRequest struct {
	Enabled      bool   `json:"enabled"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	ButtonLabel  string `json:"button_label"`
}

type oidcSettingsResponse struct {
	Enabled     bool   `json:"enabled"`
	Issuer      string `json:"issuer"`
	ClientID    string `json:"client_id"`
	HasSecret   bool   `json:"has_secret"`
	ButtonLabel string `json:"button_label"`
	RedirectURL string `json:"redirect_url"`
}

type oidcSettingsHandler struct {
	deps OIDCSettingsDeps
}

func newOIDCSettingsHandler(deps OIDCSettingsDeps) *oidcSettingsHandler {
	return &oidcSettingsHandler{deps: deps}
}

func (h *oidcSettingsHandler) storeOrError(w http.ResponseWriter) (*store.Store, bool) {
	if h.deps.Store == nil {
		internal(w, "oidc settings without a store", errors.New("nil store"))
		return nil, false
	}
	return h.deps.Store, true
}

func (h *oidcSettingsHandler) settingsResponse(r *http.Request, config store.OIDCConfig) oidcSettingsResponse {
	return oidcSettingsResponse{
		Enabled:     config.Enabled,
		Issuer:      config.Issuer,
		ClientID:    config.ClientID,
		HasSecret:   len(config.ClientSecretEnc) > 0,
		ButtonLabel: config.ButtonLabel,
		RedirectURL: h.redirectURL(r),
	}
}

// redirectURL uses the panel's configured public origin when there is one.
// Otherwise it falls back to the request's Host over https; a reverse proxy
// that terminates TLS must therefore pass the public Host through.
func (h *oidcSettingsHandler) redirectURL(r *http.Request) string {
	base := strings.TrimRight(strings.TrimSpace(h.deps.PublicURL), "/")
	if base == "" {
		host := strings.TrimSpace(r.Host)
		if host == "" {
			host = "localhost"
		}
		base = "https://" + host
	}
	return base + coreoidc.RedirectPath
}

func (h *oidcSettingsHandler) getSettings(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	config, err := st.GetOIDCConfig(r.Context())
	if err != nil {
		internal(w, "get oidc settings", err)
		return
	}
	writeJSON(w, http.StatusOK, h.settingsResponse(r, config))
}

func (h *oidcSettingsHandler) updateSettings(w http.ResponseWriter, r *http.Request) {
	st, ok := h.storeOrError(w)
	if !ok {
		return
	}
	var request oidcSettingsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	request.Issuer = strings.TrimSpace(request.Issuer)
	request.ClientID = strings.TrimSpace(request.ClientID)
	request.ButtonLabel = strings.TrimSpace(request.ButtonLabel)

	if request.Issuer != "" {
		if err := coreoidc.ValidateIssuer(request.Issuer); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	if request.Enabled && (request.Issuer == "" || request.ClientID == "") {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if utf8.RuneCountInString(request.ButtonLabel) > 40 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	var secretEnc []byte
	if request.ClientSecret != "" {
		if h.deps.Box == nil {
			internal(w, "encrypt the oidc client secret", errors.New("nil secret box"))
			return
		}
		secretEnc = h.deps.Box.Seal([]byte(request.ClientSecret))
	}

	// Enabling the provider must be proven against discovery before the row is
	// changed, so a typo cannot leave SSO enabled but unusable.
	if request.Enabled {
		if _, err := coreoidc.NewProvider(r.Context(), coreoidc.Config{
			Issuer:       request.Issuer,
			ClientID:     request.ClientID,
			ClientSecret: request.ClientSecret,
			RedirectURL:  h.redirectURL(r),
			Label:        request.ButtonLabel,
		}); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "oidc_discovery_failed")
			return
		}
	}

	session, _ := sessionFromContext(r.Context())
	saved, err := st.SaveOIDCConfig(r.Context(), store.OIDCConfig{
		Enabled:         request.Enabled,
		Issuer:          request.Issuer,
		ClientID:        request.ClientID,
		ClientSecretEnc: secretEnc,
		ButtonLabel:     request.ButtonLabel,
		UpdatedBy:       session.User.Username,
	})
	if err != nil {
		internal(w, "save oidc settings", err)
		return
	}

	h.audit(r, saved)
	writeJSON(w, http.StatusOK, h.settingsResponse(r, saved))
}

func (h *oidcSettingsHandler) audit(r *http.Request, config store.OIDCConfig) {
	if h.deps.Audit == nil {
		return
	}
	session, _ := sessionFromContext(r.Context())
	event := store.AuditEvent{
		Actor:   session.User.Username,
		ActorID: session.User.ID,
		Action:  "oidc_settings_update",
		IP:      clientIP(r),
		Result:  "success",
		Scope:   map[string]any{"type": "settings", "section": "oidc"},
		Details: map[string]any{
			"enabled":      config.Enabled,
			"issuer":       config.Issuer,
			"client_id":    config.ClientID,
			"has_secret":   len(config.ClientSecretEnc) > 0,
			"button_label": config.ButtonLabel,
		},
	}
	if err := h.deps.Audit.AppendAudit(r.Context(), event); err != nil {
		// An audit failure must not undo a saved setting.
	}
}
