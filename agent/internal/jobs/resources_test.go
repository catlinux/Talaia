package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// fakeEngine answers the Docker Engine endpoints the resource code uses.
func fakeEngine(t *testing.T) (*DockerActions, *[]string) {
	t.Helper()

	var (
		mu       sync.Mutex
		requests []string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{
			"ImageID": "sha256:aaaaaaaaaaaa1111",
			"NetworkSettings": {"Networks": {"app_default": {"NetworkID": "aa1111111111"}}},
			"Mounts": [{"Type": "volume", "Name": "app_data"}, {"Type": "bind", "Name": ""}]
		}]`))
	})
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"Id": "sha256:aaaaaaaaaaaa1111", "RepoTags": ["nginx:1.27"], "Created": 1760000000, "Size": 100},
			{"Id": "sha256:bbbbbbbbbbbb2222", "RepoTags": null, "Created": 1750000000, "Size": 300}
		]`))
	})
	mux.HandleFunc("/volumes", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Volumes": [
			{"Name": "app_data", "Driver": "local", "CreatedAt": "2026-01-01T00:00:00Z"},
			{"Name": "old_cache", "Driver": "local", "CreatedAt": "2025-01-01T00:00:00Z"}
		]}`))
	})
	mux.HandleFunc("/networks", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"Id": "aa1111111111", "Name": "app_default", "Driver": "bridge", "Created": "2026-01-01T00:00:00Z"},
			{"Id": "aa2222222222", "Name": "orphan", "Driver": "bridge", "Created": "2026-02-01T00:00:00Z"},
			{"Id": "aa3333333333", "Name": "bridge", "Driver": "bridge", "Created": "2026-01-01T00:00:00Z"}
		]`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "in-use"):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"resource is in use"}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return NewDockerActionsWithClient(server.URL, server.Client()), &requests
}

type listing struct {
	Kind  string         `json:"kind"`
	Items []resourceItem `json:"items"`
}

func list(t *testing.T, docker *DockerActions, kind agentv1.ResourceKind) listing {
	t.Helper()
	text, err := docker.ListResources(context.Background(), kind)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var out listing
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("the listing is not JSON: %v", err)
	}
	return out
}

func TestListImagesMarksTheOnesInUseAndPutsFreeOnesFirst(t *testing.T) {
	docker, _ := fakeEngine(t)

	got := list(t, docker, agentv1.ResourceKind_RESOURCE_KIND_IMAGE)

	if got.Kind != "image" || len(got.Items) != 2 {
		t.Fatalf("listing = %+v", got)
	}
	if got.Items[0].ID != "sha256:bbbbbbbbbbbb2222" || got.Items[0].InUse {
		t.Errorf("the unused image should come first: %+v", got.Items[0])
	}
	if got.Items[1].ID != "sha256:aaaaaaaaaaaa1111" || !got.Items[1].InUse || got.Items[1].Tags[0] != "nginx:1.27" {
		t.Errorf("the used image: %+v", got.Items[1])
	}
}

func TestListVolumesMarksTheOnesAContainerMounts(t *testing.T) {
	docker, _ := fakeEngine(t)

	got := list(t, docker, agentv1.ResourceKind_RESOURCE_KIND_VOLUME)

	byName := map[string]bool{}
	for _, item := range got.Items {
		byName[item.Name] = item.InUse
	}
	if !byName["app_data"] || byName["old_cache"] {
		t.Errorf("in use = %v", byName)
	}
	if got.Items[0].Name != "old_cache" {
		t.Errorf("the unused volume should come first: %+v", got.Items)
	}
}

func TestListNetworksMarksSystemAndUsedOnes(t *testing.T) {
	docker, _ := fakeEngine(t)

	got := list(t, docker, agentv1.ResourceKind_RESOURCE_KIND_NETWORK)

	state := map[string]resourceItem{}
	for _, item := range got.Items {
		state[item.Name] = item
	}
	if !state["app_default"].InUse || state["orphan"].InUse || !state["bridge"].System || state["orphan"].System {
		t.Errorf("networks = %+v", state)
	}
}

func TestListRefusesAnUnsetKind(t *testing.T) {
	docker, _ := fakeEngine(t)

	if _, err := docker.ListResources(context.Background(), agentv1.ResourceKind_RESOURCE_KIND_UNSPECIFIED); err == nil {
		t.Error("expected an error")
	}
}

