package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

func TestAuthorizationDenialBudgetLimitAndSummary(t *testing.T) {
	clock := newTestClock()
	budget := newDenialBudget()
	ip := "192.0.2.10"

	for i := 0; i < authzDenialMaxRows; i++ {
		events, write := budget.record(clock.Now(), "u1", "alice", "u1", ip)
		if !write || len(events) != 0 {
			t.Fatalf("denial %d: write=%v events=%+v", i+1, write, events)
		}
	}
	for i := 0; i < 5; i++ {
		events, write := budget.record(clock.Now(), "u1", "alice", "u1", ip)
		if write || len(events) != 0 {
			t.Fatalf("suppressed denial %d: write=%v events=%+v", i+1, write, events)
		}
	}

	clock.Advance(authzDenialWindow)
	events, write := budget.record(clock.Now(), "u1", "alice", "u1", ip)
	if !write || len(events) != 1 {
		t.Fatalf("new window: write=%v events=%+v", write, events)
	}
	summary := events[0]
	if summary.Action != authzDeniedSuppressedAction || summary.Result != "denied" ||
		summary.Actor != "alice" || summary.ActorID != "u1" || summary.IP != ip {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Scope != nil {
		t.Fatalf("summary scope = %+v", summary.Scope)
	}
	if summary.Details["suppressed"] != 5 ||
		summary.Details["window_seconds"] != int(authzDenialWindow/time.Second) ||
		len(summary.Details) != 2 {
		t.Fatalf("summary details = %+v", summary.Details)
	}
}

func TestAuthorizationDenialBudgetIsPerActorAndAPIKey(t *testing.T) {
	clock := newTestClock()
	budget := newDenialBudget()
	base := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}

	for i := 0; i < authzDenialMaxRows+1; i++ {
		budget.record(clock.Now(), denialBudgetKey(base), "alice", "u1", "192.0.2.10")
	}
	events, write := budget.record(clock.Now(), "u2", "bob", "u2", "192.0.2.11")
	if !write || len(events) != 0 {
		t.Fatalf("another actor was budgeted with the first: write=%v events=%+v", write, events)
	}

	key1 := base
	key1.AuthKind = AuthKindAPIKey
	key1.APIKeyID = "key-1"
	key2 := key1
	key2.APIKeyID = "key-2"
	for i := 0; i < authzDenialMaxRows+1; i++ {
		budget.record(clock.Now(), denialBudgetKey(key1), "alice", "u1", "192.0.2.10")
	}
	events, write = budget.record(clock.Now(), denialBudgetKey(key2), "alice", "u1", "192.0.2.10")
	if !write || len(events) != 0 {
		t.Fatalf("an API key was budgeted with another key: write=%v events=%+v", write, events)
	}
	if denialBudgetKey(base) == denialBudgetKey(key1) {
		t.Fatal("the session and API key share a budget key")
	}
}

func TestAuthorizationDenialBudgetFlushesExpiredOtherActors(t *testing.T) {
	clock := newTestClock()
	budget := newDenialBudget()
	for i := 0; i < authzDenialMaxRows+1; i++ {
		budget.record(clock.Now(), "a", "alice", "a", "192.0.2.1")
		budget.record(clock.Now(), "b", "bob", "b", "192.0.2.2")
	}
	budget.record(clock.Now(), "c", "carol", "c", "192.0.2.3")

	clock.Advance(authzDenialWindow)
	events, write := budget.record(clock.Now(), "d", "dave", "d", "192.0.2.4")
	if !write || len(events) != 2 {
		t.Fatalf("sweep: write=%v events=%+v", write, events)
	}
	seen := map[string]string{}
	for _, event := range events {
		seen[event.Actor] = event.IP
		if event.Action != authzDeniedSuppressedAction || event.Details["suppressed"] != 1 || event.Scope != nil {
			t.Fatalf("flush event = %+v", event)
		}
	}
	if seen["alice"] != "192.0.2.1" || seen["bob"] != "192.0.2.2" {
		t.Fatalf("flushed actors = %v", seen)
	}
	budget.mu.Lock()
	remaining := len(budget.windows)
	budget.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("budget entries = %d, want only the new actor", remaining)
	}
}

