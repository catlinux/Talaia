package inventory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const dockerContainersJSON = `[
  {
    "Id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "Names": ["/shop-web-1"],
    "Image": "nginx:1.27",
    "State": "running",
    "Status": "Up 2 hours",
    "Labels": {
      "com.docker.compose.project": "shop",
      "com.docker.compose.service": "web",
      "com.docker.compose.project.working_dir": "/opt/talaia/stacks/shop",
      "com.docker.compose.project.config_files": "/opt/talaia/stacks/shop/compose.yaml,/opt/talaia/stacks/shop/compose.override.yaml"
    }
  },
  {
    "Id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "Names": ["/shop-db-1"],
    "Image": "postgres:16",
    "State": "running",
    "Status": "Up 2 hours",
    "Labels": {
      "com.docker.compose.project": "shop",
      "com.docker.compose.service": "db",
      "com.docker.compose.project.working_dir": "/opt/talaia/stacks/shop",
      "com.docker.compose.project.config_files": "/opt/talaia/stacks/shop/compose.yaml"
    }
  },
  {
    "Id": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
    "Names": ["/loose"],
    "Image": "alpine:3.20",
    "State": "exited",
    "Status": "Exited (0) 3 hours ago",
    "Labels": {}
  }
]`

func dockerServer(t *testing.T, containers string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Version":"27.1.1"}`))
	})
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(containers))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestDockerCollectsContainersAndProjects(t *testing.T) {
	server := dockerServer(t, dockerContainersJSON)
	docker := NewDockerWithClient(server.URL, server.Client())

	result := docker.Collect(context.Background())

	if !result.Available {
		t.Fatalf("docker should be available")
	}
	if result.Version != "27.1.1" {
		t.Errorf("version = %q, want 27.1.1", result.Version)
	}
	if len(result.Containers) != 3 {
		t.Fatalf("containers = %d, want 3", len(result.Containers))
	}

	// Sorted by name: loose, shop-db-1, shop-web-1.
	loose := result.Containers[0]
	if loose.GetName() != "loose" {
		t.Errorf("first container = %q, want loose", loose.GetName())
	}
	if loose.GetId() != "cccccccccccc" {
		t.Errorf("short id = %q, want 12 characters", loose.GetId())
	}
	if loose.GetImage() != "alpine:3.20" || loose.GetState() != "exited" || loose.GetStatus() != "Exited (0) 3 hours ago" {
		t.Errorf("loose = %+v", loose)
	}
	if loose.GetComposeProject() != "" {
		t.Errorf("loose compose project = %q, want empty", loose.GetComposeProject())
	}

	web := result.Containers[2]
	if web.GetName() != "shop-web-1" || web.GetComposeProject() != "shop" || web.GetComposeService() != "web" {
		t.Errorf("shop-web-1 = %+v", web)
	}

	if len(result.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(result.Projects))
	}
	shop := result.Projects[0]
	if shop.GetName() != "shop" {
		t.Errorf("project name = %q, want shop", shop.GetName())
	}
	if shop.GetWorkingDir() != "/opt/talaia/stacks/shop" {
		t.Errorf("working dir = %q", shop.GetWorkingDir())
	}
	if len(shop.GetConfigFiles()) != 2 {
		t.Errorf("config files = %v, want 2 entries", shop.GetConfigFiles())
	}
	if shop.GetContainerCount() != 2 || shop.GetRunningCount() != 2 {
		t.Errorf("counts = %d/%d, want 2 containers and 2 running", shop.GetContainerCount(), shop.GetRunningCount())
	}
}

func TestDockerNotAvailable(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
	}{
		{
			name:    "error status",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }),
		},
		{
			name:    "invalid json",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{")) }),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)

			result := NewDockerWithClient(server.URL, server.Client()).Collect(context.Background())

			if result.Available {
				t.Fatal("docker should not be available")
			}
			if len(result.Containers) != 0 || len(result.Projects) != 0 {
				t.Fatalf("lists should be empty, got %d containers and %d projects", len(result.Containers), len(result.Projects))
			}
		})
	}
}

func TestDockerUnreachable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	result := NewDockerWithClient(url, http.DefaultClient).Collect(context.Background())

	if result.Available {
		t.Fatal("docker should not be available when the socket cannot be reached")
	}
}

func TestSplitConfigFiles(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty", value: "", want: nil},
		{name: "single", value: "/s/compose.yaml", want: []string{"/s/compose.yaml"}},
		{name: "multiple", value: "/s/compose.yaml,/s/extra.yaml", want: []string{"/s/compose.yaml", "/s/extra.yaml"}},
		{name: "spaces and empty parts", value: " /s/compose.yaml , , /s/extra.yaml ", want: []string{"/s/compose.yaml", "/s/extra.yaml"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitConfigFiles(tt.value)
			if len(got) != len(tt.want) {
				t.Fatalf("splitConfigFiles(%q) = %v, want %v", tt.value, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("splitConfigFiles(%q) = %v, want %v", tt.value, got, tt.want)
				}
			}
		})
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "long id", id: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", want: "aaaaaaaaaaaa"},
		{name: "short id", id: "abc", want: "abc"},
		{name: "empty", id: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortID(tt.id); got != tt.want {
				t.Fatalf("shortID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestContainerName(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{name: "with slash", names: []string{"/web"}, want: "web"},
		{name: "without slash", names: []string{"web"}, want: "web"},
		{name: "first of many", names: []string{"/a", "/b"}, want: "a"},
		{name: "empty", names: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containerName(tt.names); got != tt.want {
				t.Fatalf("containerName(%v) = %q, want %q", tt.names, got, tt.want)
			}
		})
	}
}
