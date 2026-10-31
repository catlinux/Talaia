package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

func createAdminExtraUser(t *testing.T, panel *testPanel, username string) store.User {
	t.Helper()
	user, err := panel.store.CreateUserFull(context.Background(), username, auth.HashPassword(testPassword),
		username, username+"@example.com")
	if err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	return user
}

func TestUserAdministrationAPI(t *testing.T) {
	panel := newTestPanel(t)
	admin := panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	bob := createAdminExtraUser(t, panel, "bob")

	response := panel.get(t, "/api/v1/users")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list users status = %d, want 200", response.StatusCode)
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read users: %v", err)
	}
	if strings.Contains(string(raw), "password_hash") || strings.Contains(string(raw), "totp_secret") {
		t.Fatal("users response leaked a secret field")
	}
	var listed usersResponse
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(listed.Users) != 2 {
		t.Fatalf("users = %+v, want admin and bob", listed.Users)
	}
	foundAdmin := false
	for _, user := range listed.Users {
		if user.ID == admin.ID {
			foundAdmin = true
			if !user.TOTPEnabled || user.Disabled {
				t.Fatalf("admin entry = %+v", user)
			}
			if len(user.Bindings) != 1 || user.Bindings[0].Role != string(rbac.RoleAdmin) ||
				user.Bindings[0].Scope.Type != string(rbac.ScopeOrganization) {
				t.Fatalf("admin bindings = %+v", user.Bindings)
			}
		}
	}
	if !foundAdmin {
		t.Fatal("admin is missing from the user list")
	}

	if response := panel.do(t, panel.client, http.MethodPatch, "/api/v1/users/"+bob.ID,
		map[string]string{"display_name": "Bob B.", "email": "BOB2@Example.com"}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("update user status = %d, want 204", response.StatusCode)
	}
	updated, err := panel.store.GetUser(context.Background(), bob.ID)
	if err != nil || updated.DisplayName != "Bob B." || updated.Email != "bob2@example.com" {
		t.Fatalf("updated bob = %+v, %v", updated, err)
	}

	response = panel.post(t, "/api/v1/users/"+bob.ID+"/bindings", map[string]any{
		"role":  string(rbac.RoleViewer),
		"scope": map[string]any{"type": string(rbac.ScopeOrganization)},
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create binding status = %d, want 201", response.StatusCode)
	}
	var binding adminBinding
	decodeBody(t, response, &binding)
	if binding.Role != string(rbac.RoleViewer) || binding.Scope.Type != string(rbac.ScopeOrganization) {
		t.Fatalf("binding = %+v", binding)
	}

	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/bindings", map[string]any{
		"role":  string(rbac.RoleViewer),
		"scope": map[string]any{"type": string(rbac.ScopeOrganization)},
	}); response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate binding status = %d, want 409", response.StatusCode)
	}
	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/bindings", map[string]any{
		"role":  "not-a-role",
		"scope": map[string]any{"type": string(rbac.ScopeOrganization)},
	}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid role status = %d, want 400", response.StatusCode)
	}
	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/bindings", map[string]any{
		"role":  string(rbac.RoleViewer),
		"scope": map[string]any{"type": string(rbac.ScopeServer), "server_id": "missing"},
	}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing server status = %d, want 400", response.StatusCode)
	}

	if response := panel.do(t, panel.client, http.MethodPatch, "/api/v1/bindings/"+binding.ID,
		map[string]string{"role": string(rbac.RoleAppOperator)}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("update binding status = %d, want 204", response.StatusCode)
	}
	response = panel.get(t, "/api/v1/users/"+bob.ID+"/bindings")
	var bindings userBindingsResponse
	decodeBody(t, response, &bindings)
	if len(bindings.Bindings) != 1 || bindings.Bindings[0].Role != string(rbac.RoleAppOperator) {
		t.Fatalf("bob bindings = %+v", bindings.Bindings)
	}

	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/disable", nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("disable bob status = %d, want 204", response.StatusCode)
	}
	disabled, err := panel.store.GetUser(context.Background(), bob.ID)
	if err != nil || disabled.DisabledAt.IsZero() {
		t.Fatalf("disabled bob = %+v, %v", disabled, err)
	}
	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/enable", nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("enable bob status = %d, want 204", response.StatusCode)
	}

	if response := panel.post(t, "/api/v1/users/"+admin.ID+"/disable", nil); response.StatusCode != http.StatusConflict || errorCode(t, response) != "cannot_disable_self" {
		t.Fatalf("self disable = %d %q, want 409 cannot_disable_self", response.StatusCode, errorCode(t, response))
	}
	stillEnabled, err := panel.store.GetUser(context.Background(), admin.ID)
	if err != nil || !stillEnabled.DisabledAt.IsZero() {
		t.Fatalf("self disable changed the user: %+v, %v", stillEnabled, err)
	}

	adminBindings := panel.get(t, "/api/v1/users/"+admin.ID+"/bindings")
	var own userBindingsResponse
	decodeBody(t, adminBindings, &own)
	if len(own.Bindings) != 1 {
		t.Fatalf("admin bindings = %+v", own.Bindings)
	}
	if response := panel.do(t, panel.client, http.MethodPatch, "/api/v1/bindings/"+own.Bindings[0].ID,
		map[string]string{"role": string(rbac.RoleViewer)}); response.StatusCode != http.StatusConflict {
		t.Fatalf("demote last admin status = %d, want 409", response.StatusCode)
	}
	if response := panel.do(t, panel.client, http.MethodDelete, "/api/v1/bindings/"+own.Bindings[0].ID, nil); response.StatusCode != http.StatusConflict {
		t.Fatalf("delete last admin status = %d, want 409", response.StatusCode)
	}
	if response := panel.do(t, panel.client, http.MethodDelete, "/api/v1/bindings/"+binding.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete bob binding status = %d, want 204", response.StatusCode)
	}

	// Every successful mutation is audited with the acting administrator.
	entries, err := panel.store.ListAudit(context.Background(), 200)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	actions := map[string]bool{}
	for _, entry := range entries {
		if entry.ActorID == admin.ID {
			actions[entry.Action] = true
		}
	}
	for _, action := range []string{"user_update", "user_disable", "user_enable", "binding_create",
		"binding_update", "binding_delete"} {
		if !actions[action] {
			t.Errorf("missing audit action %s", action)
		}
	}

	// A viewer is refused by the route permission before any handler runs.
	viewerClient, _ := criterionUser(t, panel, "viewer-admin-route", string(rbac.RoleViewer), store.RoleBinding{
		ScopeType: store.ScopeOrganization,
	})
	if response := panel.do(t, viewerClient, http.MethodGet, "/api/v1/users", nil); response.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer users status = %d, want 403", response.StatusCode)
	}
	for _, id := range []string{bob.ID, "00000000-0000-0000-0000-000000000099"} {
		if response := panel.do(t, viewerClient, http.MethodDelete, "/api/v1/users/"+id, map[string]string{"confirm_username": "bob"}); response.StatusCode != http.StatusForbidden {
			t.Errorf("viewer delete %s status = %d, want 403", id, response.StatusCode)
		}
	}
}

