package store

import (
	"context"
	"fmt"
	"testing"
	"time"
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