func TestRemoveResourcesCallsTheEngineWithoutForce(t *testing.T) {
	docker, requests := fakeEngine(t)
	items := []*agentv1.ResourceRef{
		{Kind: agentv1.ResourceKind_RESOURCE_KIND_IMAGE, Id: "sha256:bbbbbbbbbbbb2222"},
		{Kind: agentv1.ResourceKind_RESOURCE_KIND_VOLUME, Id: "old_cache"},
		{Kind: agentv1.ResourceKind_RESOURCE_KIND_NETWORK, Id: "aa2222222222"},
	}

	text, err := docker.RemoveResources(context.Background(), items)
	if err != nil {
		t.Fatalf("RemoveResources: %v", err)
	}

	want := []string{
		"DELETE /images/sha256:bbbbbbbbbbbb2222?force=0&noprune=0",
		"DELETE /volumes/old_cache?force=0",
		"DELETE /networks/aa2222222222",
	}
	if strings.Join(*requests, "|") != strings.Join(want, "|") {
		t.Errorf("requests = %v, want %v", *requests, want)
	}
	for _, request := range *requests {
		if strings.Contains(request, "force=1") {
			t.Errorf("a removal was forced: %s", request)
		}
	}

	var out struct{ Results []removalResult }
	if err := json.Unmarshal([]byte(strings.ReplaceAll(text, `"results"`, `"Results"`)), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(out.Results) != 3 || !out.Results[0].OK || out.Results[1].Kind != "volume" {
		t.Errorf("results = %+v", out.Results)
	}
}

func TestRemoveResourcesReportsTheOnesDockerRefuses(t *testing.T) {
	docker, _ := fakeEngine(t)
	items := []*agentv1.ResourceRef{
		{Kind: agentv1.ResourceKind_RESOURCE_KIND_VOLUME, Id: "in-use-volume"},
		{Kind: agentv1.ResourceKind_RESOURCE_KIND_VOLUME, Id: "free_volume"},
	}

	text, err := docker.RemoveResources(context.Background(), items)
	if err != nil {
		t.Fatalf("a refusal of Docker must not fail the whole command: %v", err)
	}

	var out struct{ Results []removalResult }
	if err := json.Unmarshal([]byte(strings.ReplaceAll(text, `"results"`, `"Results"`)), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if out.Results[0].OK || !strings.Contains(out.Results[0].Message, "in use") || !out.Results[1].OK {
		t.Errorf("results = %+v", out.Results)
	}
}

func TestRemoveResourcesRefusesBadInput(t *testing.T) {
	docker, requests := fakeEngine(t)
	bad := [][]*agentv1.ResourceRef{
		nil,
		{{Kind: agentv1.ResourceKind_RESOURCE_KIND_VOLUME, Id: "../etc"}},
		{{Kind: agentv1.ResourceKind_RESOURCE_KIND_IMAGE, Id: "nginx:latest"}},
		{{Kind: agentv1.ResourceKind_RESOURCE_KIND_NETWORK, Id: "bridge"}},
		{{Kind: agentv1.ResourceKind_RESOURCE_KIND_UNSPECIFIED, Id: "abcdefabcdef"}},
	}
	for _, items := range bad {
		if _, err := docker.RemoveResources(context.Background(), items); err == nil {
			t.Errorf("RemoveResources(%v) succeeded", items)
		}
	}

	many := make([]*agentv1.ResourceRef, maxRemovals+1)
	for i := range many {
		many[i] = &agentv1.ResourceRef{Kind: agentv1.ResourceKind_RESOURCE_KIND_VOLUME, Id: "v"}
	}
	if _, err := docker.RemoveResources(context.Background(), many); err == nil {
		t.Error("too many resources were accepted")
	}
	if len(*requests) != 0 {
		t.Errorf("the engine was called for invalid input: %v", *requests)
	}
}

func TestValidResourceRef(t *testing.T) {
	tests := []struct {
		kind agentv1.ResourceKind
		id   string
		want bool
	}{
		{agentv1.ResourceKind_RESOURCE_KIND_IMAGE, "sha256:" + strings.Repeat("a", 64), true},
		{agentv1.ResourceKind_RESOURCE_KIND_IMAGE, strings.Repeat("a", 12), true},
		{agentv1.ResourceKind_RESOURCE_KIND_IMAGE, "SHA256:aaaaaaaaaaaa", false},
		{agentv1.ResourceKind_RESOURCE_KIND_VOLUME, "my-volume_1.0", true},
		{agentv1.ResourceKind_RESOURCE_KIND_VOLUME, "-x", false},
		{agentv1.ResourceKind_RESOURCE_KIND_NETWORK, strings.Repeat("f", 64), true},
		{agentv1.ResourceKind_RESOURCE_KIND_NETWORK, "my_network", false},
	}
	for _, tt := range tests {
		if got := ValidResourceRef(tt.kind, tt.id); got != tt.want {
			t.Errorf("ValidResourceRef(%v, %q) = %v, want %v", tt.kind, tt.id, got, tt.want)
		}
	}
}
