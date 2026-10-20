package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTerminalSessionLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)

	session, err := s.CreateTerminalSession(ctx, serverID, "web", "sh", "admin")
	if err != nil {
		t.Fatalf("CreateTerminalSession: %v", err)
	}
	if session.ID == "" || !session.Open() || session.Container != "web" || session.Privileged {
		t.Fatalf("new session = %+v", session)
	}

	if err := s.SetTerminalPrivileged(ctx, session.ID, true); err != nil {
		t.Fatalf("SetTerminalPrivileged: %v", err)
	}
	for seq, data := range []string{"first", "second"} {
		if err := s.AppendTerminalChunk(ctx, session.ID, seq, []byte("sealed-"+data), len(data)); err != nil {
			t.Fatalf("AppendTerminalChunk %d: %v", seq, err)
		}
	}
	// A repeated position is refused, so a chunk cannot be replaced.
	if err := s.AppendTerminalChunk(ctx, session.ID, 1, []byte("again"), 5); err == nil {
		t.Error("a chunk position was written twice")
	}

	if err := s.CloseTerminalSession(ctx, session.ID, "exited"); err != nil {
		t.Fatalf("CloseTerminalSession: %v", err)
	}
	if err := s.CloseTerminalSession(ctx, session.ID, "idle"); err != nil {
		t.Fatalf("second CloseTerminalSession: %v", err)
	}

	got, err := s.GetTerminalSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetTerminalSession: %v", err)
	}
	if got.Open() || got.CloseReason != "exited" || !got.Privileged || got.RecordedBytes != int64(len("first")+len("second")) {
		t.Errorf("session = %+v", got)
	}

	chunks, err := s.TerminalChunks(ctx, session.ID)
	if err != nil || len(chunks) != 2 || string(chunks[0].Sealed) != "sealed-first" || chunks[1].Seq != 1 {
		t.Errorf("chunks = %+v, %v", chunks, err)
	}

	if _, err := s.GetTerminalSession(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session = %v, want ErrNotFound", err)
	}
	list, err := s.ListTerminalSessions(ctx, 100)
	if err != nil || len(list) == 0 {
		t.Errorf("ListTerminalSessions = %v, %v", list, err)
	}
}

func TestTerminalOrphansAndPurge(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)

	open, _ := s.CreateTerminalSession(ctx, serverID, "web", "sh", "admin")
	done, _ := s.CreateTerminalSession(ctx, serverID, "db", "bash", "admin")
	_ = s.AppendTerminalChunk(ctx, done.ID, 0, []byte("x"), 1)
	_ = s.CloseTerminalSession(ctx, done.ID, "closed")

	// Nothing is purged while the cutoff is in the past, and a session that is
	// still open is never purged.
	if n, err := s.PurgeTerminalSessions(ctx, time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Errorf("early purge = %d, %v", n, err)
	}
	if n, err := s.PurgeTerminalSessions(ctx, time.Now().Add(time.Hour)); err != nil || n < 1 {
		t.Errorf("purge = %d, %v", n, err)
	}
	if _, err := s.GetTerminalSession(ctx, done.ID); !errors.Is(err, ErrNotFound) {
		t.Error("a purged session is still there")
	}
	if chunks, _ := s.TerminalChunks(ctx, done.ID); len(chunks) != 0 {
		t.Error("the recording of a purged session is still there")
	}
	if _, err := s.GetTerminalSession(ctx, open.ID); err != nil {
		t.Errorf("an open session was purged: %v", err)
	}

	if n, err := s.CloseOrphanTerminalSessions(ctx, "core_restart"); err != nil || n < 1 {
		t.Errorf("orphans = %d, %v", n, err)
	}
	if got, _ := s.GetTerminalSession(ctx, open.ID); got.Open() || got.CloseReason != "core_restart" {
		t.Errorf("orphan = %+v", got)
	}
}
