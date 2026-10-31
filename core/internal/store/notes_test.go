package store

import (
	"context"
	"testing"
)

func TestResourceNotesLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID, err := s.CreateServer(ctx, testName(t, s))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	notes, err := s.ListNotes(ctx, serverID, "container")
	if err != nil || len(notes) != 0 {
		t.Fatalf("empty notes = %+v, %v", notes, err)
	}

	if err := s.SetNote(ctx, serverID, "container", "web", "shop website, do not touch", "alice"); err != nil {
		t.Fatalf("set note: %v", err)
	}
	notes, err = s.ListNotes(ctx, serverID, "container")
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 || notes[0].Name != "web" || notes[0].Note != "shop website, do not touch" ||
		notes[0].UpdatedBy != "alice" || notes[0].UpdatedAt.IsZero() {
		t.Fatalf("notes = %+v", notes)
	}

	if err := s.SetNote(ctx, serverID, "container", "web", "updated", "bob"); err != nil {
		t.Fatalf("update note: %v", err)
	}
	notes, err = s.ListNotes(ctx, serverID, "container")
	if err != nil || len(notes) != 1 || notes[0].Note != "updated" || notes[0].UpdatedBy != "bob" {
		t.Fatalf("updated notes = %+v, %v", notes, err)
	}

	if err := s.SetNote(ctx, serverID, "stack", "web", "stack note", "alice"); err != nil {
		t.Fatalf("set stack note: %v", err)
	}
	notes, err = s.ListNotes(ctx, serverID, "container")
	if err != nil || len(notes) != 1 {
		t.Fatalf("container notes after stack write = %+v, %v", notes, err)
	}
	notes, err = s.ListNotes(ctx, serverID, "stack")
	if err != nil || len(notes) != 1 || notes[0].Name != "web" {
		t.Fatalf("stack notes = %+v, %v", notes, err)
	}

	if err := s.DeleteNote(ctx, serverID, "container", "web"); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	if err := s.DeleteNote(ctx, serverID, "container", "web"); err != nil {
		t.Fatalf("delete missing note: %v", err)
	}
	notes, err = s.ListNotes(ctx, serverID, "container")
	if err != nil || len(notes) != 0 {
		t.Fatalf("notes after delete = %+v, %v", notes, err)
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM server WHERE id = $1`, serverID); err != nil {
		t.Fatalf("delete server: %v", err)
	}
	notes, err = s.ListNotes(ctx, serverID, "stack")
	if err != nil || len(notes) != 0 {
		t.Fatalf("notes after server delete = %+v, %v", notes, err)
	}
}
