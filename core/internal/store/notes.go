package store

import (
	"context"
	"fmt"
	"time"
)

// ResourceNote is one operator note about a container, a stack or a service.
type ResourceNote struct {
	Name      string
	Note      string
	UpdatedBy string
	UpdatedAt time.Time
}

// ListNotes returns the notes of one kind on a server, ordered by name.
func (s *Store) ListNotes(ctx context.Context, serverID, kind string) ([]ResourceNote, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT name, note, updated_by, updated_at
		 FROM resource_note
		 WHERE server_id = $1 AND kind = $2
		 ORDER BY name`, serverID, kind)
	if err != nil {
		return nil, fmt.Errorf("list notes for server %q: %w", serverID, err)
	}
	defer rows.Close()

	notes := make([]ResourceNote, 0)
	for rows.Next() {
		var note ResourceNote
		if err := rows.Scan(&note.Name, &note.Note, &note.UpdatedBy, &note.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan note for server %q: %w", serverID, err)
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notes for server %q: %w", serverID, err)
	}
	return notes, nil
}

// SetNote creates or replaces the note of one resource.
func (s *Store) SetNote(ctx context.Context, serverID, kind, name, note, actor string) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO resource_note (server_id, kind, name, note, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT (server_id, kind, name) DO UPDATE
		 SET note = EXCLUDED.note, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		serverID, kind, name, note, actor); err != nil {
		return fmt.Errorf("set %s note %q on server %q: %w", kind, name, serverID, err)
	}
	return nil
}

// DeleteNote removes the note of one resource. A missing note is not an error.
func (s *Store) DeleteNote(ctx context.Context, serverID, kind, name string) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM resource_note WHERE server_id = $1 AND kind = $2 AND name = $3`,
		serverID, kind, name); err != nil {
		return fmt.Errorf("delete %s note %q on server %q: %w", kind, name, serverID, err)
	}
	return nil
}
