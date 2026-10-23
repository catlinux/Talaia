package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
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

	mu       sync.Mutex
	cached   *coreoidc.Provider
	cacheKey string
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

// provider builds a provider from a stored config and an already decrypted
// secret. Login and settings both use it so discovery and redirect handling
// cannot drift apart.
func (h *oidcSettingsHandler) provider(ctx context.Context, config store.OIDCConfig, clientSecret, redirectURL string) (*coreoidc.Provider, error) {
	return coreoidc.NewProvider(ctx, coreoidc.Config{
		Issuer:       config.Issuer,
		ClientID:     config.ClientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Label:        config.ButtonLabel,
	})
}

// storedProvider opens the encrypted client secret before building a provider.
// The provider is reused while the stored configuration stays the same, so
// login starts and callbacks share its discovery cache.
func (h *oidcSettingsHandler) storedProvider(ctx context.Context, config store.OIDCConfig, redirectURL string) (*coreoidc.Provider, error) {
	key := config.Issuer + "\x00" + config.ClientID + "\x00" + config.ButtonLabel + "\x00" +
		redirectURL + "\x00" + string(config.ClientSecretEnc)
	h.mu.Lock()
	if h.cached != nil && h.cacheKey == key {
		provider := h.cached
		h.mu.Unlock()
		return provider, nil
	}
	h.mu.Unlock()

	secret := ""
	if len(config.ClientSecretEnc) > 0 {
		if h.deps.Box == nil {
			return nil, errors.New("the oidc client secret cannot be opened without a secret box")
		}
		opened, err := h.deps.Box.Open(config.ClientSecretEnc)
		if err != nil {
			return nil, err
		}
		secret = string(opened)
	}
	provider, err := h.provider(ctx, config, secret, redirectURL)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.cached = provider
	h.cacheKey = key
	h.mu.Unlock()
	return provider, nil
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
		if _, err := h.provider(r.Context(), store.OIDCConfig{
			Issuer: request.Issuer, ClientID: request.ClientID, ButtonLabel: request.ButtonLabel,
		}, request.ClientSecret, h.redirectURL(r)); err != nil {
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
