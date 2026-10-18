package store

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestStoreMigrate(t *testing.T) {
	url := os.Getenv("TALAIA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TALAIA_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})

	if err := s.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	var exists bool
	err = s.pool.QueryRow(ctx, `SELECT to_regclass('public.organization') IS NOT NULL`).Scan(&exists)
	if err != nil {
		t.Fatalf("query organization table: %v", err)
	}
	if !exists {
		t.Fatal("organization table does not exist after migrations")
	}
}
