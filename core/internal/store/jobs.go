package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Job states. They only move forward.
const (
	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

// Job is one typed command sent to the agent of a server.
type Job struct {
	ID       string
	ServerID string
	Kind     string
	Params   map[string]any
	State    string
	Message  string
	// Output is the text the job produced, bounded to MaxJobOutput bytes.
	Output     string
	CreatedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	FinishedAt time.Time
}

// Done reports whether the job has finished, successfully or not.
func (j Job) Done() bool {
	return j.State == JobSucceeded || j.State == JobFailed
}

// MaxJobOutput bounds the output stored for a job.
const MaxJobOutput = 256 << 10

const jobColumns = `id, server_id, kind, params, state, message, output, created_by, created_at, updated_at, finished_at`

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJob(row jobScanner) (Job, error) {
	var (
		job        Job
		finishedAt *time.Time
	)
	if err := row.Scan(&job.ID, &job.ServerID, &job.Kind, &job.Params, &job.State, &job.Message, &job.Output,
		&job.CreatedBy, &job.CreatedAt, &job.UpdatedAt, &finishedAt); err != nil {
		return Job{}, err
	}
	if finishedAt != nil {
		job.FinishedAt = *finishedAt
	}
	return job, nil
}

// CreateJob stores a new pending job for a server.
func (s *Store) CreateJob(ctx context.Context, serverID, kind string, params map[string]any, createdBy string) (Job, error) {
	if strings.TrimSpace(serverID) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(createdBy) == "" {
		return Job{}, errors.New("server id, kind and creator are required")
	}
	if params == nil {
		params = map[string]any{}
	}

	job, err := scanJob(s.pool.QueryRow(ctx,
		`INSERT INTO job (server_id, kind, params, state, created_by)
		 VALUES ($1, $2, $3, 'pending', $4)
		 RETURNING `+jobColumns,
		serverID, kind, params, createdBy))
	if err != nil {
		return Job{}, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

// UpdateJobState moves a job forward. It only changes a job of that server, and
// a job that has finished never changes again, so a late or repeated event from
// the agent cannot rewrite the outcome. It returns whether the row changed.
func (s *Store) UpdateJobState(ctx context.Context, id, serverID, state, message, output string) (bool, error) {
	switch state {
	case JobRunning, JobSucceeded, JobFailed:
	default:
		return false, fmt.Errorf("invalid job state %q", state)
	}

	// PostgreSQL refuses text that is not valid UTF-8; the cut can split a
	// character.
	if len(output) > MaxJobOutput {
		output = output[len(output)-MaxJobOutput:]
	}
	output = strings.ToValidUTF8(output, "�")
	message = strings.ToValidUTF8(message, "�")

	tag, err := s.pool.Exec(ctx,
		`UPDATE job
		 SET state = $3,
		     message = $4,
		     output = $5,
		     updated_at = now(),
		     finished_at = CASE WHEN $3 IN ('succeeded', 'failed') THEN now() ELSE NULL END
		 WHERE id = $1 AND server_id = $2
		   AND state IN ('pending', 'running')
		   AND NOT (state = 'running' AND $3 = 'running')`,
		id, serverID, state, message, output)
	if err != nil {
		return false, fmt.Errorf("update job state: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// GetJob returns one job.
func (s *Store) GetJob(ctx context.Context, id string) (Job, error) {
	job, err := scanJob(s.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM job WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

// FailStaleJob marks an unfinished job as failed once it is older than maxAge,
// for example because the agent disconnected and never reported. It returns
// whether the job changed.
func (s *Store) FailStaleJob(ctx context.Context, id string, maxAge time.Duration) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE job
		 SET state = 'failed', message = 'timed out', updated_at = now(), finished_at = now()
		 WHERE id = $1 AND state IN ('pending', 'running')
		   AND created_at < now() - make_interval(secs => $2)`,
		id, maxAge.Seconds())
	if err != nil {
		return false, fmt.Errorf("fail stale job: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