func TestDeleteUserAPI(t *testing.T) {
	panel := newTestPanel(t)
	admin := panel.createUser(t, "delete-admin", testPassword)
	panel.enableTOTP(t, "delete-admin", testPassword)
	target := createAdminExtraUser(t, panel, "delete-target")
	path := "/api/v1/users/" + target.ID
	delete := func(client *http.Client, body any) *http.Response {
		return panel.do(t, client, http.MethodDelete, path, body)
	}
	deleteID := func(id string, body any) *http.Response {
		return panel.do(t, panel.client, http.MethodDelete, "/api/v1/users/"+id, body)
	}
	if response := delete(panel.client, map[string]string{"confirm_username": target.Username}); response.StatusCode != http.StatusConflict || errorCode(t, response) != "user_not_disabled" {
		t.Fatalf("active delete = %d %q, want 409 user_not_disabled", response.StatusCode, errorCode(t, response))
	}
	for _, test := range []struct {
		body          map[string]string
		field, reason string
	}{
		{map[string]string{}, "confirm_username", "required"},
		{map[string]string{"confirm_username": "Delete-target"}, "confirm_username", "invalid_value"},
	} {
		response := delete(panel.client, test.body)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("confirmation status = %d, want 400", response.StatusCode)
		}
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Error struct {
				Field  string `json:"field"`
				Reason string `json:"reason"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Field != test.field || body.Error.Reason != test.reason || strings.Contains(string(raw), "Delete-target") {
			t.Fatalf("confirmation error body = %+v", body)
		}
	}
	if response := deleteID(admin.ID, map[string]string{"confirm_username": admin.Username}); response.StatusCode != http.StatusConflict || errorCode(t, response) != "cannot_delete_self" {
		t.Fatalf("self delete = %d, want 409 cannot_delete_self", response.StatusCode)
	}
	if response := deleteID("00000000-0000-0000-0000-000000000099", map[string]string{"confirm_username": "missing"}); response.StatusCode != http.StatusNotFound || errorCode(t, response) != "not_found" {
		t.Fatalf("unknown delete = %d, want 404 not_found", response.StatusCode)
	}
	target2 := createAdminExtraUser(t, panel, "delete-target-two")
	_, token := directAPIKey(t, panel, admin.ID, nil, time.Now().Add(time.Hour))
	if response := apiKeyRequest(t, panel, http.MethodDelete, "/api/v1/users/"+target2.ID, token, map[string]string{"confirm_username": target2.Username}, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("API key delete = %d, want 403", response.StatusCode)
	}
	oldToken := auth.NewSessionToken()
	if _, err := panel.store.CreateSession(context.Background(), target2.ID, auth.HashToken(oldToken), store.StageFull,
		panel.clock.Now().Add(fullSessionTTL), "192.0.2.10", "test"); err != nil {
		t.Fatal(err)
	}
	if response := panel.post(t, "/api/v1/users/"+target2.ID+"/disable", nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("disable target = %d", response.StatusCode)
	}
	if response := deleteID(target2.ID, map[string]string{"confirm_username": target2.Username}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete user = %d, want 204", response.StatusCode)
	}
	entries, err := panel.store.ListAudit(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	foundAudit := false
	for _, entry := range entries {
		if entry.Action == "user_delete" && entry.ActorID == admin.ID && entry.Details["user_id"] == target2.ID && entry.Details["username"] == target2.Username {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Fatal("missing user_delete audit event")
	}
	parsed, err := url.Parse(panel.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	oldClient := panel.newClient(t)
	oldClient.Jar.SetCookies(parsed, []*http.Cookie{sessionCookie(oldToken, store.StageFull)})
	if response := panel.do(t, oldClient, http.MethodGet, "/api/v1/me", nil); response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "session_required" {
		t.Fatalf("deleted user's cookie = %d %q, want 401 session_required", response.StatusCode, errorCode(t, response))
	}
}

func TestDeleteUserStoreErrorCodes(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{store.ErrLastAdmin, "last_admin"},
		{store.ErrUserNotDisabled, "user_not_disabled"},
		{store.ErrNotFound, "not_found"},
	} {
		t.Run(test.code, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			newUserAdminHandler(UserAdminDeps{}).writeStoreError(recorder, test.err)
			if recorder.Code != http.StatusConflict && test.code != "not_found" {
				t.Fatalf("status = %d, want 409", recorder.Code)
			}
			if test.code == "not_found" && recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", recorder.Code)
			}
			if code := errorCodeOf(t, recorder); code != test.code {
				t.Fatalf("code = %q, want %q", code, test.code)
			}
		})
	}
}

// createMFAUser leaves a full session for the new user in its own client.
func createMFAUser(t *testing.T, panel *testPanel, username string) (store.User, *http.Client, []string) {
	t.Helper()
	user := createAdminExtraUser(t, panel, username)
	client := panel.newClient(t)
	previous := panel.client
	panel.client = client
	defer func() { panel.client = previous }()
	_, codes := panel.enableTOTP(t, username, testPassword)
	return user, client, codes
}

func TestResetTOTPAPI(t *testing.T) {
	panel := newTestPanel(t)
	ctx := context.Background()
	admin := panel.createUser(t, "reset-admin", testPassword)
	adminSecret, _ := panel.enableTOTP(t, "reset-admin", testPassword)
	target, targetClient, recoveryCodes := createMFAUser(t, panel, "reset-target")
	if response := panel.createTrustedDevice(t, targetClient, "laptop"); response.StatusCode != http.StatusCreated {
		t.Fatalf("create target device status = %d, want 201", response.StatusCode)
	}

	code := panel.code(adminSecret)
	response := panel.post(t, "/api/v1/users/"+target.ID+"/reset-2fa", map[string]string{
		"confirm_username": target.Username,
		"totp_code":        code,
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reset status = %d, want 200", response.StatusCode)
	}
	var result resetTOTPResponse
	decodeBody(t, response, &result)
	if result.DevicesRevoked != 1 || result.SessionsRevoked < 1 {
		t.Fatalf("reset result = %+v, want one device and at least one session", result)
	}

	updated, err := panel.store.GetUser(ctx, target.ID)
	if err != nil {
		t.Fatalf("get reset target: %v", err)
	}
	if len(updated.TOTPSecretEnc) != 0 || !updated.TOTPEnabledAt.IsZero() || updated.TOTPLastStep != 0 {
		t.Fatalf("reset target still has TOTP state: %+v", updated)
	}
	devices, err := panel.store.ListTrustedDevices(ctx, target.ID)
	if err != nil {
		t.Fatalf("list target devices: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("target devices after reset = %+v, want none", devices)
	}
	if used, err := panel.store.UseRecoveryCode(ctx, target.ID,
		auth.HashToken(auth.NormalizeRecoveryCode(recoveryCodes[0]))); err != nil || used {
		t.Fatalf("recovery code after reset = %v, %v; want missing", used, err)
	}
	if response := panel.do(t, targetClient, http.MethodGet, "/api/v1/me", nil); response.StatusCode != http.StatusUnauthorized ||
		errorCode(t, response) != "session_required" {
		t.Fatalf("reset target session = %d %q, want 401 session_required", response.StatusCode, errorCode(t, response))
	}

	entries, err := panel.store.ListAudit(ctx, 200)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != "mfa_reset" || entry.ActorID != admin.ID {
			continue
		}
		found = true
		if entry.Details["user_id"] != target.ID || entry.Details["username"] != target.Username {
			t.Errorf("mfa_reset details = %+v", entry.Details)
		}
		if got, ok := entry.Details["sessions_revoked"].(float64); !ok || int(got) != result.SessionsRevoked {
			t.Errorf("audited sessions_revoked = %v, want %d", entry.Details["sessions_revoked"], result.SessionsRevoked)
		}
		if got, ok := entry.Details["devices_revoked"].(float64); !ok || int(got) != result.DevicesRevoked {
			t.Errorf("audited devices_revoked = %v, want %d", entry.Details["devices_revoked"], result.DevicesRevoked)
		}
	}
	if !found {
		t.Fatal("missing mfa_reset audit event")
	}

	// The same code has been consumed by the replay guard.
	other := createAdminExtraUser(t, panel, "reset-other")
	if response := panel.post(t, "/api/v1/users/"+other.ID+"/reset-2fa", map[string]string{
		"confirm_username": other.Username, "totp_code": code,
	}); response.StatusCode != http.StatusForbidden || errorCode(t, response) != "code_invalid" {
		t.Fatalf("reused code = %d %q, want 403 code_invalid", response.StatusCode, errorCode(t, response))
	}

	// A disabled target starts again at TOTP setup.
	login := panel.newClient(t)
	loginHTTPResponse := panel.do(t, login, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": target.Username, "password": testPassword,
	})
	if loginHTTPResponse.StatusCode != http.StatusOK {
		t.Fatalf("target login status = %d, want 200", loginHTTPResponse.StatusCode)
	}
	var loginBody loginResponse
	decodeBody(t, loginHTTPResponse, &loginBody)
	if loginBody.Next != "totp_setup" {
		t.Fatalf("target next step = %q, want totp_setup", loginBody.Next)
	}
}

func TestResetTOTPValidation(t *testing.T) {
	panel := newTestPanel(t)
	admin := panel.createUser(t, "reset-validation-admin", testPassword)
	adminSecret, _ := panel.enableTOTP(t, "reset-validation-admin", testPassword)
	target := createAdminExtraUser(t, panel, "reset-validation-target")
	unknown := "00000000-0000-0000-0000-000000000099"
	wrongCode := "000000"
	if wrongCode == panel.code(adminSecret) {
		wrongCode = "111111"
	}

	tests := []struct {
		name   string
		userID string
		body   map[string]string
		status int
		code   string
		field  string
		reason string
	}{
		{"self", admin.ID, nil, http.StatusConflict, "cannot_reset_self", "", ""},
		{"unknown user", unknown, nil, http.StatusNotFound, "not_found", "", ""},
		{"missing username", target.ID, map[string]string{}, http.StatusBadRequest, "invalid_request", "confirm_username", "required"},
		{"wrong username", target.ID, map[string]string{"confirm_username": "other"}, http.StatusBadRequest, "invalid_request", "confirm_username", "invalid_value"},
		{"missing code", target.ID, map[string]string{"confirm_username": target.Username}, http.StatusForbidden, "code_invalid", "", ""},
		{"wrong code", target.ID, map[string]string{"confirm_username": target.Username, "totp_code": wrongCode}, http.StatusForbidden, "code_invalid", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := panel.post(t, "/api/v1/users/"+test.userID+"/reset-2fa", test.body)
			if response.StatusCode != test.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.status)
			}
			var body errorBody
			decodeBody(t, response, &body)
			if body.Error.Code != test.code || body.Error.Field != test.field || body.Error.Reason != test.reason {
				t.Fatalf("error = %+v, want code=%q field=%q reason=%q", body.Error, test.code, test.field, test.reason)
			}
		})
	}

	entries, err := panel.store.ListAudit(context.Background(), 200)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	denied := 0
	for _, entry := range entries {
		if entry.Action == "mfa_reset_denied" && entry.ActorID == admin.ID &&
			entry.Details["user_id"] == target.ID && entry.Details["reason"] == "code_invalid" {
			denied++
		}
	}
	if denied < 2 {
		t.Fatalf("mfa_reset_denied events = %d, want at least 2", denied)
	}
}

func TestResetTOTPRateLimit(t *testing.T) {
	limiter := NewRateLimiterWithClock(50, time.Minute, time.Now)
	panel := newTestPanel(t, withLimiter(limiter))
	panel.createUser(t, "reset-limit-admin", testPassword)
	_, _ = panel.enableTOTP(t, "reset-limit-admin", testPassword)
	target := createAdminExtraUser(t, panel, "reset-limit-target")
	for _, ip := range []string{"127.0.0.1", "::1"} {
		for limiter.Allow(ip) {
		}
	}
	response := panel.post(t, "/api/v1/users/"+target.ID+"/reset-2fa", map[string]string{
		"confirm_username": target.Username, "totp_code": "000000",
	})
	if response.StatusCode != http.StatusTooManyRequests || errorCode(t, response) != "rate_limited" {
		t.Fatalf("rate-limited reset = %d %q, want 429 rate_limited", response.StatusCode, errorCode(t, response))
	}
}

func TestResetTOTPAuthorization(t *testing.T) {
	panel := newTestPanel(t)
	target := createAdminExtraUser(t, panel, "reset-authz-target")

	viewerClient, _ := criterionUser(t, panel, "reset-viewer", string(rbac.RoleViewer), store.RoleBinding{
		ScopeType: store.ScopeOrganization,
	})
	if response := panel.do(t, viewerClient, http.MethodPost, "/api/v1/users/"+target.ID+"/reset-2fa",
		map[string]string{"confirm_username": target.Username, "totp_code": "000000"}); response.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer reset = %d, want 403", response.StatusCode)
	}

	owner := panel.createUser(t, "reset-api-owner", testPassword)
	_, token := directAPIKey(t, panel, owner.ID, nil, time.Now().Add(time.Hour))
	if response := apiKeyRequest(t, panel, http.MethodPost, "/api/v1/users/"+target.ID+"/reset-2fa", token,
		map[string]string{"confirm_username": target.Username, "totp_code": "000000"}, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("API key reset = %d, want 403", response.StatusCode)
	}
}
