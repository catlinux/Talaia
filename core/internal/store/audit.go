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
	ActorID string
	Action  string
	IP      string
	Result  string
	Details map[string]any
	Scope   map[string]any
}

// AuditEntry is a stored audit event.
type AuditEntry struct {
	ID      string
	At      time.Time
	Actor   string
	ActorID string
	Action  string
	IP      string
	Result  string
	Details map[string]any
	Scope   map[string]any
}

// AuditCursor is the position of an entry in the audit log: its time and id.
type AuditCursor struct {
	At time.Time
	ID string
}

// AuditFilter selects a page of the audit log. Before is the cursor of the last
// entry of the previous page. An Action ending in "*" is a prefix filter.
type AuditFilter struct {
	Before   *AuditCursor
	Limit    int
	Action   string
	ServerID string
	Actor    string
}

const (
	// DefaultAuditLimit is the page size when the caller does not choose one.
	DefaultAuditLimit = 50
	// MaxAuditLimit is the largest page the core returns.
	MaxAuditLimit = 200
)

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
	var scope any
	if event.Scope != nil {
		scope = event.Scope
	}

	if _, err := s.pool.Exec(ctx,
		`INSERT INTO audit_event (actor, actor_id, action, ip, result, details, scope)
		 VALUES ($1, NULLIF($2, '')::uuid, $3, NULLIF($4, ''), $5, $6, $7)`,
		event.Actor, event.ActorID, event.Action, event.IP, event.Result, details, scope); err != nil {
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
		`SELECT id, at, actor, COALESCE(actor_id::text, ''), action, COALESCE(ip, ''), result, details, scope
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
		if err := rows.Scan(&entry.ID, &entry.At, &entry.Actor, &entry.ActorID,
			&entry.Action, &entry.IP, &entry.Result, &entry.Details, &entry.Scope); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read audit events: %w", err)
	}
	return entries, nil
}

// ListAuditPage returns one page of the audit log, newest first. The cursor is
// the time and id of the last row of the previous page, so pages neither repeat
// nor skip rows that share a timestamp. Action, ServerID and Actor are
// optional filters. An Action ending in "*" matches a prefix.
func (s *Store) ListAuditPage(ctx context.Context, filter AuditFilter) ([]AuditEntry, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = DefaultAuditLimit
	}
	if limit < 1 || limit > MaxAuditLimit {
		return nil, fmt.Errorf("audit limit must be between 1 and %d", MaxAuditLimit)
	}

	args := make([]any, 0, 6)
	placeholder := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	var query strings.Builder
	query.WriteString(`SELECT id, at, actor, COALESCE(actor_id::text, ''), action, COALESCE(ip, ''), result, details, scope
		FROM audit_event
		WHERE TRUE`)
	if filter.Action != "" {
		if strings.HasSuffix(filter.Action, "*") {
			prefix := strings.TrimSuffix(filter.Action, "*")
			query.WriteString(" AND action LIKE " + placeholder(escapeAuditLike(prefix)+"%") + " ESCAPE '!'")
		} else {
			query.WriteString(" AND action = " + placeholder(filter.Action))
		}
	}
	if filter.ServerID != "" {
		query.WriteString(" AND details->>'server_id' = " + placeholder(filter.ServerID))
	}
	if filter.Actor != "" {
		query.WriteString(" AND actor = " + placeholder(filter.Actor))
	}
	if filter.Before != nil {
		query.WriteString(" AND (at, id) < (" + placeholder(filter.Before.At) +
			"::timestamptz, " + placeholder(filter.Before.ID) + "::uuid)")
	}
	query.WriteString(" ORDER BY at DESC, id DESC LIMIT " + placeholder(limit))

	rows, err := s.pool.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list audit page: %w", err)
	}
	defer rows.Close()

	entries := make([]AuditEntry, 0, min(limit, 100))
	for rows.Next() {
		var entry AuditEntry
		if err := rows.Scan(&entry.ID, &entry.At, &entry.Actor, &entry.ActorID,
			&entry.Action, &entry.IP, &entry.Result, &entry.Details, &entry.Scope); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read audit events: %w", err)
	}
	return entries, nil
}

// escapeAuditLike escapes the wildcards of a LIKE pattern. "!" is the escape
// character used by the action prefix query.
func escapeAuditLike(value string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value)
}
