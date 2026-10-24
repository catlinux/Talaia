package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/go-chi/chi/v5"
)

func TestAlertEntryUsesSnakeCaseAndSummary(t *testing.T) {
	a := store.Alert{ID: "a", RuleType: "container", Severity: "critical", State: "firing", Summary: json.RawMessage(`{"message":"down"}`)}
	b, err := json.Marshal(alertEntry(a, "host"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err = json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"rule_type", "server_name", "subject_kind", "started_at", "resolved_at", "acked_at", "acked_by", "summary"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing %q", key)
		}
	}
}

func alertRequest(p *testPanel, user store.User, effective rbac.Effective, method, path string, handler http.Handler) *httptest.ResponseRecorder {
	return authorizedRequest(p, user, effective, method, path, "{}", "alertID", strings.TrimPrefix(path, "/alerts/"), handler)
}

func authorizedRequest(p *testPanel, user store.User, effective rbac.Effective, method, path, body, param, value string, handler http.Handler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := chi.NewRouteContext()
	if param != "" {
		ctx.URLParams.Add(param, value)
	}
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
	session := Session{User: user, Stage: store.StageFull}
	r = r.WithContext(withAuthorizer(withEffective(withSession(r.Context(), session), effective), NewAuthorizer(AuthorizerDeps{Bindings: p.store, Servers: p.store, Audit: p.store})))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func alertBinding(t *testing.T, p *testPanel, name, role, scopeType, serverID, groupID, scopeName string) (store.User, rbac.Effective) {
	t.Helper()
	ctx := context.Background()
	users, err := p.store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) == 0 {
		admin := p.createUser(t, name+"-bootstrap", testPassword)
		if role == string(rbac.RoleAdmin) && scopeType == store.ScopeOrganization {
			return admin, rbac.Resolve([]rbac.Binding{{Scope: rbac.Scope{Type: rbac.ScopeOrganization}, Role: rbac.RoleAdmin}})
		}
	} else if role == string(rbac.RoleAdmin) && scopeType == store.ScopeOrganization {
		return users[0], rbac.Resolve([]rbac.Binding{{Scope: rbac.Scope{Type: rbac.ScopeOrganization}, Role: rbac.RoleAdmin}})
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		t.Fatalf("random invite token: %v", err)
	}
	tokenHash := sha256.Sum256(token)
	_, err = p.store.CreateInvite(ctx, store.Invite{TokenHash: tokenHash[:], Username: name, Role: role, ScopeType: scopeType, ScopeServerID: serverID, ScopeGroupID: groupID, ScopeName: scopeName, CreatedBy: "test"})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	u, err := p.store.ConsumeInvite(ctx, tokenHash[:], auth.HashPassword(testPassword))
	if err != nil {
		t.Fatalf("consume invite: %v", err)
	}
	return u, rbac.Resolve([]rbac.Binding{{Scope: rbac.Scope{Type: rbac.ScopeType(scopeType), ServerID: serverID, GroupID: groupID, Name: scopeName}, Role: rbac.Role(role)}})
}

func openTestAlert(t *testing.T, p *testPanel, fingerprint string, serverID *string, kind, name, stack, severity string) store.Alert {
	t.Helper()
	a, _, err := p.store.OpenAlert(context.Background(), store.Alert{Fingerprint: fingerprint, ServerID: serverID, RuleType: kind, SubjectKind: kind, SubjectName: name, Stack: stack, Severity: severity, Summary: json.RawMessage(`{"ok":false}`), StartedAt: p.clock.Now()})
	if err != nil {
		t.Fatalf("open alert: %v", err)
	}
	return a
}

