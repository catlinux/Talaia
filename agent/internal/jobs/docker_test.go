package jobs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func TestDockerActionsCallTheEngine(t *testing.T) {
	tests := []struct {
		action agentv1.ContainerActionType
		method string
		path   string
	}{
		{agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_START, http.MethodPost, "/containers/web/start"},
		{agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_STOP, http.MethodPost, "/containers/web/stop?t=10"},
		{agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_RESTART, http.MethodPost, "/containers/web/restart?t=10"},
		{agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_REMOVE, http.MethodDelete, "/containers/web?v=0&force=0"},
	}

	for _, tt := range tests {
		t.Run(tt.action.String(), func(t *testing.T) {
			var gotMethod, gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.RequestURI()
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			err := NewDockerActionsWithClient(server.URL, server.Client()).Do(context.Background(), "web", tt.action)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			if gotMethod != tt.method || gotPath != tt.path {
				t.Errorf("request = %s %s, want %s %s", gotMethod, gotPath, tt.method, tt.path)
			}
		})
	}
}

func TestDockerActionsAnswers(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"already started is fine", http.StatusNotModified, "", ""},
		{"engine error message", http.StatusConflict, `{"message":"cannot remove a running container"}`, "cannot remove a running container"},
		{"no such container", http.StatusNotFound, `{"message":"No such container: web"}`, "No such container"},
		{"error without a message", http.StatusInternalServerError, "oops", "500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			err := NewDockerActionsWithClient(server.URL, server.Client()).Do(context.Background(), "web", agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_START)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Do: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestDockerActionsRefuseBadInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the engine must not be called")
	}))
	defer server.Close()
	docker := NewDockerActionsWithClient(server.URL, server.Client())

	if err := docker.Do(context.Background(), "../etc", agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_START); err == nil {
		t.Error("expected an error for a bad container reference")
	}
	if err := docker.Do(context.Background(), "web", agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_UNSPECIFIED); err == nil {
		t.Error("expected an error for an unset action")
	}
}
