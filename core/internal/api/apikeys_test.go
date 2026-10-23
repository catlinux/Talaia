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

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

func apiKeyRequest(t *testing.T, panel *testPanel, method, path, token string, body any, origin bool) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal api key request: %v", err)
		}
		reader = strings.NewReader(string(payload))
	}
	request, err := http.NewRequest(method, panel.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build api key request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin {
		request.Header.Set("Origin", testOrigin)
	}
	response, err := panel.rawClient().Do(request)
	if err != nil {
		t.Fatalf("%s %s with api key: %v", method, path, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func cookieRequestWithoutOrigin(t *testing.T, panel *testPanel, method, path, token string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal cookie request: %v", err)
		}
		reader = strings.NewReader(string(payload))
	}
	request, err := http.NewRequest(method, panel.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build cookie request: %v", err)
	}
	cookie := panel.sessionCookieValue(t, panel.client)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookie})
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := panel.server.Client().Do(request)
	if err != nil {
		t.Fatalf("send cookie request: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func createAPIKeyViaHTTP(t *testing.T, panel *testPanel, secret []byte, bindings []map[string]any, days int) apiKeyCreatedResponse {
	t.Helper()
	body := map[string]any{
		"name": "test-key", "bindings": bindings, "totp_code": panel.code(secret),
	}
	if days != 0 {
		body["expires_in_days"] = days
	}
	response := panel.post(t, "/api/v1/apikeys", body)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create api key status = %d, want 201 (body %s)", response.StatusCode, response.Body)
	}
	var created apiKeyCreatedResponse
	decodeBody(t, response, &created)
	return created
}

func directAPIKey(t *testing.T, panel *testPanel, ownerID string, binding *store.RoleBinding, expiresAt time.Time) (store.APIKey, string) {
	t.Helper()
	keyID, secret, token, err := newAPIKeyToken()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	key, err := panel.store.CreateAPIKey(context.Background(), keyID, auth.HashToken(secret),
		"direct-key", ownerID, expiresAt)
	if err != nil {
		t.Fatalf("create direct key: %v", err)
	}
	if binding != nil {
		binding.SubjectType = store.SubjectAPIKey
		binding.SubjectID = key.ID
		if _, err := panel.store.CreateRoleBinding(context.Background(), *binding); err != nil {
			t.Fatalf("create direct key binding: %v", err)
		}
	}
	return key, token
}