func TestAuthorizationDenialBudgetMapCap(t *testing.T) {
	t.Run("expired entries are swept before failing open", func(t *testing.T) {
		clock := newTestClock()
		now := clock.Now()
		budget := newDenialBudget()
		budget.mu.Lock()
		for i := 0; i < authzDenialMaxActors; i++ {
			budget.windows[fmt.Sprintf("old-%d", i)] = &denialWindow{
				start: now.Add(-authzDenialWindow - time.Minute),
			}
		}
		budget.mu.Unlock()

		events, write := budget.record(now, "new", "new", "new", "192.0.2.50")
		if !write || len(events) != 0 {
			t.Fatalf("write=%v events=%+v", write, events)
		}
		budget.mu.Lock()
		remaining := len(budget.windows)
		_, tracked := budget.windows["new"]
		budget.mu.Unlock()
		if remaining != 1 || !tracked {
			t.Fatalf("entries = %d, tracked new = %v", remaining, tracked)
		}
	})

	t.Run("a full live map fails open without growing", func(t *testing.T) {
		clock := newTestClock()
		now := clock.Now()
		budget := newDenialBudget()
		budget.mu.Lock()
		for i := 0; i < authzDenialMaxActors; i++ {
			budget.windows[fmt.Sprintf("live-%d", i)] = &denialWindow{start: now}
		}
		budget.mu.Unlock()

		events, write := budget.record(now, "new", "new", "new", "192.0.2.50")
		if !write || len(events) != 0 {
			t.Fatalf("write=%v events=%+v", write, events)
		}
		budget.mu.Lock()
		remaining := len(budget.windows)
		_, tracked := budget.windows["new"]
		budget.mu.Unlock()
		if remaining != authzDenialMaxActors || tracked {
			t.Fatalf("entries = %d, tracked new = %v", remaining, tracked)
		}
	})
}

func TestAuthorizerDenyKeepsResponseAndFlushesSummary(t *testing.T) {
	clock := newTestClock()
	audit := &fakeAudit{}
	authorizer := NewAuthorizer(AuthorizerDeps{Audit: audit, Now: clock.Now})
	session := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/s2/reboot", nil)

	deny := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		authorizer.deny(recorder, request, session, rbac.PermHostReboot, targetServer,
			rbac.Target{ServerID: "s2"}, http.StatusForbidden, "forbidden")
		return recorder
	}

	for i := 0; i < authzDenialMaxRows; i++ {
		recorder := deny()
		if recorder.Code != http.StatusForbidden || errorCodeOf(t, recorder) != "forbidden" {
			t.Fatalf("denial %d status = %d", i+1, recorder.Code)
		}
	}
	if len(audit.events) != authzDenialMaxRows {
		t.Fatalf("audit events = %d", len(audit.events))
	}

	recorder := deny()
	if recorder.Code != http.StatusForbidden || errorCodeOf(t, recorder) != "forbidden" {
		t.Fatalf("suppressed denial status = %d", recorder.Code)
	}
	if len(audit.events) != authzDenialMaxRows {
		t.Fatalf("suppressed denial added an audit row: %d", len(audit.events))
	}

	clock.Advance(authzDenialWindow)
	recorder = deny()
	if recorder.Code != http.StatusForbidden || errorCodeOf(t, recorder) != "forbidden" {
		t.Fatalf("new-window denial status = %d", recorder.Code)
	}
	if len(audit.events) != authzDenialMaxRows+2 {
		t.Fatalf("audit events = %d, want %d", len(audit.events), authzDenialMaxRows+2)
	}
	summary := audit.events[authzDenialMaxRows]
	if summary.Action != authzDeniedSuppressedAction || summary.Details["suppressed"] != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	own := audit.events[authzDenialMaxRows+1]
	if own.Action != authzDeniedAction || own.Details["path"] != "/api/v1/servers/s2/reboot" {
		t.Fatalf("own denial = %+v", own)
	}
}
