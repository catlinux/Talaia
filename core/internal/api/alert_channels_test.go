package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/catlinux/Talaia/core/internal/notify"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

type failingChannelTester struct{ err error }

// The fake bot tokens are assembled at run time: a literal with the shape of a
// real Telegram token in the source would be reported by secret scanners.
var (
	fakeFirstToken       = "123456789:" + strings.Repeat("F", 35)
	fakeReplacementToken = "123456789:" + strings.Repeat("R", 35)
)

func (f failingChannelTester) SendTest(context.Context, string) error { return f.err }

func TestAlertChannelEntryNeverSerializesSecret(t *testing.T) {
	entry := alertChannelEntry(store.NotificationChannel{
		ID: "channel", Name: "ops", Type: "telegram", Enabled: true,
		Language: "en", Config: json.RawMessage(`{"chat_id":"123"}`), HasSecret: true,
	})
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	var got map[string]json.RawMessage
	if err = json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["secret"]; ok || strings.Contains(text, "actual-token") {
		t.Fatalf("channel response contains a secret: %s", text)
	}
	for _, field := range []string{"id", "name", "type", "enabled", "language", "config", "has_secret", "last_error", "last_ok_at"} {
		if _, ok := got[field]; !ok {
			t.Errorf("missing %q in %s", field, text)
		}
	}
}

func TestAlertChannelValidationUsesNotifierRules(t *testing.T) {
	tests := []struct {
		name   string
		typ    string
		config string
		secret string
		valid  bool
	}{
		{"discord rejects another host", "discord", `{}`, `{"url":"https://example.com/api/webhooks/1/token"}`, false},
		{"discord accepts its webhook URL in the secret", "discord", `{}`, `{"url":"https://discord.com/api/webhooks/1/token"}`, true},
		{"discord URL in the public config is not enough", "discord", `{"url":"https://discord.com/api/webhooks/1/token"}`, `{"url":"https://evil.example.com/x"}`, false},
		{"public http webhook is accepted", "webhook", `{}`, `{"url":"http://example.com/hook"}`, true},
		{"webhook with a file URL is rejected", "webhook", `{}`, `{"url":"file:///etc/passwd"}`, false},
		{"telegram rejects a token that is not a bot token", "telegram", `{"chat_id":"1"}`, `{"token":"../../x"}`, false},
		{"email requires tls mode", "email", `{"host":"mail.example.com","port":587,"from":"a@example.com","to":["b@example.com"]}`, `{"username":"user","password":"pass"}`, false},
		{"telegram requires chat id", "telegram", `{}`, `{"token":"token"}`, false},
		{"unknown type is rejected", "sms", `{}`, `{}`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := notify.ValidateChannel(test.typ, json.RawMessage(test.config), json.RawMessage(test.secret))
			if (err == nil) != test.valid {
				t.Fatalf("ValidateChannel error=%v, valid=%t", err, test.valid)
			}
		})
	}
}

func TestAlertChannelAndSettingsRoutesRequireOrganizationAdminSession(t *testing.T) {
	routes := []routeKey{
		{http.MethodGet, "/channels"},
		{http.MethodPost, "/channels"},
		{http.MethodPatch, "/channels/{channelID}"},
		{http.MethodDelete, "/channels/{channelID}"},
		{http.MethodPost, "/channels/{channelID}/test"},
		{http.MethodGet, "/settings/alerts"},
		{http.MethodPut, "/settings/alerts"},
	}
	adminBinding := store.RoleBinding{SubjectType: store.SubjectUser, SubjectID: "u1", ScopeType: store.ScopeOrganization, Role: string(rbac.RoleAdmin)}
	apiKeyBinding := store.RoleBinding{SubjectType: store.SubjectAPIKey, SubjectID: "key1", ScopeType: store.ScopeOrganization, Role: string(rbac.RoleAdmin)}
	bindings := stubBindings{
		byUser:    map[string][]store.RoleBinding{"u1": {adminBinding}},
		bySubject: map[string][]store.RoleBinding{store.SubjectAPIKey + "/key1": {apiKeyBinding}},
	}
	handler := routeTestRouter(t, NewAuthorizer(AuthorizerDeps{Bindings: bindings}))
	for _, route := range routes {
		path := strings.ReplaceAll(routePath(route.Method, route.Pattern), "{channelID}", "channel-1")
		adminRequest := authzRequest(route.Method, path, Session{User: store.User{ID: "u1", Username: "admin"}, Stage: store.StageFull})
		if got := doAuthorized(t, handler, adminRequest); got != http.StatusNoContent {
			t.Errorf("%s %s for admin: status=%d, want 204", route.Method, route.Pattern, got)
		}
		for _, role := range []rbac.Role{rbac.RoleViewer, rbac.RoleMetricsOnly, rbac.RoleServerOperator, rbac.RoleAppOperator} {
			request := authzRequest(route.Method, path, Session{User: store.User{ID: "u1", Username: "reader"}, Stage: store.StageFull})
			request = request.WithContext(withEffective(request.Context(), rbac.Resolve([]rbac.Binding{{Scope: rbac.Scope{Type: rbac.ScopeOrganization}, Role: role}})))
			if got := doAuthorized(t, handler, request); got != http.StatusForbidden {
				t.Errorf("%s %s for %s: status=%d, want 403", route.Method, route.Pattern, role, got)
			}
		}
		keyRequest := authzRequest(route.Method, path, Session{User: store.User{ID: "u1", Username: "admin"}, Stage: store.StageFull, AuthKind: AuthKindAPIKey, APIKeyID: "key1"})
		if got := doAuthorized(t, handler, keyRequest); got != http.StatusForbidden {
			t.Errorf("%s %s for API key: status=%d, want 403", route.Method, route.Pattern, got)
		}
		if got := doAuthorized(t, handler, httptest.NewRequest(route.Method, path, nil)); got != http.StatusUnauthorized {
			t.Errorf("%s %s without authentication: status=%d, want 401", route.Method, route.Pattern, got)
		}
	}
}

