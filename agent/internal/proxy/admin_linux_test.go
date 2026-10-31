//go:build linux

package proxy

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestUnixAdminDialsTheUnixSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "talaia-admin-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	socket := filepath.Join(dir, "admin.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/load" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	if err := NewUnixAdmin(socket).Load(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("Load: %v", err)
	}
}