func TestAlertsFilterGroupBindingsAndOrganizationChecks(t *testing.T) {
	p := newTestPanel(t)
	serverA, err := p.store.CreateServer(context.Background(), "alerts-a-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	serverB, err := p.store.CreateServer(context.Background(), "alerts-b-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	group, err := p.store.CreateGroup(context.Background(), "alerts-group-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err = p.store.SetServerGroup(context.Background(), serverA, group.ID); err != nil {
		t.Fatal(err)
	}
	alertA := openTestAlert(t, p, "group-alert-"+serverA, &serverA, "container", "web", "shop", "critical")
	openTestAlert(t, p, "other-alert-"+serverB, &serverB, "container", "db", "data", "warning")
	orgAlert := openTestAlert(t, p, "org-alert", nil, "check", "health", "", "warning")
	groupUser, groupEffective := alertBinding(t, p, "alerts-group-user", string(rbac.RoleAppOperator), store.ScopeGroup, "", group.ID, "")
	list := alertRequest(p, groupUser, groupEffective, http.MethodGet, "/alerts", NewAlertsHandler(AlertsDeps{Store: p.store}))
	if list.Code != http.StatusOK {
		t.Fatalf("group list status=%d body=%s", list.Code, list.Body)
	}
	var rows []alertJSON
	if err = json.Unmarshal(list.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != alertA.ID || rows[0].ServerName == "" {
		t.Fatalf("group alerts = %+v", rows)
	}
	ack := alertRequest(p, groupUser, groupEffective, http.MethodPost, "/alerts/"+alertA.ID, NewAlertAckHandler(AlertsDeps{Store: p.store, Audit: p.store, Now: p.clock.Now}))
	if ack.Code != http.StatusOK {
		t.Fatalf("group ack status=%d body=%s", ack.Code, ack.Body)
	}
	stored, err := p.store.GetAlert(context.Background(), alertA.ID)
	if err != nil || stored.AckedBy != groupUser.Username {
		t.Fatalf("acked alert=%+v err=%v", stored, err)
	}
	audit, err := p.store.ListAudit(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	ackAudited := false
	for _, entry := range audit {
		if entry.Action == "alert_ack" && entry.Actor == groupUser.Username {
			ackAudited = true
			break
		}
	}
	if !ackAudited {
		t.Fatal("alert acknowledgement was not audited")
	}
	orgUser, orgEffective := alertBinding(t, p, "alerts-org-user", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	orgList := alertRequest(p, orgUser, orgEffective, http.MethodGet, "/alerts", NewAlertsHandler(AlertsDeps{Store: p.store}))
	var orgRows []alertJSON
	if err = json.Unmarshal(orgList.Body.Bytes(), &orgRows); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range orgRows {
		found = found || row.ID == orgAlert.ID
	}
	if !found {
		t.Fatalf("organization alert %s missing from %v", orgAlert.ID, orgRows)
	}
	groupSummary := alertRequest(p, groupUser, groupEffective, http.MethodGet, "/alerts/summary", NewAlertSummaryHandler(AlertsDeps{Store: p.store}))
	var summary struct {
		Counts  map[string]int    `json:"counts"`
		Servers map[string]string `json:"servers"`
	}
	if err = json.Unmarshal(groupSummary.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Counts["critical"] != 1 || summary.Counts["warning"] != 0 || summary.Servers[serverA] != "critical" {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestAlertHistoryRejectsInvalidPaging(t *testing.T) {
	p := newTestPanel(t)
	u, effective := alertBinding(t, p, "alerts-admin", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	for _, query := range []string{"?state=history&limit=nope", "?state=history&limit=0", "?state=history&limit=201", "?state=history&offset=-1", "?state=history&offset=x"} {
		r := httptest.NewRequest(http.MethodGet, "/alerts"+query, nil)
		r = r.WithContext(withEffective(withSession(r.Context(), Session{User: u, Stage: store.StageFull}), effective))
		w := httptest.NewRecorder()
		NewAlertsHandler(AlertsDeps{Store: p.store}).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", query, w.Code, w.Body)
		}
	}
}

func TestAlertHistoryHonorsLimitOffsetAndServerFilter(t *testing.T) {
	p := newTestPanel(t)
	serverA, err := p.store.CreateServer(context.Background(), "alert-history-a-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	serverB, err := p.store.CreateServer(context.Background(), "alert-history-b-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	a := openTestAlert(t, p, "history-a-"+serverA, &serverA, "server", "", "", "critical")
	p.clock.Advance(time.Second)
	b := openTestAlert(t, p, "history-b-"+serverB, &serverB, "server", "", "", "warning")
	if err = p.store.ResolveAlert(context.Background(), a.ID, p.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err = p.store.ResolveAlert(context.Background(), b.ID, p.clock.Now()); err != nil {
		t.Fatal(err)
	}
	user, effective := alertBinding(t, p, "alert-history-admin", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	for _, tc := range []struct {
		query string
		want  string
		count int
	}{
		{"?state=history&limit=1&offset=0", b.ID, 1},
		{"?state=history&limit=1&offset=1", a.ID, 1},
		{"?state=history&limit=20&offset=0&server_id=" + serverA, a.ID, 1},
	} {
		r := httptest.NewRequest(http.MethodGet, "/alerts"+tc.query, nil)
		r = r.WithContext(withEffective(withSession(r.Context(), Session{User: user, Stage: store.StageFull}), effective))
		w := httptest.NewRecorder()
		NewAlertsHandler(AlertsDeps{Store: p.store}).ServeHTTP(w, r)
		var rows []alertJSON
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", tc.query, w.Code, w.Body)
		}
		if err = json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != tc.count || rows[0].ID != tc.want {
			t.Errorf("%s alerts=%+v", tc.query, rows)
		}
	}
}

func TestAlertStackVisibilityAckAndUnwatch(t *testing.T) {
	p := newTestPanel(t)
	serverID, err := p.store.CreateServer(context.Background(), "alert-stack-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	owned := openTestAlert(t, p, "owned-stack-"+serverID, &serverID, "container", "web", "shop", "critical")
	other := openTestAlert(t, p, "other-stack-"+serverID, &serverID, "container", "db", "data", "warning")
	user, effective := alertBinding(t, p, "alert-stack-user", string(rbac.RoleAppOperator), store.ScopeStack, serverID, "", "shop")
	list := alertRequest(p, user, effective, http.MethodGet, "/alerts", NewAlertsHandler(AlertsDeps{Store: p.store}))
	var rows []alertJSON
	if err = json.Unmarshal(list.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != owned.ID {
		t.Fatalf("stack visible alerts=%+v", rows)
	}
	hiddenAck := alertRequest(p, user, effective, http.MethodPost, "/alerts/"+other.ID, NewAlertAckHandler(AlertsDeps{Store: p.store, Audit: p.store, Now: p.clock.Now}))
	if hiddenAck.Code != http.StatusNotFound {
		t.Fatalf("invisible alert ack status=%d", hiddenAck.Code)
	}
	unwatch := alertRequest(p, user, effective, http.MethodPost, "/alerts/"+owned.ID, NewAlertUnwatchHandler(AlertsDeps{Store: p.store, Audit: p.store, Hooks: p.alerts, Now: p.clock.Now}))
	if unwatch.Code != http.StatusOK {
		t.Fatalf("unwatch status=%d body=%s", unwatch.Code, unwatch.Body)
	}
	if len(p.alerts.containers) != 1 || p.alerts.containers[0] != "web" {
		t.Fatalf("unwatch hook calls=%v", p.alerts.containers)
	}
	resolved, err := p.store.GetAlert(context.Background(), owned.ID)
	if err != nil || resolved.State != "resolved" {
		t.Fatalf("resolved alert=%+v err=%v", resolved, err)
	}
	nonContainer := openTestAlert(t, p, "server-alert-"+serverID, &serverID, "check", "health", "shop", "warning")
	badUnwatch := alertRequest(p, user, effective, http.MethodPost, "/alerts/"+nonContainer.ID, NewAlertUnwatchHandler(AlertsDeps{Store: p.store, Hooks: p.alerts, Now: p.clock.Now}))
	if badUnwatch.Code != http.StatusBadRequest {
		t.Fatalf("non-container unwatch status=%d", badUnwatch.Code)
	}
}

func TestAlertViewerCanReadButCannotAcknowledge(t *testing.T) {
	p := newTestPanel(t)
	serverID, err := p.store.CreateServer(context.Background(), "alert-viewer-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	a := openTestAlert(t, p, "viewer-alert-"+serverID, &serverID, "server", "", "", "warning")
	user, effective := alertBinding(t, p, "alert-viewer", string(rbac.RoleViewer), store.ScopeServer, serverID, "", "")
	list := alertRequest(p, user, effective, http.MethodGet, "/alerts", NewAlertsHandler(AlertsDeps{Store: p.store}))
	if list.Code != http.StatusOK {
		t.Fatalf("viewer list status=%d", list.Code)
	}
	ack := alertRequest(p, user, effective, http.MethodPost, "/alerts/"+a.ID, NewAlertAckHandler(AlertsDeps{Store: p.store, Audit: p.store, Now: p.clock.Now}))
	if ack.Code != http.StatusForbidden {
		t.Fatalf("viewer ack status=%d", ack.Code)
	}
}

func TestViewerAndMetricsOnlyAlertPermissionMatrix(t *testing.T) {
	p := newTestPanel(t)
	serverID, err := p.store.CreateServer(context.Background(), "alert-readonly-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	a := openTestAlert(t, p, "readonly-alert-"+serverID, &serverID, "container", "web", "shop", "critical")
	for _, role := range []string{string(rbac.RoleViewer), string(rbac.RoleMetricsOnly)} {
		t.Run(role, func(t *testing.T) {
			user, effective := alertBinding(t, p, "alert-readonly-"+role, role, store.ScopeServer, serverID, "", "")
			if got := alertRequest(p, user, effective, http.MethodGet, "/alerts", NewAlertsHandler(AlertsDeps{Store: p.store})).Code; got != http.StatusOK {
				t.Fatalf("list status=%d", got)
			}
			if got := alertRequest(p, user, effective, http.MethodGet, "/alerts/summary", NewAlertSummaryHandler(AlertsDeps{Store: p.store})).Code; got != http.StatusOK {
				t.Fatalf("summary status=%d", got)
			}
			if got := alertRequest(p, user, effective, http.MethodGet, "/silences", NewSilenceListHandler(AlertsDeps{Store: p.store, Now: p.clock.Now})).Code; got != http.StatusOK {
				t.Fatalf("silence list status=%d", got)
			}
			deps := AlertsDeps{Store: p.store, Audit: p.store, Hooks: p.alerts, Now: p.clock.Now}
			for _, handler := range []http.Handler{NewAlertAckHandler(deps), NewAlertUnwatchHandler(deps)} {
				if got := alertRequest(p, user, effective, http.MethodPost, "/alerts/"+a.ID, handler).Code; got != http.StatusForbidden {
					t.Errorf("write status=%d, want 403", got)
				}
			}
			body := `{"scope_type":"server","server_id":"` + serverID + `","duration_minutes":10,"reason":"maintenance"}`
			if got := authorizedRequest(p, user, effective, http.MethodPost, "/silences", body, "", "", NewSilenceCreateHandler(deps)).Code; got != http.StatusForbidden {
				t.Errorf("silence create status=%d, want 403", got)
			}
		})
	}
}

func TestAlertRoutesAuthorizeAPIKeyAndRequireAuthentication(t *testing.T) {
	p := newTestPanel(t)
	serverID, err := p.store.CreateServer(context.Background(), "alert-api-key-server-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	alert := openTestAlert(t, p, "api-key-alert-"+serverID, &serverID, "server", "", "", "warning")
	owner := p.createUser(t, "alert-api-key-owner", testPassword)
	keyID := "alert-api-key"
	key, err := p.store.CreateAPIKey(context.Background(), keyID, []byte("test-secret-hash"), "test", owner.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.store.CreateRoleBinding(context.Background(), store.RoleBinding{SubjectType: store.SubjectAPIKey, SubjectID: key.ID, Role: string(rbac.RoleAdmin), ScopeType: store.ScopeOrganization})
	if err != nil {
		t.Fatalf("create api key binding: %v", err)
	}
	authorizer := NewAuthorizer(AuthorizerDeps{Bindings: p.store, Servers: p.store, Audit: p.store})
	deps := AlertsDeps{Store: p.store, Audit: p.store, Now: p.clock.Now}
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		registerRoute(r, authorizer, http.MethodGet, "/alerts", NewAlertsHandler(deps))
		registerRoute(r, authorizer, http.MethodPost, "/alerts/{alertID}/ack", NewAlertAckHandler(deps))
	})
	request := func(method, path string, apiKey bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if apiKey {
			r = r.WithContext(withSession(r.Context(), Session{User: owner, Stage: store.StageFull, AuthKind: AuthKindAPIKey, APIKeyID: key.ID}))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if got := request(http.MethodGet, "/api/v1/alerts", true); got.Code != http.StatusOK {
		t.Fatalf("api key list status=%d body=%s", got.Code, got.Body)
	}
	if got := request(http.MethodPost, "/api/v1/alerts/"+alert.ID+"/ack", true); got.Code != http.StatusOK {
		t.Fatalf("api key ack status=%d body=%s", got.Code, got.Body)
	}
	if got := request(http.MethodGet, "/api/v1/alerts", false); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list status=%d body=%s", got.Code, got.Body)
	}
}