func TestAlertChannelSecretsStaySealedAndOutOfResponsesAndAudit(t *testing.T) {
	p := newTestPanel(t)
	user, effective := alertBinding(t, p, "channel-secrets-admin", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	deps := AlertsDeps{Store: p.store, Box: p.box, Audit: p.store}
	create := authorizedRequest(p, user, effective, http.MethodPost, "/channels", `{"name":"ops","type":"telegram","enabled":true,"language":"en","config":{"chat_id":"123"},"secret":{"token":"`+fakeFirstToken+`"}}`, "", "", NewAlertChannelCreateHandler(deps))
	if create.Code != http.StatusCreated || strings.Contains(create.Body.String(), fakeFirstToken) {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body)
	}
	var created alertChannelJSON
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	sealed, err := p.store.GetChannelSecret(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := p.box.Open(sealed)
	if err != nil || !bytes.Contains(plain, []byte(fakeFirstToken)) {
		t.Fatalf("stored secret was not sealed JSON: plain=%s err=%v", plain, err)
	}

	before := append([]byte(nil), sealed...)
	patch := authorizedRequest(p, user, effective, http.MethodPatch, "/channels/"+created.ID, `{"name":"ops-updated"}`, "channelID", created.ID, NewAlertChannelUpdateHandler(deps))
	if patch.Code != http.StatusOK || strings.Contains(patch.Body.String(), fakeFirstToken) {
		t.Fatalf("patch without secret status=%d body=%s", patch.Code, patch.Body)
	}
	sealed, err = p.store.GetChannelSecret(context.Background(), created.ID)
	if err != nil || !bytes.Equal(before, sealed) {
		t.Fatalf("patch without secret changed sealed secret: err=%v", err)
	}
	plain, err = p.box.Open(sealed)
	if err != nil || !bytes.Contains(plain, []byte(fakeFirstToken)) {
		t.Fatalf("kept secret did not open: err=%v", err)
	}

	replace := authorizedRequest(p, user, effective, http.MethodPatch, "/channels/"+created.ID, `{"secret":{"token":"`+fakeReplacementToken+`"}}`, "channelID", created.ID, NewAlertChannelUpdateHandler(deps))
	if replace.Code != http.StatusOK || strings.Contains(replace.Body.String(), fakeReplacementToken) {
		t.Fatalf("patch with secret status=%d body=%s", replace.Code, replace.Body)
	}
	sealed, err = p.store.GetChannelSecret(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err = p.box.Open(sealed)
	if err != nil || !bytes.Contains(plain, []byte(fakeReplacementToken)) {
		t.Fatalf("replacement secret was not stored: err=%v", err)
	}

	deps.Notifier = failingChannelTester{err: errors.New("send failed with " + fakeReplacementToken)}
	test := authorizedRequest(p, user, effective, http.MethodPost, "/channels/"+created.ID+"/test", `{}`, "channelID", created.ID, NewAlertChannelTestHandler(deps))
	if test.Code != http.StatusOK || strings.Contains(test.Body.String(), fakeReplacementToken) || !strings.Contains(test.Body.String(), "[redacted]") {
		t.Fatalf("test response status=%d body=%s", test.Code, test.Body)
	}
	audit, err := p.store.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range audit {
		encoded, _ := json.Marshal(event.Details)
		if bytes.Contains(encoded, []byte(fakeFirstToken)) || bytes.Contains(encoded, []byte(fakeReplacementToken)) {
			t.Fatalf("audit event %q contains a channel secret: %s", event.Action, encoded)
		}
	}
	list := authorizedRequest(p, user, effective, http.MethodGet, "/channels", `{}`, "", "", NewAlertChannelsHandler(deps))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), fakeReplacementToken) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body)
	}
}
