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

func TestValidAlertCheck(t *testing.T) {
	tests := []struct {
		name  string
		check store.AlertCheck
		valid bool
	}{
		{"http", store.AlertCheck{Name: "web", Kind: "http", Target: "https://example.com/health", Config: json.RawMessage(`{"method":"GET"}`), IntervalSeconds: 60}, true},
		{"tcp", store.AlertCheck{Name: "tcp", Kind: "tcp", Target: "example.com:443", IntervalSeconds: 30}, true},
		{"tls", store.AlertCheck{Name: "tls", Kind: "tls", Target: "example.com:443", IntervalSeconds: 86400}, true},
		{"bad scheme", store.AlertCheck{Name: "bad", Kind: "http", Target: "ftp://example.com", IntervalSeconds: 60}, false},
		{"userinfo", store.AlertCheck{Name: "bad", Kind: "http", Target: "https://user:pass@example.com", IntervalSeconds: 60}, false},
		{"bad port", store.AlertCheck{Name: "bad", Kind: "tcp", Target: "example.com:70000", IntervalSeconds: 60}, false},
		{"bad interval low", store.AlertCheck{Name: "bad", Kind: "tcp", Target: "example.com:80", IntervalSeconds: 29}, false},
		{"bad interval high", store.AlertCheck{Name: "bad", Kind: "tcp", Target: "example.com:80", IntervalSeconds: 86401}, false},
		{"unknown kind", store.AlertCheck{Name: "bad", Kind: "icmp", Target: "example.com", IntervalSeconds: 60}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validAlertCheck(tc.check); got != tc.valid {
				t.Fatalf("validAlertCheck()=%v, want %v", got, tc.valid)
			}
		})
	}
}

type alertCheckStoreFake struct {
	AlertsStore
	check store.AlertCheck
}

func (f *alertCheckStoreFake) GetAlertCheck(context.Context, string) (store.AlertCheck, error) {
	return f.check, nil
}
func (f *alertCheckStoreFake) CreateAlertCheck(_ context.Context, v store.AlertCheck) (store.AlertCheck, error) {
	f.check = v
	f.check.ID = "check-id"
	return f.check, nil
}

type alertCheckControlFake struct {
	reloads int
	result  store.AlertCheckResult
	got     store.AlertCheck
}

func (f *alertCheckControlFake) Reload() { f.reloads++ }
func (f *alertCheckControlFake) RunOnce(_ context.Context, v store.AlertCheck) store.AlertCheckResult {
	f.got = v
	return f.result
}

