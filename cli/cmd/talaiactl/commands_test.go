package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestServersListTableAndJSON(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/servers" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"servers": []map[string]any{
				{"id": "srv-1", "name": "vps-web-01", "connected": true, "live": map[string]any{"agent_version": "0.2.8"}},
				{"id": "srv-2", "name": "vps-db-01", "connected": false},
			},
		})
	})
	h.setCredentials()

	if code := h.run("servers", "list"); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, h.stderr.String())
	}
	output := h.stdout.String()
	for _, want := range []string{"ID", "NAME", "srv-1", "vps-web-01", "online", "offline", "0.2.8"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, missing %q", output, want)
		}
	}
	assertKeyHidden(t, output, h.stderr.String())

	h.resetOutput()
	if code := h.run("servers", "--json", "list"); code != 0 {
		t.Fatalf("json exit = %d", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(h.stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("json output = %q: %v", h.stdout.String(), err)
	}
	if decoded["servers"] == nil {
		t.Fatalf("json = %#v", decoded)
	}
}

func TestServerResolutionByIDNameAndAmbiguity(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/servers":
			writeJSON(w, http.StatusOK, map[string]any{"servers": []map[string]any{
				{"id": "srv-1", "name": "web", "connected": true},
				{"id": "srv-2", "name": "web", "connected": true},
				{"id": "srv-3", "name": "db", "connected": true},
			}})
		case "/api/v1/servers/srv-3/containers":
			writeJSON(w, http.StatusOK, map[string]any{"connected": true, "containers": []map[string]any{
				{"id": "c1", "name": "postgres", "image": "postgres:17", "state": "running", "status": "Up"},
			}})
		default:
			http.NotFound(w, r)
		}
	})
	h.setCredentials()

	if code := h.run("containers", "list", "db"); code != 0 {
		t.Fatalf("name resolution exit = %d, stderr = %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "postgres") {
		t.Fatalf("output = %q", h.stdout.String())
	}

	h.resetOutput()
	if code := h.run("containers", "list", "srv-3"); code != 0 {
		t.Fatalf("id resolution exit = %d", code)
	}

	h.resetOutput()
	if code := h.run("containers", "list", "web"); code != 1 {
		t.Fatalf("ambiguous exit = %d, want 1", code)
	}
	if !strings.Contains(h.stderr.String(), "2 servers") {
		t.Fatalf("ambiguous error = %q", h.stderr.String())
	}

	h.resetOutput()
	if code := h.run("containers", "list", "missing"); code != 1 {
		t.Fatalf("missing exit = %d, want 1", code)
	}
	if !strings.Contains(h.stderr.String(), "No server named") {
		t.Fatalf("missing error = %q", h.stderr.String())
	}
}