func TestAPIKeyManagementAndRestrictions(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	ctx := context.Background()
	serverID, err := panel.store.CreateServer(ctx, "key-server-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	key := createAPIKeyViaHTTP(t, panel, secret, []map[string]any{{
		"role":  string(rbac.RoleViewer),
		"scope": map[string]any{"type": string(rbac.ScopeServer), "server_id": serverID},
	}}, 30)
	if !strings.HasPrefix(key.Key, "tlk_") || !strings.Contains(key.Key, "_") || len(key.Key) < 40 {
		t.Fatalf("key format = %q", key.Key)
	}
	response := panel.get(t, "/api/v1/apikeys")
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read keys: %v", err)
	}
	if strings.Contains(string(raw), key.Key) {
		t.Fatal("the api key list contains the plaintext key")
	}
	var list apiKeysResponse
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode keys: %v", err)
	}
	if len(list.Keys) != 1 || list.Keys[0].ID != key.ID || len(list.Keys[0].Bindings) != 1 {
		t.Fatalf("keys = %+v", list.Keys)
	}

	servers := decodePanelJSON[serversResponse](t, apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", key.Key, nil, false))
	if len(servers.Servers) != 1 || servers.Servers[0].ID != serverID {
		t.Fatalf("key servers = %+v, want only %s", servers.Servers, serverID)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/users", key.Key, nil, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("key users status = %d, want 403", response.StatusCode)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/auth/session", key.Key, nil, false); response.StatusCode != http.StatusUnauthorized ||
		errorCode(t, response) != "session_required" {
		t.Fatalf("key on auth = %d, want 401 session_required", response.StatusCode)
	}

	// Wrong secret and revoked keys are indistinguishable from unknown keys.
	wrong := key.Key[:strings.LastIndex(key.Key, "_")+1] + strings.Repeat("a", 32)
	wrongResponse := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", wrong, nil, false)
	if wrongResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong secret status = %d, want 401", wrongResponse.StatusCode)
	}
	if code := errorCode(t, wrongResponse); code != "invalid_api_key" {
		t.Fatalf("wrong secret code = %q, want invalid_api_key", code)
	}
	if response := panel.do(t, panel.client, http.MethodDelete, "/api/v1/apikeys/"+key.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke key status = %d, want 204", response.StatusCode)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", key.Key, nil, false); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked key status = %d, want 401", response.StatusCode)
	}

	// A key with an organization admin binding still cannot use admin-only,
	// terminal, settings, reboot or key-management routes.
	panel.clock.Advance(30 * time.Second)
	adminKey := createAPIKeyViaHTTP(t, panel, secret, []map[string]any{{
		"role":  string(rbac.RoleAdmin),
		"scope": map[string]any{"type": string(rbac.ScopeOrganization)},
	}}, 0)
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/users", adminKey.Key, nil, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("admin key users status = %d, want 403", response.StatusCode)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/system/version", adminKey.Key, nil, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("admin key system status = %d, want 403", response.StatusCode)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/apikeys", adminKey.Key, nil, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("admin key keys status = %d, want 403", response.StatusCode)
	}
	if response := apiKeyRequest(t, panel, http.MethodPost, "/api/v1/servers/"+serverID+"/reboot", adminKey.Key,
		map[string]any{"confirm": true}, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("admin key reboot status = %d, want 403", response.StatusCode)
	}
	if response := apiKeyRequest(t, panel, http.MethodPost,
		"/api/v1/servers/"+serverID+"/containers/web/terminal", adminKey.Key,
		map[string]any{"confirm": true, "shell": "bash"}, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("admin key terminal status = %d, want 403", response.StatusCode)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet,
		"/api/v1/terminal/00000000-0000-0000-0000-000000000000/ws", adminKey.Key, nil, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("admin key ws status = %d, want 403", response.StatusCode)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/audit", adminKey.Key, nil, false); response.StatusCode != http.StatusOK {
		t.Fatalf("admin key audit status = %d, want 200", response.StatusCode)
	}

	entries, err := panel.store.ListAudit(ctx, 200)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	foundAuthz, foundUseDenied := false, false
	for _, entry := range entries {
		if entry.Action == "authz_denied" && entry.Details["api_key_id"] == adminKey.ID {
			foundAuthz = true
		}
		if entry.Action == "apikey_use_denied" {
			foundUseDenied = true
		}
	}
	if !foundAuthz || !foundUseDenied {
		t.Fatalf("audit: authz=%v useDenied=%v", foundAuthz, foundUseDenied)
	}
}

func TestAPIKeyCreateTOTPAndExpiry(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	ctx := context.Background()
	serverID, err := panel.store.CreateServer(ctx, "key-totp-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	binding := map[string]any{
		"role":  string(rbac.RoleViewer),
		"scope": map[string]any{"type": string(rbac.ScopeServer), "server_id": serverID},
	}

	if response := panel.post(t, "/api/v1/apikeys", map[string]any{
		"name": "no-code", "bindings": []map[string]any{binding},
	}); response.StatusCode != http.StatusForbidden || errorCode(t, response) != "code_invalid" {
		t.Fatalf("missing code = %d %q", response.StatusCode, errorCode(t, response))
	}
	if response := panel.post(t, "/api/v1/apikeys", map[string]any{
		"name": "too-long", "expires_in_days": 366, "totp_code": panel.code(secret),
		"bindings": []map[string]any{binding},
	}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("long expiry status = %d, want 400", response.StatusCode)
	}

	created := createAPIKeyViaHTTP(t, panel, secret, []map[string]any{binding}, 1)
	// The same TOTP code cannot create a second key.
	if response := panel.post(t, "/api/v1/apikeys", map[string]any{
		"name": "replay", "totp_code": panel.code(secret), "bindings": []map[string]any{binding},
	}); response.StatusCode != http.StatusForbidden || errorCode(t, response) != "code_invalid" {
		t.Fatalf("replayed code = %d %q", response.StatusCode, errorCode(t, response))
	}
	panel.clock.Advance(48 * time.Hour)
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", created.Key, nil, false); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired key status = %d, want 401", response.StatusCode)
	}
}

