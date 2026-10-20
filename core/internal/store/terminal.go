package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TerminalSession is the record of one interactive shell in a container.
type TerminalSession struct {
	ID            string
	ServerID      string
	Container     string
	Shell         string
	CreatedBy     string
	Privileged    bool
	OpenedAt      time.Time
	ClosedAt      time.Time
	CloseReason   string
	RecordedBytes int64
}

// Open reports whether the session has not been closed.
func (t TerminalSession) Open() bool { return t.ClosedAt.IsZero() }

const terminalColumns = `id, server_id, container, shell, created_by, privileged, opened_at, closed_at, close_reason, recorded_bytes`

func scanTerminalSession(row pgx.Row) (TerminalSession, error) {
	var (
		session  TerminalSession
		closedAt *time.Time
	)
	if err := row.Scan(&session.ID, &session.ServerID, &session.Container, &session.Shell, &session.CreatedBy,
		&session.Privileged, &session.OpenedAt, &closedAt, &session.CloseReason, &session.RecordedBytes); err != nil {
		return TerminalSession{}, err
	}
	if closedAt != nil {
		session.ClosedAt = *closedAt
	}
	return session, nil
}

// CreateTerminalSession stores a new session and returns it with its id.
func (s *Store) CreateTerminalSession(ctx context.Context, serverID, container, shell, createdBy string) (TerminalSession, error) {
	session, err := scanTerminalSession(s.pool.QueryRow(ctx,
		`INSERT INTO terminal_session (server_id, container, shell, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+terminalColumns,
		serverID, container, shell, createdBy))
	if err != nil {
		return TerminalSession{}, fmt.Errorf("create terminal session: %w", err)
	}
	return session, nil
}

// SetTerminalPrivileged records that the container of a session is as good as
// root on the host.
func (s *Store) SetTerminalPrivileged(ctx context.Context, id string, privileged bool) error {
	if _, err := s.pool.Exec(ctx, `UPDATE terminal_session SET privileged = $2 WHERE id = $1`, id, privileged); err != nil {
		return fmt.Errorf("update terminal session: %w", err)
	}
	return nil
}

// AppendTerminalChunk stores one sealed chunk of the recording of a session and
// adds its plain size to the total.
func (s *Store) AppendTerminalChunk(ctx context.Context, id string, seq int, sealed []byte, plainBytes int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("append terminal chunk: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `INSERT INTO terminal_chunk (session_id, seq, data) VALUES ($1, $2, $3)`, id, seq, sealed); err != nil {
		return fmt.Errorf("append terminal chunk: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE terminal_session SET recorded_bytes = recorded_bytes + $2 WHERE id = $1`, id, plainBytes); err != nil {
		return fmt.Errorf("append terminal chunk: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("append terminal chunk: %w", err)
	}
	return nil
}

// CloseTerminalSession marks a session as closed. A session closed once keeps
// its first reason.
func (s *Store) CloseTerminalSession(ctx context.Context, id, reason string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE terminal_session SET closed_at = now(), close_reason = $2 WHERE id = $1 AND closed_at IS NULL`,
		id, reason); err != nil {
		return fmt.Errorf("close terminal session: %w", err)
	}
	return nil
}

// CloseOrphanTerminalSessions closes the sessions a restart of the core left
// open: their shells are gone with the streams.
func (s *Store) CloseOrphanTerminalSessions(ctx context.Context, reason string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE terminal_session SET closed_at = now(), close_reason = $1 WHERE closed_at IS NULL`, reason)
	if err != nil {
		return 0, fmt.Errorf("close orphan terminal sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// GetTerminalSession returns one session.
func (s *Store) GetTerminalSession(ctx context.Context, id string) (TerminalSession, error) {
	session, err := scanTerminalSession(s.pool.QueryRow(ctx, `SELECT `+terminalColumns+` FROM terminal_session WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return TerminalSession{}, ErrNotFound
	}
	if err != nil {
		return TerminalSession{}, fmt.Errorf("get terminal session: %w", err)
	}
	return session, nil
}

// ListTerminalSessions returns the most recent sessions first.
func (s *Store) ListTerminalSessions(ctx context.Context, limit int) ([]TerminalSession, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+terminalColumns+` FROM terminal_session ORDER BY opened_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list terminal sessions: %w", err)
	}
	defer rows.Close()

	var sessions []TerminalSession
	for rows.Next() {
		session, err := scanTerminalSession(rows)
		if err != nil {
			return nil, fmt.Errorf("list terminal sessions: %w", err)
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// TerminalChunk is one sealed piece of a recording.
type TerminalChunk struct {
	Seq    int
	Sealed []byte
}

// TerminalChunks returns the sealed chunks of a session in order.
func (s *Store) TerminalChunks(ctx context.Context, id string) ([]TerminalChunk, error) {
	rows, err := s.pool.Query(ctx, `SELECT seq, data FROM terminal_chunk WHERE session_id = $1 ORDER BY seq`, id)
	if err != nil {
		return nil, fmt.Errorf("read terminal chunks: %w", err)
	}
	defer rows.Close()

	var chunks []TerminalChunk
	for rows.Next() {
		var chunk TerminalChunk
		if err := rows.Scan(&chunk.Seq, &chunk.Sealed); err != nil {
			return nil, fmt.Errorf("read terminal chunks: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
}

// PurgeTerminalSessions deletes the closed sessions opened before cutoff, with
// their recordings, and returns how many it deleted.
func (s *Store) PurgeTerminalSessions(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM terminal_session WHERE closed_at IS NOT NULL AND opened_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge terminal sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
