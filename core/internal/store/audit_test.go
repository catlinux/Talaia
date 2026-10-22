package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// auditAction returns an action no other test uses and schedules the removal of
// that test's events: the audit table is append-only for the core code, but the
// tests should not leave rows behind on every run.
func auditAction(t *testing.T, s *Store) string {
	t.Helper()

	action := fmt.Sprintf("test.action.%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM audit_event WHERE action = $1`, action)
	})
	return action
}

// findAudit returns the entries of an action, newest first.
func findAudit(t *testing.T, entries []AuditEntry, action string) []AuditEntry {
	t.Helper()

	var found []AuditEntry
	for _, entry := range entries {
		if entry.Action == action {
			found = append(found, entry)
		}
	}
	return found
}

func TestAppendAuditAndListIt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	action := auditAction(t, s)
	events := []AuditEvent{
		{Actor: "admin", Action: action, IP: "192.0.2.20", Result: "success", Details: map[string]any{"step": "login"}},
		{Actor: "admin", Action: action, Result: "failure"},
		{Actor: "cli", Action: action, IP: "192.0.2.21", Result: "success", Details: map[string]any{"count": 2.0}},
	}
	for _, event := range events {
		if err := s.AppendAudit(ctx, event); err != nil {
			t.Fatalf("append audit: %v", err)
		}
	}

	entries, err := s.ListAudit(ctx, 100)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}

	found := findAudit(t, entries, action)
	if len(found) != len(events) {
		t.Fatalf("entries of %q = %d, want %d", action, len(found), len(events))
	}

	// Newest first: the third event was appended last.
	if found[0].Actor != "cli" || found[0].Result != "success" {
		t.Errorf("first entry = %+v, want the last appended event", found[0])
	}
	if found[0].Details["count"] != 2.0 {
		t.Errorf("details = %v, want the stored map", found[0].Details)
	}

	if found[1].Actor != "admin" || found[1].Result != "failure" {
		t.Errorf("second entry = %+v", found[1])
	}
	if found[1].IP != "" {
		t.Errorf("ip = %q, want empty for an event without address", found[1].IP)
	}
	if found[1].Details != nil {
		t.Errorf("details = %v, want nil", found[1].Details)
	}

	if found[2].Details["step"] != "login" {
		t.Errorf("details = %v, want the stored map", found[2].Details)
	}

	for _, entry := range found {
		if entry.ID == "" || entry.At.IsZero() {
			t.Errorf("entry without id or time: %+v", entry)
		}
	}
}

func TestListAuditReturnsTheNewestFirstAndHonoursTheLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	action := auditAction(t, s)
	for i := 0; i < 3; i++ {
		if err := s.AppendAudit(ctx, AuditEvent{
			Actor: "admin", Action: action, Result: "success",
			Details: map[string]any{"index": float64(i)},
		}); err != nil {
			t.Fatalf("append audit: %v", err)
		}
	}

	// The limit cuts the list, and the newest event is the first one.
	entries, err := s.ListAudit(ctx, 1)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].At.After(entries[i-1].At) {
			t.Error("the list is not ordered newest first")
		}
	}
}

func TestAppendAuditValidatesItsInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.AppendAudit(ctx, AuditEvent{Action: " ", Result: "success"}); err == nil {
		t.Error("expected an error for an empty action")
	}
	if err := s.AppendAudit(ctx, AuditEvent{Action: "test", Result: " "}); err == nil {
		t.Error("expected an error for an empty result")
	}
	if _, err := s.ListAudit(ctx, 0); err == nil {
		t.Error("expected an error for a non-positive limit")
	}
}

// insertAuditAt adds an event with an explicit time, which AppendAudit cannot
// set. The tests use it to create rows that share a timestamp.
func insertAuditAt(t *testing.T, s *Store, at time.Time, actor, action string, details map[string]any) string {
	t.Helper()

	var id string
	if err := s.pool.QueryRow(context.Background(),
		`INSERT INTO audit_event (at, actor, action, result, details)
		 VALUES ($1, $2, $3, 'success', $4)
		 RETURNING id`,
		at, actor, action, details).Scan(&id); err != nil {
		t.Fatalf("insert audit event: %v", err)
	}
	return id
}

