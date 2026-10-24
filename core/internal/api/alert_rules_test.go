package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/go-chi/chi/v5"
)

func TestValidAlertRuleParams(t *testing.T) {
	tests := []struct {
		name, kind, params string
		valid              bool
	}{
		{"server offline", "server_offline", `{"seconds":45}`, true},
		{"cpu", "cpu_high", `{"threshold":90,"for_seconds":0}`, true},
		{"memory", "mem_high", `{"threshold":90,"for_seconds":60}`, true},
		{"disk", "disk_high", `{"threshold":90,"for_seconds":0}`, true},
		{"container down", "container_down", `{"seconds":20}`, true},
		{"container unhealthy", "container_unhealthy", `{"seconds":60}`, true},
		{"check failure", "check_failed", `{"consecutive_failures":2}`, true},
		{"certificate", "cert_expiry", `{"days":14}`, true},
		{"bad range", "server_offline", `{"seconds":86401}`, false},
		{"missing param", "cpu_high", `{"threshold":90}`, false},
		{"extra param", "cert_expiry", `{"days":14,"token":"secret"}`, false},
		{"unknown type", "other", `{}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := store.AlertRule{Name: "test", Type: tc.kind, Severity: "warning", ScopeType: "organization", Params: json.RawMessage(tc.params)}
			if got := validAlertRule(v); got != tc.valid {
				t.Fatalf("validAlertRule()=%v, want %v", got, tc.valid)
			}
		})
	}
}

func TestAlertRuleScopeShape(t *testing.T) {
	valid := []store.AlertRule{
		{ScopeType: "organization"},
		{ScopeType: "server", ScopeServerID: stringPtr("server")},
		{ScopeType: "group", ScopeGroupID: stringPtr("group")},
		{ScopeType: "stack", ScopeServerID: stringPtr("server"), ScopeName: stringPtr("stack")},
		{ScopeType: "container", ScopeServerID: stringPtr("server"), ScopeName: stringPtr("container")},
	}
	for _, v := range valid {
		if !validAlertRuleScopeShape(v) {
			t.Errorf("expected valid scope: %+v", v)
		}
	}
	invalid := []store.AlertRule{
		{ScopeType: "server"},
		{ScopeType: "group", ScopeGroupID: stringPtr("group"), ScopeServerID: stringPtr("server")},
		{ScopeType: "stack", ScopeServerID: stringPtr("server")},
		{ScopeType: "container", ScopeServerID: stringPtr("server"), ScopeName: stringPtr(" ")},
		{ScopeType: "invalid"},
	}
	for _, v := range invalid {
		if validAlertRuleScopeShape(v) {
			t.Errorf("expected invalid scope: %+v", v)
		}
	}
}

func stringPtr(v string) *string { return &v }

func TestAlertRuleHandlersUseStoreAndAuditChanges(t *testing.T) {
	p := newTestPanel(t)
	user, effective := alertBinding(t, p, "rule-handler-admin", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	deps := AlertsDeps{Store: p.store, Audit: p.store}
	get := alertRequest(p, user, effective, http.MethodGet, "/alert-rules", NewAlertRulesHandler(deps))
	var builtins []alertRuleJSON
	if get.Code != http.StatusOK || json.Unmarshal(get.Body.Bytes(), &builtins) != nil || len(builtins) != 6 {
		t.Fatalf("list built-in rules status=%d body=%s count=%d", get.Code, get.Body, len(builtins))
	}
	for _, rule := range builtins {
		if !rule.Builtin {
			t.Fatalf("expected built-in rule: %+v", rule)
		}
	}

	createBody := `{"name":"CPU load","type":"cpu_high","severity":"warning","params":{"threshold":80,"for_seconds":5}}`
	created := authorizedRequest(p, user, effective, http.MethodPost, "/alert-rules", createBody, "", "", NewAlertRuleCreateHandler(deps))
	var rule alertRuleJSON
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &rule) != nil || rule.Type != "cpu_high" || rule.Builtin {
		t.Fatalf("create rule status=%d body=%s", created.Code, created.Body)
	}
	patchBody := `{"enabled":false,"params":{"threshold":70,"for_seconds":10},"channel_ids":[]}`
	updated := authorizedRequest(p, user, effective, http.MethodPatch, "/alert-rules/"+rule.ID, patchBody, "ruleID", rule.ID, NewAlertRuleUpdateHandler(deps))
	if updated.Code != http.StatusOK {
		t.Fatalf("update rule status=%d body=%s", updated.Code, updated.Body)
	}
	stored, err := p.store.GetAlertRule(context.Background(), rule.ID)
	var storedParams map[string]int
	if err == nil {
		err = json.Unmarshal(stored.Params, &storedParams)
	}
	if err != nil || stored.Enabled || storedParams["threshold"] != 70 || storedParams["for_seconds"] != 10 {
		t.Fatalf("updated rule=%+v err=%v", stored, err)
	}

	for _, builtin := range builtins {
		deleted := authorizedRequest(p, user, effective, http.MethodDelete, "/alert-rules/"+builtin.ID, "", "ruleID", builtin.ID, NewAlertRuleDeleteHandler(deps))
		if deleted.Code != http.StatusBadRequest && deleted.Code != http.StatusConflict {
			t.Fatalf("delete built-in status=%d body=%s", deleted.Code, deleted.Body)
		}
		if _, err := p.store.GetAlertRule(context.Background(), builtin.ID); err != nil {
			t.Fatalf("built-in rule %s disappeared: %v", builtin.ID, err)
		}
		break
	}
	deleted := authorizedRequest(p, user, effective, http.MethodDelete, "/alert-rules/"+rule.ID, "", "ruleID", rule.ID, NewAlertRuleDeleteHandler(deps))
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete user rule status=%d body=%s", deleted.Code, deleted.Body)
	}
	if _, err = p.store.GetAlertRule(context.Background(), rule.ID); err == nil {
		t.Fatal("deleted user rule still exists")
	}

	audit, err := p.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range audit {
		if entry.Action != "alert_rule_create" && entry.Action != "alert_rule_update" && entry.Action != "alert_rule_delete" {
			continue
		}
		seen[entry.Action] = true
		if len(entry.Details) != 2 || entry.Details["name"] != "CPU load" || entry.Details["type"] != "cpu_high" {
			t.Errorf("unexpected audit details for %s: %#v", entry.Action, entry.Details)
		}
		if strings.Contains(fmt.Sprint(entry.Details), "threshold") || strings.Contains(fmt.Sprint(entry.Details), "https://") || strings.Contains(fmt.Sprint(entry.Details), "secret") {
			t.Errorf("audit details contain params or secret-looking data: %#v", entry.Details)
		}
	}
	for _, action := range []string{"alert_rule_create", "alert_rule_update", "alert_rule_delete"} {
		if !seen[action] {
			t.Errorf("missing %s audit entry", action)
		}
	}
}

func TestAlertRuleHandlersRejectInvalidDefinitions(t *testing.T) {
	p := newTestPanel(t)
	user, effective := alertBinding(t, p, "rule-invalid-admin", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	cases := []struct{ name, body string }{
		{"unknown type", `{"name":"bad","type":"unknown","severity":"warning","params":{}}`},
		{"bad severity", `{"name":"bad","type":"cpu_high","severity":"urgent","params":{"threshold":80,"for_seconds":5}}`},
		{"threshold zero", `{"name":"bad","type":"cpu_high","severity":"warning","params":{"threshold":0,"for_seconds":5}}`},
		{"threshold 101", `{"name":"bad","type":"cpu_high","severity":"warning","params":{"threshold":101,"for_seconds":5}}`},
		{"for seconds over limit", `{"name":"bad","type":"cpu_high","severity":"warning","params":{"threshold":80,"for_seconds":86401}}`},
		{"certificate days zero", `{"name":"bad","type":"cert_expiry","severity":"warning","params":{"days":0}}`},
		{"server does not exist", `{"name":"bad","type":"cpu_high","severity":"warning","params":{"threshold":80,"for_seconds":5},"scope_type":"server","server_id":"00000000-0000-4000-8000-000000000001"}`},
		{"stack requires name", `{"name":"bad","type":"cpu_high","severity":"warning","params":{"threshold":80,"for_seconds":5},"scope_type":"stack","server_id":"00000000-0000-4000-8000-000000000001"}`},
		{"unknown channel", `{"name":"bad","type":"cpu_high","severity":"warning","params":{"threshold":80,"for_seconds":5},"channel_ids":["00000000-0000-4000-8000-000000000002"]}`},
		{"repeat interval over limit", `{"name":"bad","type":"cpu_high","severity":"warning","params":{"threshold":80,"for_seconds":5},"repeat_interval_seconds":604801}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := authorizedRequest(p, user, effective, http.MethodPost, "/alert-rules", tc.body, "", "", NewAlertRuleCreateHandler(AlertsDeps{Store: p.store}))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid rule status=%d body=%s json=%s", w.Code, w.Body, tc.body)
			}
		})
	}
}

