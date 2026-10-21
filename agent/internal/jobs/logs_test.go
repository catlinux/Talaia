package jobs

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// frame builds one Docker stream frame.
func frame(stream byte, text string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(text)))
	return append(header, text...)
}

func TestDemultiplexJoinsStdoutAndStderrFrames(t *testing.T) {
	var raw bytes.Buffer
	raw.Write(frame(1, "out line\n"))
	raw.Write(frame(2, "err line\n"))

	if got, want := demultiplex(raw.Bytes()), "out line\nerr line\n"; got != want {
		t.Errorf("demultiplex = %q, want %q", got, want)
	}
}

func TestDemultiplexKeepsPlainTextOfATTYContainer(t *testing.T) {
	if got := demultiplex([]byte("plain text\n")); got != "plain text\n" {
		t.Errorf("demultiplex = %q", got)
	}
}

func TestDemultiplexToleratesATruncatedFrame(t *testing.T) {
	raw := frame(1, "complete\n")
	raw = append(raw, frame(1, "cut short")[:12]...)

	got := demultiplex(raw)

	if !strings.HasPrefix(got, "complete\n") {
		t.Errorf("demultiplex = %q", got)
	}
}

func TestLastBytesKeepsTheEndAtALineBoundary(t *testing.T) {
	text := strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10) + "\n"

	got := lastBytes(text, 15)

	if got != strings.Repeat("b", 10)+"\n" {
		t.Errorf("lastBytes = %q", got)
	}
	if lastBytes("short", 100) != "short" {
		t.Error("a short text must be returned as it is")
	}
}

func TestClampTail(t *testing.T) {
	for in, want := range map[int]int{0: 200, -5: 200, 1: 1, 500: 500, 5000: 1000} {
		if got := clampTail(in); got != want {
			t.Errorf("clampTail(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestDockerLogsCallsTheEngineAndDemultiplexes(t *testing.T) {
	var (
		gotPath  string
		gotQuery map[string][]string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		_, _ = w.Write(frame(1, "2026-10-20T10:00:00Z started\n"))
	}))
	defer server.Close()

	text, err := NewDockerActionsWithClient(server.URL, server.Client()).Logs(
		context.Background(), "web", 5000, time.Time{}, false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}

	if text != "2026-10-20T10:00:00Z started\n" {
		t.Errorf("text = %q", text)
	}
	if gotPath != "/containers/web/logs" {
		t.Errorf("path = %q", gotPath)
	}
	if got := gotQuery["tail"]; len(got) != 1 || got[0] != "1000" {
		t.Errorf("tail = %v, want 1000", got)
	}
	// An old core sends neither field: it keeps the timestamps of before.
	if got := gotQuery["timestamps"]; len(got) != 1 || got[0] != "1" {
		t.Errorf("timestamps = %v, want 1", got)
	}
	if got := gotQuery["since"]; len(got) != 0 {
		t.Errorf("since = %v, want none", got)
	}
}

func TestDockerLogsSendsSinceAndTimestamps(t *testing.T) {
	queries := make([]map[string][]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query())
		_, _ = w.Write(frame(1, "2026-10-20T10:00:01Z new line\n"))
	}))
	defer server.Close()
	docker := NewDockerActionsWithClient(server.URL, server.Client())
	since := time.Date(2026, 10, 20, 10, 0, 0, 123456789, time.UTC)

	if _, err := docker.Logs(context.Background(), "web", 100, since, true); err != nil {
		t.Fatalf("with timestamps: %v", err)
	}
	if _, err := docker.Logs(context.Background(), "web", 100, since, false); err != nil {
		t.Fatalf("without timestamps: %v", err)
	}

	if len(queries) != 2 {
		t.Fatalf("requests = %d, want 2", len(queries))
	}
	for index, query := range queries {
		if got := query["since"]; len(got) != 1 || got[0] != "1792490400.123456789" {
			t.Errorf("request %d since = %v", index, got)
		}
	}
	if got := queries[0]["timestamps"]; len(got) != 1 || got[0] != "1" {
		t.Errorf("timestamps asked = %v, want 1", got)
	}
	if got := queries[1]["timestamps"]; len(got) != 0 {
		t.Errorf("timestamps not asked = %v, want none", got)
	}
}

func TestParseSince(t *testing.T) {
	if got, err := parseSince(""); err != nil || !got.IsZero() {
		t.Errorf("empty since = %v, %v", got, err)
	}
	want := time.Date(2026, 10, 20, 10, 0, 0, 123456789, time.UTC)
	if got, err := parseSince("2026-10-20T10:00:00.123456789Z"); err != nil || !got.Equal(want) {
		t.Errorf("since = %v, %v", got, err)
	}
	for _, bad := range []string{"yesterday", "2026-10-20", "2026-10-20 10:00:00Z", "2026-13-40T10:00:00Z"} {
		if _, err := parseSince(bad); err == nil {
			t.Errorf("since %q was accepted", bad)
		}
	}
}

func TestDockerLogsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such container: ghost"}`))
	}))
	defer server.Close()
	docker := NewDockerActionsWithClient(server.URL, server.Client())

	if _, err := docker.Logs(context.Background(), "ghost", 10, time.Time{}, false); err == nil || !strings.Contains(err.Error(), "No such container") {
		t.Errorf("error = %v", err)
	}
	if _, err := docker.Logs(context.Background(), "../etc", 10, time.Time{}, false); err == nil {
		t.Error("expected an error for a bad reference")
	}
}