func TestListAuditPagePaginatesWithoutGapsOrDuplicates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	action := auditAction(t, s)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	want := make(map[string]bool, 5)
	for i := 0; i < 5; i++ {
		id := insertAuditAt(t, s, at, "admin", action, map[string]any{"index": float64(i)})
		want[id] = true
	}

	seen := make(map[string]bool, len(want))
	var before *AuditCursor
	pages := 0
	for {
		entries, err := s.ListAuditPage(ctx, AuditFilter{
			Before: before,
			Limit:  2,
			Action: action,
		})
		if err != nil {
			t.Fatalf("list audit page %d: %v", pages+1, err)
		}
		pages++
		for _, entry := range entries {
			if seen[entry.ID] {
				t.Fatalf("entry %s appears twice", entry.ID)
			}
			seen[entry.ID] = true
			if !entry.At.Equal(at) {
				t.Errorf("at = %v, want %v", entry.At, at)
			}
		}
		if len(entries) < 2 {
			break
		}
		last := entries[len(entries)-1]
		before = &AuditCursor{At: last.At, ID: last.ID}
	}

	if pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
	if len(seen) != len(want) {
		t.Fatalf("entries = %d, want %d", len(seen), len(want))
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("entry %s was skipped", id)
		}
	}
}

func TestListAuditPageFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	base := fmt.Sprintf("test.filter.%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(),
			`DELETE FROM audit_event WHERE action LIKE $1`, base+"%")
	})

	serverOne := uuid.NewString()
	serverTwo := uuid.NewString()
	actorOne := "audit-one-" + fmt.Sprint(time.Now().UnixNano())
	actorTwo := "audit-two-" + fmt.Sprint(time.Now().UnixNano())
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	events := []struct {
		actor   string
		action  string
		details map[string]any
	}{
		{actor: actorOne, action: base + "_one", details: map[string]any{"server_id": serverOne}},
		{actor: actorOne, action: base + "_two", details: map[string]any{"server_id": serverTwo}},
		{actor: actorTwo, action: base + "Xone", details: map[string]any{"server_id": serverOne}},
		{actor: actorTwo, action: base + "%one", details: map[string]any{"server_id": serverTwo}},
	}
	for i, event := range events {
		insertAuditAt(t, s, at.Add(time.Duration(i)*time.Second), event.actor, event.action, event.details)
	}

	tests := []struct {
		name   string
		filter AuditFilter
		want   int
	}{
		{
			name:   "exact action",
			filter: AuditFilter{Action: base + "_one"},
			want:   1,
		},
		{
			name:   "action prefix escapes underscore",
			filter: AuditFilter{Action: base + "_*"},
			want:   2,
		},
		{
			name:   "action prefix escapes percent",
			filter: AuditFilter{Action: base + "%*"},
			want:   1,
		},
		{
			name:   "server",
			filter: AuditFilter{ServerID: serverOne},
			want:   2,
		},
		{
			name:   "actor",
			filter: AuditFilter{Actor: actorTwo},
			want:   2,
		},
		{
			name:   "filters combine",
			filter: AuditFilter{Action: base + "_*", Actor: actorOne, ServerID: serverOne},
			want:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.filter.Limit = 50
			entries, err := s.ListAuditPage(ctx, tt.filter)
			if err != nil {
				t.Fatalf("list audit page: %v", err)
			}
			if len(entries) != tt.want {
				t.Fatalf("entries = %d, want %d", len(entries), tt.want)
			}
		})
	}
}

func TestListAuditPageLimits(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		limit   int
		wantErr bool
	}{
		{name: "default", limit: 0},
		{name: "smallest", limit: 1},
		{name: "largest", limit: MaxAuditLimit},
		{name: "negative", limit: -1, wantErr: true},
		{name: "too large", limit: MaxAuditLimit + 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.ListAuditPage(ctx, AuditFilter{Limit: tt.limit})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
