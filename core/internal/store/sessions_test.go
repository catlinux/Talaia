package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// sessionTokenSeq keeps the generated token hashes unique even when the clock
// does not move between two calls.
var sessionTokenSeq atomic.Int64

// sessionTokenHash returns a token hash no other test uses.
func sessionTokenHash() []byte {
	return []byte(fmt.Sprintf("test-token-%d-%d", time.Now().UnixNano(), sessionTokenSeq.Add(1)))
}

// createTestSession inserts a session and returns its id and token hash.
func createTestSession(t *testing.T, s *Store, userID, stage string, expiresAt time.Time) (string, []byte) {
	t.Helper()

	hash := sessionTokenHash()
	id, err := s.CreateSession(context.Background(), userID, hash, stage, expiresAt, "192.0.2.10", "test-agent")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return id, hash
}

func TestCreateAndGetSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	id, hash := createTestSession(t, s, user.ID, StageMFAPending, time.Now().Add(5*time.Minute))

	session, err := s.GetSessionByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.ID != id {
		t.Errorf("id = %q, want %q", session.ID, id)
	}
	if session.UserID != user.ID {
		t.Errorf("user id = %q, want %q", session.UserID, user.ID)
	}
	if session.Stage != StageMFAPending {
		t.Errorf("stage = %q, want %q", session.Stage, StageMFAPending)
	}
	if session.IP != "192.0.2.10" || session.UserAgent != "test-agent" {
		t.Errorf("address = %q / %q", session.IP, session.UserAgent)
	}
	if session.MFAFailures != 0 {
		t.Errorf("mfa failures = %d, want 0", session.MFAFailures)
	}
	if session.CreatedAt.IsZero() || session.LastSeenAt.IsZero() {
		t.Error("the session timestamps are zero")
	}
	if !session.RevokedAt.IsZero() {
		t.Errorf("a new session must not be revoked: %s", session.RevokedAt)
	}
}

func TestGetSessionIgnoresRevokedExpiredAndUnknown(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	expiredID, expiredHash := createTestSession(t, s, user.ID, StageFull, time.Now().Add(-time.Minute))
	_ = expiredID
	revokedID, revokedHash := createTestSession(t, s, user.ID, StageFull, time.Now().Add(time.Hour))
	if err := s.RevokeSession(ctx, revokedID); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	tests := []struct {
		name string
		hash []byte
	}{
		{name: "expired", hash: expiredHash},
		{name: "revoked", hash: revokedHash},
		{name: "unknown", hash: []byte("never issued")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.GetSessionByTokenHash(ctx, tt.hash); !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestCreateSessionValidatesItsInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	if _, err := s.CreateSession(ctx, user.ID, sessionTokenHash(), "half", time.Now().Add(time.Hour), "", ""); err == nil {
		t.Error("expected an error for an unknown stage")
	}
	if _, err := s.CreateSession(ctx, user.ID, nil, StageFull, time.Now().Add(time.Hour), "", ""); err == nil {
		t.Error("expected an error for a missing token hash")
	}
	if _, err := s.CreateSession(ctx, user.ID, sessionTokenHash(), StageFull, time.Time{}, "", ""); err == nil {
		t.Error("expected an error for a missing expiry")
	}
}

func TestTouchSessionMovesLastSeenAtMostOnceAMinute(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	id, hash := createTestSession(t, s, user.ID, StageFull, time.Now().Add(time.Hour))
	before, err := s.GetSessionByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	// A session created a moment ago must not be written again.
	if err := s.TouchSession(ctx, id, time.Now()); err != nil {
		t.Fatalf("touch session: %v", err)
	}
	after, err := s.GetSessionByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !after.LastSeenAt.Equal(before.LastSeenAt) {
		t.Errorf("last_seen_at moved from %s to %s although less than a minute passed",
			before.LastSeenAt, after.LastSeenAt)
	}

	// After a minute of idleness it is updated.
	later := time.Now().Add(2 * time.Minute)
	if err := s.TouchSession(ctx, id, later); err != nil {
		t.Fatalf("touch session: %v", err)
	}
	after, err = s.GetSessionByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !after.LastSeenAt.After(before.LastSeenAt) {
		t.Errorf("last_seen_at did not move: %s then %s", before.LastSeenAt, after.LastSeenAt)
	}

	// Touching a session that no longer exists is not an error.
	if err := s.TouchSession(ctx, "00000000-0000-0000-0000-000000000000", later); err != nil {
		t.Errorf("touch of a missing session = %v, want nil", err)
	}
}

func TestUpgradeSessionRevokesTheOldToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	oldID, oldHash := createTestSession(t, s, user.ID, StageMFAPending, time.Now().Add(5*time.Minute))

	newHash := sessionTokenHash()
	newID, err := s.UpgradeSession(ctx, oldID, newHash, StageFull, time.Now().Add(12*time.Hour))
	if err != nil {
		t.Fatalf("upgrade session: %v", err)
	}
	if newID == oldID {
		t.Error("the upgrade reused the session id")
	}

	if _, err := s.GetSessionByTokenHash(ctx, oldHash); !errors.Is(err, ErrNotFound) {
		t.Errorf("the old token still works: %v", err)
	}

	session, err := s.GetSessionByTokenHash(ctx, newHash)
	if err != nil {
		t.Fatalf("get the new session: %v", err)
	}
	if session.ID != newID || session.Stage != StageFull {
		t.Errorf("new session = %+v, want id %q and stage %q", session, newID, StageFull)
	}
	if session.UserID != user.ID || session.IP != "192.0.2.10" || session.UserAgent != "test-agent" {
		t.Errorf("the new session did not inherit the context: %+v", session)
	}

	// The old session is gone: upgrading it again fails.
	if _, err := s.UpgradeSession(ctx, oldID, sessionTokenHash(), StageFull, time.Now().Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("upgrading a revoked session = %v, want ErrNotFound", err)
	}
}

func TestRevokeSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	id, hash := createTestSession(t, s, user.ID, StageFull, time.Now().Add(time.Hour))

	if err := s.RevokeSession(ctx, id); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := s.GetSessionByTokenHash(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Errorf("the revoked session is still live: %v", err)
	}
	if err := s.RevokeSession(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking twice = %v, want ErrNotFound", err)
	}
}

func TestRevokeUserSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	ids := make([]string, 3)
	hashes := make([][]byte, 3)
	for i := range ids {
		ids[i], hashes[i] = createTestSession(t, s, user.ID, StageFull, time.Now().Add(time.Hour))
	}

	revoked, err := s.RevokeUserSessions(ctx, user.ID, ids[0])
	if err != nil {
		t.Fatalf("revoke user sessions: %v", err)
	}
	if revoked != 2 {
		t.Errorf("revoked = %d, want 2", revoked)
	}
	if _, err := s.GetSessionByTokenHash(ctx, hashes[0]); err != nil {
		t.Errorf("the excepted session was revoked: %v", err)
	}
	for _, hash := range hashes[1:] {
		if _, err := s.GetSessionByTokenHash(ctx, hash); !errors.Is(err, ErrNotFound) {
			t.Errorf("a session was not revoked: %v", err)
		}
	}

	revoked, err = s.RevokeUserSessions(ctx, user.ID, "")
	if err != nil {
		t.Fatalf("revoke all sessions: %v", err)
	}
	if revoked != 1 {
		t.Errorf("revoked = %d, want 1", revoked)
	}
}

func TestIncrementMFAFailures(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	id, _ := createTestSession(t, s, user.ID, StageMFAPending, time.Now().Add(5*time.Minute))

	for want := 1; want <= 3; want++ {
		got, err := s.IncrementMFAFailures(ctx, id)
		if err != nil {
			t.Fatalf("increment: %v", err)
		}
		if got != want {
			t.Errorf("failures = %d, want %d", got, want)
		}
	}

	missing := "00000000-0000-0000-0000-000000000000"
	if _, err := s.IncrementMFAFailures(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("increment of a missing session = %v, want ErrNotFound", err)
	}
}

func TestDeletingAUserDeletesItsSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	_, hash := createTestSession(t, s, user.ID, StageFull, time.Now().Add(time.Hour))

	if _, err := s.pool.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := s.GetSessionByTokenHash(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Errorf("the session of a deleted user is still live: %v", err)
	}
}