func TestContainersListAndAction(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/servers":
			writeJSON(w, http.StatusOK, map[string]any{"servers": []map[string]any{
				{"id": "srv-1", "name": "vps-web-01", "connected": true},
			}})
		case "/api/v1/servers/srv-1/containers":
			writeJSON(w, http.StatusOK, map[string]any{"connected": true, "containers": []map[string]any{
				{"id": "c1", "name": "web", "image": "nginx:1", "state": "running", "status": "Up 2 hours"},
			}})
		case "/api/v1/servers/srv-1/containers/web/actions":
			writeJSON(w, http.StatusAccepted, map[string]any{"job_id": "job-1"})
		default:
			http.NotFound(w, r)
		}
	})
	h.setCredentials()

	if code := h.run("containers", "list", "vps-web-01"); code != 0 {
		t.Fatalf("list exit = %d, stderr = %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "nginx:1") {
		t.Fatalf("container output = %q", h.stdout.String())
	}

	h.resetOutput()
	if code := h.run("containers", "--json", "list", "vps-web-01"); code != 0 {
		t.Fatalf("json list exit = %d", code)
	}
	var listJSON map[string]any
	if err := json.Unmarshal(h.stdout.Bytes(), &listJSON); err != nil {
		t.Fatalf("containers json = %q: %v", h.stdout.String(), err)
	}
	if listJSON["containers"] == nil {
		t.Fatalf("containers json = %#v", listJSON)
	}

	h.resetOutput()
	if code := h.run("containers", "start", "vps-web-01", "web"); code != 0 {
		t.Fatalf("action exit = %d, stderr = %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "job-1") {
		t.Fatalf("action output = %q", h.stdout.String())
	}
	assertKeyHidden(t, h.stdout.String(), h.stderr.String())
	request, ok := h.findRequest(http.MethodPost, "/api/v1/servers/srv-1/containers/web/actions")
	if !ok {
		t.Fatal("action request not found")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil || body["action"] != "start" {
		t.Fatalf("action body = %q, %v", request.Body, err)
	}

	h.resetOutput()
	if code := h.run("containers", "--json", "stop", "vps-web-01", "web"); code != 0 {
		t.Fatalf("json action exit = %d", code)
	}
	if !strings.Contains(h.stdout.String(), `"job_id"`) {
		t.Fatalf("json action = %q", h.stdout.String())
	}
}

func TestStacksListAndActions(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/servers":
			writeJSON(w, http.StatusOK, map[string]any{"servers": []map[string]any{
				{"id": "srv-1", "name": "vps-web-01", "connected": true},
			}})
		case "/api/v1/stacks":
			writeJSON(w, http.StatusOK, map[string]any{"stacks": []map[string]any{
				{"server_id": "srv-1", "server_name": "vps-web-01", "name": "web", "state": "managed"},
				{"server_id": "srv-1", "server_name": "vps-web-01", "name": "db", "state": "adopted"},
			}})
		case "/api/v1/servers/srv-1/stacks":
			writeJSON(w, http.StatusOK, map[string]any{
				"connected": true, "stacks": []string{"web"}, "adopted": []string{"db"},
			})
		case "/api/v1/servers/srv-1/stacks/web/actions":
			writeJSON(w, http.StatusAccepted, map[string]any{"job_id": "job-stack"})
		default:
			http.NotFound(w, r)
		}
	})
	h.setCredentials()

	if code := h.run("stacks", "list"); code != 0 {
		t.Fatalf("all stacks exit = %d, stderr = %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "managed") || !strings.Contains(h.stdout.String(), "adopted") {
		t.Fatalf("all stacks output = %q", h.stdout.String())
	}

	h.resetOutput()
	if code := h.run("stacks", "list", "vps-web-01"); code != 0 {
		t.Fatalf("server stacks exit = %d", code)
	}
	if !strings.Contains(h.stdout.String(), "web") || !strings.Contains(h.stdout.String(), "db") {
		t.Fatalf("server stacks output = %q", h.stdout.String())
	}

	h.resetOutput()
	if code := h.run("stacks", "down", "vps-web-01", "web"); code != 0 {
		t.Fatalf("stack action exit = %d, stderr = %s", code, h.stderr.String())
	}
	request, ok := h.findRequest(http.MethodPost, "/api/v1/servers/srv-1/stacks/web/actions")
	if !ok {
		t.Fatal("stack action request not found")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body["action"] != "down" || body["confirm"] != true {
		t.Fatalf("stack action body = %#v", body)
	}
	assertKeyHidden(t, h.stdout.String(), h.stderr.String())

	h.resetOutput()
	if code := h.run("stacks", "--json", "up", "vps-web-01", "web"); code != 0 {
		t.Fatalf("stack json action exit = %d", code)
	}
	if !strings.Contains(h.stdout.String(), `"job_id"`) {
		t.Fatalf("stack json action = %q", h.stdout.String())
	}

	h.resetOutput()
	if code := h.run("stacks", "--json", "list", "vps-web-01"); code != 0 {
		t.Fatalf("stacks json exit = %d", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(h.stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("stacks json = %q: %v", h.stdout.String(), err)
	}
}
