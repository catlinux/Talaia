package checks

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

type testStore struct {
	results []store.AlertCheckResult
	private bool
}

func (s *testStore) ListEnabledAlertChecks(context.Context) ([]store.AlertCheck, error) {
	return nil, nil
}
func (s *testStore) InsertAlertCheckResult(_ context.Context, v store.AlertCheckResult) error {
	s.results = append(s.results, v)
	return nil
}
func (s *testStore) DeleteCheckResultsBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (s *testStore) GetAllowPrivateTargets(context.Context) (bool, error) { return s.private, nil }

func TestHTTPChecks(t *testing.T) {
	method := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		if strings.HasPrefix(r.URL.Path, "/r") {
			n := len(strings.TrimPrefix(r.URL.Path, "/r"))
			if n > 0 {
				http.Redirect(w, r, "/r"+strings.Repeat("x", n-1), 302)
				return
			}
		}
		if r.URL.Path == "/bad" {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	s := &testStore{private: true}
	runner := New(Options{Store: s})
	tests := []struct {
		path, cfg string
		ok        bool
		status    int
	}{{"/", `{}`, true, 200}, {"/bad", `{}`, false, 503}, {"/bad", `{"expected_codes":["503"]}`, true, 503}, {"/", `{"method":"HEAD"}`, true, 200}}
	for _, tt := range tests {
		v := runner.RunOnce(context.Background(), store.AlertCheck{ID: "a", Kind: "http", Target: srv.URL + tt.path, Config: json.RawMessage(tt.cfg)})
		if v.OK != tt.ok || v.StatusCode == nil || *v.StatusCode != tt.status {
			t.Fatalf("result=%+v", v)
		}
	}
	if method != "HEAD" {
		t.Fatalf("request method=%q", method)
	}
}

func TestHTTPErrorRedactedAndGuarded(t *testing.T) {
	s := &testStore{private: false}
	runner := New(Options{Store: s})
	v := runner.RunOnce(context.Background(), store.AlertCheck{ID: "a", Kind: "http", Target: "http://127.0.0.1:1/secret-path?token=abc", Config: json.RawMessage(`{}`)})
	if v.Error != "blocked address" {
		t.Fatalf("error=%q", v.Error)
	}
	if strings.Contains(v.Error, "secret-path") || strings.Contains(v.Error, "token") {
		t.Fatalf("unredacted error: %q", v.Error)
	}
}

func TestRefusedHTTPErrorIsRedacted(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	s := &testStore{private: true}
	runner := New(Options{Store: s})
	v := runner.RunOnce(context.Background(), store.AlertCheck{ID: "a", Kind: "http", Target: "http://" + addr + "/secret-path?token=abc", Config: json.RawMessage(`{}`)})
	if v.Error != "connection refused" {
		t.Fatalf("error=%q", v.Error)
	}
	if strings.Contains(v.Error, "secret-path") || strings.Contains(v.Error, "token") {
		t.Fatalf("unredacted error: %q", v.Error)
	}
	if len(s.results) != 1 || s.results[0].Error != v.Error {
		t.Fatalf("stored results=%+v", s.results)
	}
}
