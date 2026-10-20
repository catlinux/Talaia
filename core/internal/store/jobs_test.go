package store

import (
	"context"
	"errors"
	"testing"
	"time"
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

	if changed, err := s.UpdateJobState(ctx, job.ID, serverID, JobRunning, ""); err != nil || !changed {
		t.Fatalf("to running = %v, %v", changed, err)
	}
	// Running twice is not a change.
	if changed, _ := s.UpdateJobState(ctx, job.ID, serverID, JobRunning, ""); changed {
		t.Error("a second running event changed the job")
	}
	if changed, err := s.UpdateJobState(ctx, job.ID, serverID, JobSucceeded, "done"); err != nil || !changed {
		t.Fatalf("to succeeded = %v, %v", changed, err)
	}

	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.State != JobSucceeded || got.Message != "done" || !got.Done() || got.FinishedAt.IsZero() {
		t.Errorf("finished job = %+v", got)
	}

	// A finished job never changes again.
	if changed, _ := s.UpdateJobState(ctx, job.ID, serverID, JobFailed, "late"); changed {
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

	if changed, _ := s.UpdateJobState(ctx, job.ID, otherID, JobSucceeded, "forged"); changed {
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
	if _, err := s.UpdateJobState(ctx, "x", serverID, "pending", ""); err == nil {
		t.Error("expected an error for an invalid state")
	}
	if _, err := s.GetJob(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetJob of an unknown id = %v, want ErrNotFound", err)
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
