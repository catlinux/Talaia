package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AuditEvent is an event to append to the audit log. Details must never hold
// passwords, codes or tokens.
type AuditEvent struct {
	Actor   string
	Action  string
	IP      string
	Result  string
	Details map[string]any
}

// AuditEntry is a stored audit event.
type AuditEntry struct {
	ID      string
	At      time.Time
	Actor   string
	Action  string
	IP      string
	Result  string
	Details map[string]any
}

// AppendAudit adds an event to the log. The log is append-only: nothing in this
// package updates or deletes it.
func (s *Store) AppendAudit(ctx context.Context, event AuditEvent) error {
	if strings.TrimSpace(event.Action) == "" {
		return errors.New("audit action is required")
	}
	if strings.TrimSpace(event.Result) == "" {
		return errors.New("audit result is required")
	}

	// A nil map becomes NULL; anything else is stored as jsonb.
	var details any
	if event.Details != nil {
		details = event.Details
	}

	if _, err := s.pool.Exec(ctx,
		`INSERT INTO audit_event (actor, action, ip, result, details)
		 VALUES ($1, $2, NULLIF($3, ''), $4, $5)`,
		event.Actor, event.Action, event.IP, event.Result, details); err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	return nil
}

// ListAudit returns the newest events first.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, at, actor, action, COALESCE(ip, ''), result, details
		 FROM audit_event
		 ORDER BY at DESC, id DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	// Preallocate, but never trust a huge limit: the caller decides the page
	// size, the database the rows.
	entries := make([]AuditEntry, 0, min(limit, 100))
	for rows.Next() {
		var entry AuditEntry
		if err := rows.Scan(&entry.ID, &entry.At, &entry.Actor, &entry.Action,
			&entry.IP, &entry.Result, &entry.Details); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read audit events: %w", err)
	}
	return entries, nil
}
