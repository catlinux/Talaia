package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// newTestServerID enrolls a throw-away server and returns its id.
func newTestServerID(t *testing.T, s *Store) string {
	t.Helper()

	ctx := context.Background()
	name := testName(t, s)
	token, err := s.CreateEnrollmentToken(ctx, name, time.Minute)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	id, _, err := s.EnrollServer(ctx, token, testSigner("0a0b0c"))
	if err != nil {
		t.Fatalf("enroll server: %v", err)
	}
	return id
}

func TestJobLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)

	job, err := s.CreateJob(ctx, serverID, "container_action", map[string]any{"action": "restart", "container": "web"}, "admin")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.State != JobPending || job.Params["container"] != "web" || job.CreatedBy != "admin" || job.Done() {
		t.Fatalf("new job = %+v", job)
	}

	if changed, err := s.UpdateJobState(ctx, job.ID, serverID, JobRunning, "", ""); err != nil || !changed {
		t.Fatalf("to running = %v, %v", changed, err)
	}
	// Running twice is not a change.
	if changed, _ := s.UpdateJobState(ctx, job.ID, serverID, JobRunning, "", ""); changed {
		t.Error("a second running event changed the job")
	}
	if changed, err := s.UpdateJobState(ctx, job.ID, serverID, JobSucceeded, "done", "log line\n"); err != nil || !changed {
		t.Fatalf("to succeeded = %v, %v", changed, err)
	}

	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.State != JobSucceeded || got.Message != "done" || got.Output != "log line\n" || !got.Done() || got.FinishedAt.IsZero() {
		t.Errorf("finished job = %+v", got)
	}

	// A finished job never changes again.
	if changed, _ := s.UpdateJobState(ctx, job.ID, serverID, JobFailed, "late", ""); changed {
		t.Error("a finished job was rewritten")
	}
}

func TestJobStateOnlyChangesForItsOwnServer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)
	otherID := newTestServerID(t, s)

	job, err := s.CreateJob(ctx, serverID, "container_action", nil, "admin")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if changed, _ := s.UpdateJobState(ctx, job.ID, otherID, JobSucceeded, "forged", ""); changed {
		t.Error("another server finished the job")
	}
	got, _ := s.GetJob(ctx, job.ID)
	if got.State != JobPending {
		t.Errorf("state = %q, want pending", got.State)
	}
}

func TestJobValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)

	if _, err := s.CreateJob(ctx, serverID, "", nil, "admin"); err == nil {
		t.Error("expected an error without a kind")
	}
	if _, err := s.UpdateJobState(ctx, "x", serverID, "pending", "", ""); err == nil {
		t.Error("expected an error for an invalid state")
	}
	if _, err := s.GetJob(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetJob of an unknown id = %v, want ErrNotFound", err)
	}
}

func TestUpdateJobStateStoresBrokenUTF8(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)

	job, err := s.CreateJob(ctx, serverID, "stack_action", nil, "admin")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	// Half a character at both ends, as a cut at a byte limit leaves it.
	output := "\x9c\x94 pulled\n" + strings.Repeat("x", MaxJobOutput) + "\xe2\x9c"
	if changed, err := s.UpdateJobState(ctx, job.ID, serverID, JobSucceeded, "ok \xe2", output); err != nil || !changed {
		t.Fatalf("UpdateJobState = %v, %v", changed, err)
	}
	got, _ := s.GetJob(ctx, job.ID)
	if got.State != JobSucceeded || !utf8.ValidString(got.Output) || !utf8.ValidString(got.Message) {
		t.Errorf("job = %s, output valid %v", got.State, utf8.ValidString(got.Output))
	}
}

func TestFailStaleJob(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)

	job, err := s.CreateJob(ctx, serverID, "container_action", nil, "admin")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if changed, _ := s.FailStaleJob(ctx, job.ID, time.Hour); changed {
		t.Error("a fresh job was failed")
	}
	time.Sleep(20 * time.Millisecond)
	if changed, err := s.FailStaleJob(ctx, job.ID, 10*time.Millisecond); err != nil || !changed {
		t.Fatalf("FailStaleJob = %v, %v", changed, err)
	}
	got, _ := s.GetJob(ctx, job.ID)
	if got.State != JobFailed || got.Message != "timed out" {
		t.Errorf("stale job = %+v", got)
	}
}

func TestPurgeFinishedJobs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)

	// create makes a job in the given state and backdates it. A zero finishedAgo
	// leaves finished_at NULL, as an unfinished job has it.
	create := func(kind, state string, finishedAgo time.Duration) string {
		t.Helper()
		job, err := s.CreateJob(ctx, serverID, kind, nil, "admin")
		if err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		if state != JobPending {
			if _, err := s.UpdateJobState(ctx, job.ID, serverID, JobRunning, "", ""); err != nil {
				t.Fatalf("to running: %v", err)
			}
		}
		if state == JobSucceeded || state == JobFailed {
			if _, err := s.UpdateJobState(ctx, job.ID, serverID, state, "", ""); err != nil {
				t.Fatalf("to %s: %v", state, err)
			}
		}
		var finished any
		if finishedAgo > 0 {
			finished = time.Now().Add(-finishedAgo)
		}
		if _, err := s.pool.Exec(ctx,
			`UPDATE job SET created_at = $2, finished_at = $3 WHERE id = $1`,
			job.ID, time.Now().Add(-time.Hour), finished); err != nil {
			t.Fatalf("backdate the job: %v", err)
		}
		return job.ID
	}

	oldSucceeded := create("container_logs", JobSucceeded, 20*time.Minute)
	oldFailed := create("container_logs", JobFailed, 20*time.Minute)
	fresh := create("container_logs", JobSucceeded, 5*time.Minute)
	otherKind := create("container_action", JobSucceeded, 20*time.Minute)
	running := create("container_logs", JobRunning, 0)
	pending := create("container_logs", JobPending, 0)

	deleted, err := s.PurgeFinishedJobs(ctx, "container_logs", 15*time.Minute)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	for id, wantGone := range map[string]bool{
		oldSucceeded: true,
		oldFailed:    true,
		fresh:        false,
		otherKind:    false,
		running:      false,
		pending:      false,
	} {
		_, err := s.GetJob(ctx, id)
		if wantGone && !errors.Is(err, ErrNotFound) {
			t.Errorf("job %s was not purged: %v", id, err)
		}
		if !wantGone && err != nil {
			t.Errorf("job %s was purged: %v", id, err)
		}
	}

	if _, err := s.PurgeFinishedJobs(ctx, " ", time.Minute); err == nil {
		t.Error("an empty kind was accepted")
	}
	if _, err := s.PurgeFinishedJobs(ctx, "container_logs", -time.Minute); err == nil {
		t.Error("a negative retention was accepted")
	}
}