func TestAlertRuleAndCheckRoutesAuthorizeReadsAndWrites(t *testing.T) {
	p := newTestPanel(t)
	authorizer := NewAuthorizer(AuthorizerDeps{Bindings: p.store, Servers: p.store, Audit: p.store})
	deps := AlertsDeps{Store: p.store, Checks: &alertCheckControlFake{}, Audit: p.store}
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		registerRoute(r, authorizer, http.MethodGet, "/alert-rules", NewAlertRulesHandler(deps))
		registerRoute(r, authorizer, http.MethodPost, "/alert-rules", NewAlertRuleCreateHandler(deps))
		registerRoute(r, authorizer, http.MethodPatch, "/alert-rules/{ruleID}", NewAlertRuleUpdateHandler(deps))
		registerRoute(r, authorizer, http.MethodDelete, "/alert-rules/{ruleID}", NewAlertRuleDeleteHandler(deps))
		registerRoute(r, authorizer, http.MethodGet, "/checks", NewAlertChecksHandler(deps))
		registerRoute(r, authorizer, http.MethodPost, "/checks", NewAlertCheckCreateHandler(deps))
		registerRoute(r, authorizer, http.MethodPatch, "/checks/{checkID}", NewAlertCheckUpdateHandler(deps))
		registerRoute(r, authorizer, http.MethodDelete, "/checks/{checkID}", NewAlertCheckDeleteHandler(deps))
		registerRoute(r, authorizer, http.MethodPost, "/checks/{checkID}/run", NewAlertCheckRunHandler(deps))
	})
	request := func(user *store.User, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		if user != nil {
			r = r.WithContext(withSession(r.Context(), Session{User: *user, Stage: store.StageFull}))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	reads := []struct{ method, path string }{{http.MethodGet, "/alert-rules"}, {http.MethodGet, "/checks"}}
	writes := []struct{ method, path, body string }{
		{http.MethodPost, "/alert-rules", `{"name":"route rule","type":"cpu_high","severity":"warning","params":{"threshold":80,"for_seconds":5}}`},
		{http.MethodPatch, "/alert-rules/00000000-0000-4000-8000-000000000001", `{"enabled":false}`},
		{http.MethodDelete, "/alert-rules/00000000-0000-4000-8000-000000000001", ""},
		{http.MethodPost, "/checks", `{"name":"route check","kind":"http","target":"https://example.com","interval_seconds":60}`},
		{http.MethodPatch, "/checks/00000000-0000-4000-8000-000000000001", `{"enabled":false}`},
		{http.MethodDelete, "/checks/00000000-0000-4000-8000-000000000001", ""},
		{http.MethodPost, "/checks/00000000-0000-4000-8000-000000000001/run", ""},
	}
	admin, _ := alertBinding(t, p, "alert-route-admin", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	if got := request(&admin, http.MethodGet, "/alert-rules", ""); got.Code != http.StatusOK {
		t.Fatalf("admin list rules status=%d body=%s", got.Code, got.Body)
	}
	if got := request(&admin, http.MethodGet, "/checks", ""); got.Code != http.StatusOK {
		t.Fatalf("admin list checks status=%d body=%s", got.Code, got.Body)
	}
	builtins, err := p.store.ListAlertRules(context.Background())
	if err != nil || len(builtins) == 0 {
		t.Fatalf("list built-in rules=%d err=%v", len(builtins), err)
	}
	rule, err := p.store.CreateAlertRule(context.Background(), store.AlertRule{Name: "route delete rule", Type: "cpu_high", Severity: "warning", Params: []byte(`{"threshold":80,"for_seconds":5}`), ScopeType: "organization"})
	if err != nil {
		t.Fatal(err)
	}
	check, err := p.store.CreateAlertCheck(context.Background(), store.AlertCheck{Name: "route target", Kind: "http", Target: "https://example.com", Config: []byte(`{}`), IntervalSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	adminWrites := []struct{ method, path, body string }{
		{http.MethodPost, "/alert-rules", writes[0].body},
		{http.MethodPatch, "/alert-rules/" + builtins[0].ID, `{"enabled":false}`},
		{http.MethodDelete, "/alert-rules/" + rule.ID, ""},
		{http.MethodPost, "/checks", writes[3].body},
		{http.MethodPatch, "/checks/" + check.ID, `{"enabled":false}`},
		{http.MethodPost, "/checks/" + check.ID + "/run", ""},
		{http.MethodDelete, "/checks/" + check.ID, ""},
	}
	for _, tc := range adminWrites {
		if got := request(&admin, tc.method, tc.path, tc.body); got.Code < 200 || got.Code >= 300 {
			t.Errorf("admin %s %s: status=%d body=%s", tc.method, tc.path, got.Code, got.Body)
		}
	}
	for _, role := range []string{string(rbac.RoleViewer), string(rbac.RoleMetricsOnly), string(rbac.RoleAppOperator), string(rbac.RoleServerOperator)} {
		user, _ := alertBinding(t, p, "alert-route-"+role, role, store.ScopeOrganization, "", "", "")
		for _, tc := range reads {
			if got := request(&user, tc.method, tc.path, ""); got.Code != http.StatusOK {
				t.Errorf("%s %s role %s: status=%d body=%s", tc.method, tc.path, role, got.Code, got.Body)
			}
		}
		for _, tc := range writes {
			if got := request(&user, tc.method, tc.path, tc.body); got.Code != http.StatusForbidden {
				t.Errorf("%s %s role %s: status=%d body=%s", tc.method, tc.path, role, got.Code, got.Body)
			}
		}
	}
	owner := admin
	key, err := p.store.CreateAPIKey(context.Background(), "alert-route-api-key", []byte("test-secret-hash"), "test", owner.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.store.CreateRoleBinding(context.Background(), store.RoleBinding{SubjectType: store.SubjectAPIKey, SubjectID: key.ID, Role: string(rbac.RoleAdmin), ScopeType: store.ScopeOrganization}); err != nil {
		t.Fatal(err)
	}
	requestAPIKey := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r = r.WithContext(withSession(r.Context(), Session{User: owner, Stage: store.StageFull, AuthKind: AuthKindAPIKey, APIKeyID: key.ID}))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, tc := range reads {
		if got := requestAPIKey(tc.method, tc.path, ""); got.Code != http.StatusOK {
			t.Errorf("API key GET %s: status=%d body=%s", tc.path, got.Code, got.Body)
		}
	}
	for _, tc := range writes {
		if got := requestAPIKey(tc.method, tc.path, tc.body); got.Code != http.StatusForbidden {
			t.Errorf("API key %s %s: status=%d body=%s", tc.method, tc.path, got.Code, got.Body)
		}
	}
	for _, tc := range reads {
		if got := request(nil, tc.method, tc.path, ""); got.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s: status=%d body=%s", tc.path, got.Code, got.Body)
		}
	}
}
