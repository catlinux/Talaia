package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestJobsWaitSuccessAndJSON(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/jobs/job-1" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "job-1", "state": "succeeded", "output": "all done\n",
		})
	})
	h.setCredentials()
	if code := h.run("jobs", "wait", "job-1"); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, h.stderr.String())
	}
	if h.stdout.String() != "all done\n" {
		t.Fatalf("output = %q", h.stdout.String())
	}

	h.resetOutput()
	if code := h.run("jobs", "--json", "wait", "job-1"); code != 0 {
		t.Fatalf("json exit = %d", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(h.stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("json = %q: %v", h.stdout.String(), err)
	}
	if decoded["state"] != "succeeded" {
		t.Fatalf("json = %#v", decoded)
	}
}

func TestJobsWaitPollsUntilFinished(t *testing.T) {
	var calls int32
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		state := "running"
		if call >= 3 {
			state = "succeeded"
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": "job-1", "state": state, "output": "ok"})
	})
	h.setCredentials()
	if code := h.run("jobs", "wait", "job-1"); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, h.stderr.String())
	}
	if got := atomic.LoadInt32(&calls); got < 3 {
		t.Fatalf("calls = %d, want at least 3", got)
	}
	if h.stdout.String() != "ok\n" {
		t.Fatalf("output = %q", h.stdout.String())
	}
}

func TestJobsWaitFailureAndTimeout(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "job-1", "state": "failed", "message": "compose failed", "output": "line one\n",
		})
	})
	h.setCredentials()
	if code := h.run("jobs", "wait", "job-1"); code != 1 {
		t.Fatalf("failure exit = %d, want 1", code)
	}
	if !strings.Contains(h.stdout.String(), "line one") {
		t.Fatalf("stdout = %q", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), "compose failed") {
		t.Fatalf("stderr = %q", h.stderr.String())
	}

	h.resetOutput()
	pending := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": "job-2", "state": "pending"})
	})
	pending.setCredentials()
	start := time.Now()
	if code := pending.run("jobs", "wait", "job-2", "--timeout", "20ms"); code != 1 {
		t.Fatalf("timeout exit = %d, want 1", code)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout took too long")
	}
	if !strings.Contains(pending.stderr.String(), "Timed out") {
		t.Fatalf("timeout stderr = %q", pending.stderr.String())
	}
}

func TestActionWaitRunsTheJob(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/servers":
			writeJSON(w, http.StatusOK, map[string]any{"servers": []map[string]any{
				{"id": "srv-1", "name": "srv-1", "connected": true},
			}})
		case "/api/v1/servers/srv-1/containers/web/actions":
			writeJSON(w, http.StatusAccepted, map[string]any{"job_id": "job-1"})
		case "/api/v1/jobs/job-1":
			writeJSON(w, http.StatusOK, map[string]any{"id": "job-1", "state": "succeeded", "output": "started\n"})
		default:
			http.NotFound(w, r)
		}
	})
	h.setCredentials()
	if code := h.run("containers", "start", "srv-1", "web", "--wait"); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "Job job-1 queued") || !strings.Contains(h.stdout.String(), "started") {
		t.Fatalf("output = %q", h.stdout.String())
	}
}