func TestAlertCheckCreateReloadsAndAudits(t *testing.T) {
	st := &alertCheckStoreFake{}
	control := &alertCheckControlFake{}
	audit := &fakeAudit{}
	h := NewAlertCheckCreateHandler(AlertsDeps{Store: st, Checks: control, Audit: audit})
	r := httptest.NewRequest(http.MethodPost, "/checks", strings.NewReader(`{"name":"web","kind":"http","target":"https://example.com/health","interval_seconds":60}`))
	r = r.WithContext(withSession(r.Context(), Session{User: store.User{ID: "u1", Username: "operator"}, Stage: store.StageFull}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if control.reloads != 1 {
		t.Fatalf("Reload calls=%d", control.reloads)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "check_create" || strings.Contains(fmt.Sprint(audit.events[0].Details), "https://") {
		t.Fatalf("audit entries contain unexpected data: %+v", audit.events)
	}
}

func TestAlertCheckRunReturnsRunnerResult(t *testing.T) {
	want := store.AlertCheckResult{Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), CheckID: "check-id", OK: false, LatencyMS: 17, Error: "timeout"}
	st := &alertCheckStoreFake{check: store.AlertCheck{ID: "check-id", Name: "web", Kind: "http", Target: "https://example.com", IntervalSeconds: 60}}
	control := &alertCheckControlFake{result: want}
	audit := &fakeAudit{}
	h := NewAlertCheckRunHandler(AlertsDeps{Store: st, Checks: control, Audit: audit})
	r := httptest.NewRequest(http.MethodPost, "/checks/check-id/run", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("checkID", "check-id")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
	r = r.WithContext(withSession(r.Context(), Session{User: store.User{ID: "u1", Username: "operator"}, Stage: store.StageFull}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var got alertCheckResultJSON
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || control.got.ID != "check-id" || got.LatencyMS != want.LatencyMS || got.Error != want.Error || got.Time == nil || !got.Time.Equal(want.Time) {
		t.Fatalf("status=%d result=%+v check=%+v body=%s", w.Code, got, control.got, w.Body)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "check_run" {
		t.Fatalf("audit=%+v", audit.events)
	}
}

func TestAlertCheckHandlersUseStoreReloadAndAuditChanges(t *testing.T) {
	p := newTestPanel(t)
	user, effective := alertBinding(t, p, "check-handler-admin", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	control := &alertCheckControlFake{result: store.AlertCheckResult{Time: time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC), OK: true, LatencyMS: 19}}
	deps := AlertsDeps{Store: p.store, Checks: control, Audit: p.store}
	createBody := `{"name":"endpoint","kind":"http","target":"https://example.com/health","interval_seconds":60}`
	created := authorizedRequest(p, user, effective, http.MethodPost, "/checks", createBody, "", "", NewAlertCheckCreateHandler(deps))
	var check alertCheckJSON
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &check) != nil || check.ID == "" {
		t.Fatalf("create check status=%d body=%s", created.Code, created.Body)
	}
	if control.reloads != 1 {
		t.Fatalf("reload calls after create=%d", control.reloads)
	}

	firstTime := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	secondTime := firstTime.Add(time.Minute)
	if err := p.store.InsertAlertCheckResult(context.Background(), store.AlertCheckResult{CheckID: check.ID, Time: firstTime, OK: false, LatencyMS: 40, Error: "old failure"}); err != nil {
		t.Fatal(err)
	}
	statusCode := 204
	if err := p.store.InsertAlertCheckResult(context.Background(), store.AlertCheckResult{CheckID: check.ID, Time: secondTime, OK: true, LatencyMS: 12, StatusCode: &statusCode}); err != nil {
		t.Fatal(err)
	}
	listed := alertRequest(p, user, effective, http.MethodGet, "/checks", NewAlertChecksHandler(deps))
	var rows []alertCheckJSON
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &rows) != nil || len(rows) != 1 || rows[0].LatestResult == nil {
		t.Fatalf("list checks status=%d body=%s", listed.Code, listed.Body)
	}
	latest := rows[0].LatestResult
	if !latest.OK || latest.LatencyMS != 12 || latest.StatusCode == nil || *latest.StatusCode != statusCode || latest.Time == nil || !latest.Time.Equal(secondTime) {
		t.Fatalf("latest check result=%+v", latest)
	}

	updated := authorizedRequest(p, user, effective, http.MethodPatch, "/checks/"+check.ID, `{"enabled":false,"interval_seconds":120}`, "checkID", check.ID, NewAlertCheckUpdateHandler(deps))
	if updated.Code != http.StatusOK || control.reloads != 2 {
		t.Fatalf("update check status=%d reloads=%d body=%s", updated.Code, control.reloads, updated.Body)
	}
	stored, err := p.store.GetAlertCheck(context.Background(), check.ID)
	if err != nil || stored.Enabled || stored.IntervalSeconds != 120 {
		t.Fatalf("updated check=%+v err=%v", stored, err)
	}

	control.result = store.AlertCheckResult{Time: secondTime.Add(time.Minute), OK: false, LatencyMS: 8, Error: "timeout"}
	run := authorizedRequest(p, user, effective, http.MethodPost, "/checks/"+check.ID+"/run", "", "checkID", check.ID, NewAlertCheckRunHandler(deps))
	if run.Code != http.StatusOK {
		t.Fatalf("run check status=%d body=%s", run.Code, run.Body)
	}
	var result alertCheckResultJSON
	if err = json.Unmarshal(run.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || result.LatencyMS != 8 || result.Error != "timeout" || result.Time == nil || !result.Time.Equal(control.result.Time) {
		t.Fatalf("run result=%+v", result)
	}
	latestStored, err := p.store.LatestAlertCheckResult(context.Background(), check.ID)
	if err != nil || !latestStored.Time.Equal(secondTime) {
		t.Fatalf("run unexpectedly stored result: latest=%+v err=%v", latestStored, err)
	}

	deleted := authorizedRequest(p, user, effective, http.MethodDelete, "/checks/"+check.ID, "", "checkID", check.ID, NewAlertCheckDeleteHandler(deps))
	if deleted.Code != http.StatusOK || control.reloads != 3 {
		t.Fatalf("delete check status=%d reloads=%d body=%s", deleted.Code, control.reloads, deleted.Body)
	}
	if _, err = p.store.GetAlertCheck(context.Background(), check.ID); err == nil {
		t.Fatal("deleted check still exists")
	}

	audit, err := p.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range audit {
		if entry.Action == "check_run" || entry.Action == "check_create" || entry.Action == "check_update" || entry.Action == "check_delete" {
			seen[entry.Action] = true
			if strings.Contains(fmt.Sprint(entry.Details), "https://") || strings.Contains(fmt.Sprint(entry.Details), "timeout") {
				t.Errorf("audit details contain target or result: %#v", entry.Details)
			}
		}
	}
	for _, action := range []string{"check_create", "check_update", "check_delete", "check_run"} {
		if !seen[action] {
			t.Errorf("missing %s audit entry", action)
		}
	}
}

func TestAlertCheckHandlersRejectInvalidDefinitions(t *testing.T) {
	p := newTestPanel(t)
	user, effective := alertBinding(t, p, "check-invalid-admin", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	cases := []struct{ name, body string }{
		{"bad scheme", `{"name":"bad","kind":"http","target":"ftp://example.com","interval_seconds":60}`},
		{"userinfo", `{"name":"bad","kind":"http","target":"https://user:pass@example.com","interval_seconds":60}`},
		{"bad port", `{"name":"bad","kind":"tcp","target":"example.com:70000","interval_seconds":60}`},
		{"interval below minimum", `{"name":"bad","kind":"http","target":"https://example.com","interval_seconds":29}`},
		{"interval above maximum", `{"name":"bad","kind":"http","target":"https://example.com","interval_seconds":86401}`},
		{"unknown server", `{"name":"bad","kind":"http","target":"https://example.com","interval_seconds":60,"server_id":"00000000-0000-4000-8000-000000000001"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := authorizedRequest(p, user, effective, http.MethodPost, "/checks", tc.body, "", "", NewAlertCheckCreateHandler(AlertsDeps{Store: p.store}))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid check status=%d body=%s", w.Code, w.Body)
			}
		})
	}
}