func TestAPIKeyOwnerLosesBinding(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	owner, _ := createSessionOnlyUser(t, panel, "key-owner")
	ctx := context.Background()
	serverID, err := panel.store.CreateServer(ctx, "key-owner-server-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ownerBinding, err := panel.store.CreateRoleBinding(ctx, store.RoleBinding{
		SubjectType: store.SubjectUser, SubjectID: owner.ID,
		ScopeType: store.ScopeServer, ScopeServerID: serverID, Role: string(rbac.RoleViewer),
	})
	if err != nil {
		t.Fatalf("create owner binding: %v", err)
	}
	_, token := directAPIKey(t, panel, owner.ID, &store.RoleBinding{
		ScopeType: store.ScopeServer, ScopeServerID: serverID, Role: string(rbac.RoleViewer),
	}, time.Now().Add(time.Hour))
	servers := decodePanelJSON[serversResponse](t, apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", token, nil, false))
	if len(servers.Servers) != 1 {
		t.Fatalf("key servers before = %+v", servers.Servers)
	}
	if err := panel.store.DeleteRoleBinding(ctx, ownerBinding.ID); err != nil {
		t.Fatalf("delete owner binding: %v", err)
	}
	servers = decodePanelJSON[serversResponse](t, apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", token, nil, false))
	if len(servers.Servers) != 0 {
		t.Fatalf("key still sees servers after the owner lost the binding: %+v", servers.Servers)
	}
}

func TestAPIKeyAuthFailures(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	owner, _ := createSessionOnlyUser(t, panel, "key-failure-owner")
	ctx := context.Background()

	_, token := directAPIKey(t, panel, owner.ID, nil, time.Now().Add(time.Hour))
	wrong := token[:strings.LastIndex(token, "_")+1] + strings.Repeat("b", 32)
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", wrong, nil, false); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong secret status = %d, want 401", response.StatusCode)
	}
	key, revokedToken := directAPIKey(t, panel, owner.ID, nil, time.Now().Add(time.Hour))
	if err := panel.store.RevokeAPIKey(ctx, key.KeyID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", revokedToken, nil, false); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked key status = %d, want 401", response.StatusCode)
	}
	_, expiring := directAPIKey(t, panel, owner.ID, nil, time.Now().Add(time.Minute))
	panel.clock.Advance(2 * time.Minute)
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", expiring, nil, false); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired key status = %d, want 401", response.StatusCode)
	}
	_, disabledOwnerKey := directAPIKey(t, panel, owner.ID, nil, time.Now().Add(time.Hour))
	if err := panel.store.DisableUser(ctx, owner.ID); err != nil {
		t.Fatalf("disable key owner: %v", err)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", disabledOwnerKey, nil, false); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled owner key status = %d, want 401", response.StatusCode)
	}
}

