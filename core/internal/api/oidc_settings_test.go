package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func oidcDiscoveryTestServer(t *testing.T, healthy bool) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy || r.URL.Path != "/.well-known/openid-configuration" {
			http.Error(w, "discovery failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                server.URL,
			"authorization_endpoint":                server.URL + "/authorize",
			"token_endpoint":                        server.URL + "/token",
			"jwks_uri":                              server.URL + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestOIDCSettingsRedirectFromRequest(t *testing.T) {
	panel := newTestPanel(t)
	handler := newOIDCSettingsHandler(OIDCSettingsDeps{Store: panel.store})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings/oidc", nil)
	request.Host = "core.example.com:8443"
	recorder := httptest.NewRecorder()
	handler.getSettings(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var response oidcSettingsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if response.RedirectURL != "https://core.example.com:8443/api/v1/auth/oidc/callback" {
		t.Fatalf("redirect_url = %q", response.RedirectURL)
	}
}

func TestOIDCSettingsAPI(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	ctx := context.Background()

	response := panel.do(t, panel.client, http.MethodGet, "/api/v1/settings/oidc", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("default settings status = %d, want 200", response.StatusCode)
	}
	var initial oidcSettingsResponse
	decodeBody(t, response, &initial)
	if initial.Enabled || initial.Issuer != "" || initial.ClientID != "" || initial.HasSecret {
		t.Fatalf("default settings = %+v", initial)
	}
	if initial.RedirectURL != "https://panel.example.com/api/v1/auth/oidc/callback" {
		t.Fatalf("redirect_url = %q", initial.RedirectURL)
	}

	_, nonAdminClient := createSessionOnlyUser(t, panel, "oidc-non-admin")
	if response := panel.do(t, nonAdminClient, http.MethodGet, "/api/v1/settings/oidc", nil); response.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin settings status = %d, want 403", response.StatusCode)
	}
	if response := panel.do(t, nonAdminClient, http.MethodPut, "/api/v1/settings/oidc", map[string]any{
		"enabled": false,
	}); response.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin settings update status = %d, want 403", response.StatusCode)
	}

	adminUser, err := panel.store.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	_, key := directAPIKey(t, panel, adminUser.ID, nil, time.Now().Add(time.Hour))
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/settings/oidc", key, nil, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("api key settings status = %d, want 403", response.StatusCode)
	}

	if response := panel.do(t, panel.client, http.MethodPut, "/api/v1/settings/oidc", map[string]any{
		"enabled": false, "issuer": "http://example.com", "client_id": "talaia",
	}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-https issuer status = %d, want 400", response.StatusCode)
	}
	good := oidcDiscoveryTestServer(t, true)
	if response := panel.do(t, panel.client, http.MethodPut, "/api/v1/settings/oidc", map[string]any{
		"enabled": true, "issuer": good.URL, "client_id": "",
	}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing client_id status = %d, want 400", response.StatusCode)
	}
	if response := panel.do(t, panel.client, http.MethodPut, "/api/v1/settings/oidc", map[string]any{
		"enabled": true, "issuer": good.URL, "client_id": "talaia", "button_label": strings.Repeat("x", 41),
	}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("long label status = %d, want 400", response.StatusCode)
	}

	bad := oidcDiscoveryTestServer(t, false)
	failed := panel.do(t, panel.client, http.MethodPut, "/api/v1/settings/oidc", map[string]any{
		"enabled": true, "issuer": bad.URL, "client_id": "talaia", "client_secret": "unused",
	})
	if failed.StatusCode != http.StatusUnprocessableEntity || errorCode(t, failed) != "oidc_discovery_failed" {
		t.Fatalf("discovery failure = %d %q", failed.StatusCode, errorCode(t, failed))
	}

	const clientSecret = "super-secret-client-value"
	created := panel.do(t, panel.client, http.MethodPut, "/api/v1/settings/oidc", map[string]any{
		"enabled": true, "issuer": good.URL, "client_id": "talaia-client",
		"client_secret": clientSecret, "button_label": "Company SSO",
	})
	if created.StatusCode != http.StatusOK {
		t.Fatalf("save settings status = %d, want 200", created.StatusCode)
	}
	raw, err := io.ReadAll(created.Body)
	if err != nil {
		t.Fatalf("read settings response: %v", err)
	}
	if strings.Contains(string(raw), clientSecret) {
		t.Fatal("the settings response contains the client secret")
	}
	var saved oidcSettingsResponse
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("decode settings response: %v", err)
	}
	if !saved.Enabled || !saved.HasSecret || saved.ButtonLabel != "Company SSO" ||
		saved.RedirectURL != "https://panel.example.com/api/v1/auth/oidc/callback" {
		t.Fatalf("saved settings = %+v", saved)
	}
	if !strings.Contains(string(raw), good.URL) {
		t.Fatalf("saved issuer missing from %s", raw)
	}

	stored, err := panel.store.GetOIDCConfig(ctx)
	if err != nil {
		t.Fatalf("get stored oidc config: %v", err)
	}
	if len(stored.ClientSecretEnc) == 0 || strings.Contains(string(stored.ClientSecretEnc), clientSecret) {
		t.Fatal("the client secret was not encrypted at rest")
	}
	secretEnc := append([]byte(nil), stored.ClientSecretEnc...)

	getResponse := panel.do(t, panel.client, http.MethodGet, "/api/v1/settings/oidc", nil)
	getRaw, err := io.ReadAll(getResponse.Body)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(getRaw), clientSecret) {
		t.Fatal("GET settings contains the client secret")
	}
	var got oidcSettingsResponse
	if err := json.Unmarshal(getRaw, &got); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if got.Issuer != good.URL || got.ClientID != "talaia-client" || !got.HasSecret {
		t.Fatalf("GET settings = %+v", got)
	}

	// An empty secret keeps the encrypted value.
	kept := panel.do(t, panel.client, http.MethodPut, "/api/v1/settings/oidc", map[string]any{
		"enabled": true, "issuer": good.URL, "client_id": "talaia-client",
		"button_label": "Changed", "client_secret": "",
	})
	if kept.StatusCode != http.StatusOK {
		t.Fatalf("keep-secret update status = %d, want 200", kept.StatusCode)
	}
	stored, err = panel.store.GetOIDCConfig(ctx)
	if err != nil {
		t.Fatalf("get config after keep-secret update: %v", err)
	}
	if string(stored.ClientSecretEnc) != string(secretEnc) {
		t.Fatal("an empty client_secret replaced the stored secret")
	}

	entries, err := panel.store.ListAudit(ctx, 100)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != "oidc_settings_update" {
			continue
		}
		found = true
		details, err := json.Marshal(entry.Details)
		if err != nil {
			t.Fatalf("marshal audit details: %v", err)
		}
		if strings.Contains(string(details), clientSecret) {
			t.Fatal("the audit details contain the client secret")
		}
	}
	if !found {
		t.Fatal("the settings update was not audited")
	}
}
