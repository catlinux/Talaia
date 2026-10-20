package jobs

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		_, _ = w.Write(frame(1, "2026-10-20T10:00:00Z started\n"))
	}))
	defer server.Close()

	text, err := NewDockerActionsWithClient(server.URL, server.Client()).Logs(context.Background(), "web", 5000)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}

	if text != "2026-10-20T10:00:00Z started\n" {
		t.Errorf("text = %q", text)
	}
	if want := "/containers/web/logs?stdout=1&stderr=1&timestamps=1&tail=1000"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDockerLogsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such container: ghost"}`))
	}))
	defer server.Close()
	docker := NewDockerActionsWithClient(server.URL, server.Client())

	if _, err := docker.Logs(context.Background(), "ghost", 10); err == nil || !strings.Contains(err.Error(), "No such container") {
		t.Errorf("error = %v", err)
	}
	if _, err := docker.Logs(context.Background(), "../etc", 10); err == nil {
		t.Error("expected an error for a bad reference")
	}
}