func TestAPIKeyCSRFAndRateLimit(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	ctx := context.Background()
	serverID, err := panel.store.CreateServer(ctx, "key-csrf-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	admin, err := panel.store.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	_, token := directAPIKey(t, panel, admin.ID, &store.RoleBinding{
		ScopeType: store.ScopeServer, ScopeServerID: serverID, Role: string(rbac.RoleServerOperator),
	}, time.Now().Add(time.Hour))

	response := apiKeyRequest(t, panel, http.MethodPost, "/api/v1/servers/"+serverID+"/resources/list",
		token, map[string]any{"kind": "image"}, false)
	if response.StatusCode == http.StatusForbidden && errorCode(t, response) == "forbidden_origin" {
		t.Fatal("api key request was rejected by the browser origin check")
	}
	if response := cookieRequestWithoutOrigin(t, panel, http.MethodPost, "/api/v1/apikeys", "", map[string]any{
		"name": "csrf", "bindings": []map[string]any{},
	}); response.StatusCode != http.StatusForbidden || errorCode(t, response) != "forbidden_origin" {
		t.Fatalf("cookie request without origin = %d %q", response.StatusCode, errorCode(t, response))
	}
	if response := cookieRequestWithoutOrigin(t, panel, http.MethodPost, "/api/v1/servers/"+serverID+"/resources/list",
		token, map[string]any{"kind": "image"}); response.StatusCode != http.StatusForbidden || errorCode(t, response) != "forbidden_origin" {
		t.Fatalf("cookie plus bearer without origin = %d %q", response.StatusCode, errorCode(t, response))
	}

	_, limitedToken := directAPIKey(t, panel, admin.ID, nil, time.Now().Add(time.Hour))
	handler := Sessions(SessionDeps{
		Store: panel.store, Now: panel.clock.Now,
		APIKeyLimiter: NewRateLimiterWithClock(1, time.Minute, panel.clock.Now),
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	first := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	first.Header.Set("Authorization", "Bearer "+limitedToken)
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first rate-limited request = %d, want 200", firstRecorder.Code)
	}
	second := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	second.Header.Set("Authorization", "Bearer "+limitedToken)
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second rate-limited request = %d, want 429", secondRecorder.Code)
	}
}

func TestAPIKeyStackScope(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	serverA := enrollTestServer(t, panel)
	serverB := enrollTestServer(t, panel)
	panel.agents.set(criterionAgent(serverA, "web", "db"), criterionAgent(serverB, "web"))
	owner, _ := createSessionOnlyUser(t, panel, "key-stack-owner")
	ctx := context.Background()
	if _, err := panel.store.CreateRoleBinding(ctx, store.RoleBinding{
		SubjectType: store.SubjectUser, SubjectID: owner.ID,
		ScopeType: store.ScopeStack, ScopeServerID: serverA, ScopeName: "web", Role: string(rbac.RoleAppOperator),
	}); err != nil {
		t.Fatalf("create stack binding: %v", err)
	}
	_, token := directAPIKey(t, panel, owner.ID, &store.RoleBinding{
		ScopeType: store.ScopeStack, ScopeServerID: serverA, ScopeName: "web", Role: string(rbac.RoleAppOperator),
	}, time.Now().Add(time.Hour))

	servers := decodePanelJSON[serversResponse](t, apiKeyRequest(t, panel, http.MethodGet, "/api/v1/servers", token, nil, false))
	if len(servers.Servers) != 1 || servers.Servers[0].ID != serverA {
		t.Fatalf("stack key servers = %+v, want only %s", servers.Servers, serverA)
	}
	containers := decodePanelJSON[containersResponse](t, apiKeyRequest(t, panel, http.MethodGet,
		"/api/v1/servers/"+serverA+"/containers", token, nil, false))
	if len(containers.Containers) != 1 || containers.Containers[0].Name != serverA+"-web-ct" {
		t.Fatalf("stack key containers = %+v", containers.Containers)
	}
	if response := apiKeyRequest(t, panel, http.MethodGet,
		"/api/v1/servers/"+serverB+"/containers", token, nil, false); response.StatusCode != http.StatusNotFound {
		t.Fatalf("stack key other server = %d, want 404", response.StatusCode)
	}
}
