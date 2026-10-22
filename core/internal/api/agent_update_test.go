package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/agentpkg"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
)

// fakeAgentUpdateServers serves enrolled server rows to the update endpoints.
type fakeAgentUpdateServers struct {
	servers []store.Server
	getErr  error
	listErr error
}

func (f *fakeAgentUpdateServers) GetServer(_ context.Context, id string) (store.Server, error) {
	if f.getErr != nil {
		return store.Server{}, f.getErr
	}
	for _, server := range f.servers {
		if server.ID == id {
			return server, nil
		}
	}
	return store.Server{}, store.ErrNotFound
}

func (f *fakeAgentUpdateServers) ListServers(context.Context) ([]store.Server, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.servers, nil
}

// fakeAgentUpdateJobs records the update jobs and the duplicate checks.
type fakeAgentUpdateJobs struct {
	mu        sync.Mutex
	created   []store.Job
	updates   []string
	checks    []string
	active    map[string]bool
	createErr error
	activeErr error
	next      int
}

func (f *fakeAgentUpdateJobs) CreateJob(_ context.Context, serverID, kind string, params map[string]any, createdBy string) (store.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return store.Job{}, f.createErr
	}
	f.next++
	job := store.Job{
		ID:        fmt.Sprintf("job-%d", f.next),
		ServerID:  serverID,
		Kind:      kind,
		Params:    params,
		State:     store.JobPending,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	}
	f.created = append(f.created, job)
	return job, nil
}

func (f *fakeAgentUpdateJobs) UpdateJobState(_ context.Context, id, serverID, state, message, output string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, id+":"+serverID+":"+state+":"+message)
	return true, nil
}

func (f *fakeAgentUpdateJobs) HasActiveJob(_ context.Context, serverID, kind string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, serverID+":"+kind)
	if f.activeErr != nil {
		return false, f.activeErr
	}
	return f.active[serverID], nil
}

func (f *fakeAgentUpdateJobs) createdJobs() []store.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Job(nil), f.created...)
}

// newAgentUpdateMux mounts both endpoints behind RequireFull and a session of
// the given stage.
func newAgentUpdateMux(stage string, deps AgentUpdateDeps) http.Handler {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(withSession(r.Context(), Session{
				User:  store.User{ID: "user-1", Username: "admin"},
				Stage: stage,
			})))
		})
	})
	router.Use(RequireFull)
	router.Post("/api/v1/servers/{serverID}/agent/update", NewAgentUpdateHandler(deps).ServeHTTP)
	router.Post("/api/v1/agents/update-all", NewAgentUpdateAllHandler(deps).ServeHTTP)
	return router
}

func postAgentUpdate(handler http.Handler, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeRecorder(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode the body %q: %v", recorder.Body.String(), err)
	}
}

func newAgentDistDir(t *testing.T, archs ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, arch := range archs {
		name, ok := agentpkg.PackageName(arch)
		if !ok {
			t.Fatalf("package name for %q", arch)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package for "+arch), 0o600); err != nil {
			t.Fatalf("write package %s: %v", arch, err)
		}
	}
	return dir
}

func testAgentPackageSHA(t *testing.T, dir, arch string) string {
	t.Helper()
	sha, err := agentpkg.Info(dir, arch)
	if err != nil {
		t.Fatalf("hash package %s: %v", arch, err)
	}
	return sha
}

func TestAgentUpdateQueuesTheTypedCommand(t *testing.T) {
	dir := newAgentDistDir(t, "amd64")
	server := store.Server{ID: "srv-1", Name: "vps-web-01"}
	agent := hub.AgentInfo{ServerID: "srv-1", Hostname: "vps-web-01", AgentVersion: "1.2.2", Arch: "x86_64"}
	jobs := &fakeAgentUpdateJobs{}
	dispatcher := &fakeDispatcher{}
	audit := &fakeAudit{}
	deps := AgentUpdateDeps{
		Agents:     fakeAgents{agents: []hub.AgentInfo{agent}},
		Dispatcher: dispatcher,
		Jobs:       jobs,
		Audit:      audit,
		Servers:    &fakeAgentUpdateServers{servers: []store.Server{server}},
		Packages:   AgentPackages{Version: "1.2.3", DistDir: dir},
	}
	sha := testAgentPackageSHA(t, dir, "amd64")

	recorder := postAgentUpdate(newAgentUpdateMux(store.StageFull, deps),
		"/api/v1/servers/srv-1/agent/update", `{"confirm":true}`)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), dir) {
		t.Errorf("the response exposes the package directory: %s", recorder.Body.String())
	}

	var created jobCreatedResponse
	decodeRecorder(t, recorder, &created)
	createdJobs := jobs.createdJobs()
	if len(createdJobs) != 1 {
		t.Fatalf("jobs = %v, want one", createdJobs)
	}
	job := createdJobs[0]
	if job.Kind != "agent_update" || job.CreatedBy != "admin" || job.Params["version"] != "1.2.3" ||
		job.Params["sha256"] != sha || job.Params["from_version"] != "1.2.2" {
		t.Errorf("job = %+v", job)
	}

	sent := dispatcher.sent()
	if len(sent) != 1 || dispatcher.serverID != "srv-1" {
		t.Fatalf("dispatched %v for %q, want one command for srv-1", sent, dispatcher.serverID)
	}
	command := sent[0]
	if command.GetJobId() != created.JobID ||
		command.GetAgentUpdate().GetVersion() != "1.2.3" ||
		command.GetAgentUpdate().GetSha256() != sha {
		t.Errorf("command = %v", command)
	}

	if len(audit.events) != 1 {
		t.Fatalf("audit events = %v, want one", audit.events)
	}
	event := audit.events[0]
	if event.Action != "agent_update" || event.Actor != "admin" || event.Result != "requested" ||
		event.Details["server_name"] != "vps-web-01" ||
		event.Details["from_version"] != "1.2.2" ||
		event.Details["to_version"] != "1.2.3" ||
		event.Details["sha256"] != sha {
		t.Errorf("audit = %+v", event)
	}
}

func TestAgentUpdateValidationAndConflicts(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		server     store.Server
		agents     []hub.AgentInfo
		archs      []string
		version    string
		active     bool
		wantStatus int
		wantCode   string
	}{
		{
			name: "confirmation missing", body: `{}`, server: store.Server{ID: "srv-1", Name: "vps-1"},
			agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"}},
			archs:  []string{"amd64"}, version: "1.2.3",
			wantStatus: http.StatusBadRequest, wantCode: "confirmation_required",
		},
		{
			name: "unknown server", body: `{"confirm":true}`, server: store.Server{ID: "srv-2", Name: "other"},
			agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"}},
			archs:  []string{"amd64"}, version: "1.2.3",
			wantStatus: http.StatusNotFound, wantCode: "not_found",
		},
		{
			name: "removed server", body: `{"confirm":true}`,
			server: store.Server{ID: "srv-1", Name: "vps-1", RemovedAt: time.Now()},
			agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"}},
			archs:  []string{"amd64"}, version: "1.2.3",
			wantStatus: http.StatusNotFound, wantCode: "not_found",
		},
		{
			name: "offline agent", body: `{"confirm":true}`, server: store.Server{ID: "srv-1", Name: "vps-1"},
			archs: []string{"amd64"}, version: "1.2.3",
			wantStatus: http.StatusConflict, wantCode: "agent_offline",
		},
		{
			name: "missing package", body: `{"confirm":true}`, server: store.Server{ID: "srv-1", Name: "vps-1"},
			agents:     []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"}},
			version:    "1.2.3",
			wantStatus: http.StatusConflict, wantCode: "no_update",
		},
		{
			name: "same version", body: `{"confirm":true}`, server: store.Server{ID: "srv-1", Name: "vps-1"},
			agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.3", Arch: "amd64"}},
			archs:  []string{"amd64"}, version: "1.2.3",
			wantStatus: http.StatusConflict, wantCode: "no_update",
		},
		{
			name: "newer agent", body: `{"confirm":true}`, server: store.Server{ID: "srv-1", Name: "vps-1"},
			agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.3.0", Arch: "amd64"}},
			archs:  []string{"amd64"}, version: "1.2.3",
			wantStatus: http.StatusConflict, wantCode: "no_update",
		},
		{
			name: "unknown arch", body: `{"confirm":true}`, server: store.Server{ID: "srv-1", Name: "vps-1"},
			agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "mips"}},
			archs:  []string{"amd64"}, version: "1.2.3",
			wantStatus: http.StatusConflict, wantCode: "no_update",
		},
		{
			name: "dev core version", body: `{"confirm":true}`, server: store.Server{ID: "srv-1", Name: "vps-1"},
			agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"}},
			archs:  []string{"amd64"}, version: "dev",
			wantStatus: http.StatusConflict, wantCode: "no_update",
		},
		{
			name: "update in progress", body: `{"confirm":true}`, server: store.Server{ID: "srv-1", Name: "vps-1"},
			agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"}},
			archs:  []string{"amd64"}, version: "1.2.3", active: true,
			wantStatus: http.StatusConflict, wantCode: "update_in_progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newAgentDistDir(t, tt.archs...)
			jobs := &fakeAgentUpdateJobs{active: map[string]bool{}}
			if tt.active {
				jobs.active["srv-1"] = true
			}
			deps := AgentUpdateDeps{
				Agents:     fakeAgents{agents: tt.agents},
				Dispatcher: &fakeDispatcher{},
				Jobs:       jobs,
				Audit:      &fakeAudit{},
				Servers:    &fakeAgentUpdateServers{servers: []store.Server{tt.server}},
				Packages:   AgentPackages{Version: tt.version, DistDir: dir},
			}
			recorder := postAgentUpdate(newAgentUpdateMux(store.StageFull, deps),
				"/api/v1/servers/srv-1/agent/update", tt.body)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if code := errorCodeOf(t, recorder); code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if jobs := jobs.createdJobs(); len(jobs) != 0 {
				t.Errorf("a rejected update created jobs: %+v", jobs)
			}
		})
	}
}

func TestAgentUpdateDispatchFailures(t *testing.T) {
	tests := []struct {
		name       string
		dispatch   error
		wantStatus int
		wantCode   string
	}{
		{name: "offline", dispatch: hub.ErrNotConnected, wantStatus: http.StatusConflict, wantCode: "agent_offline"},
		{name: "busy", dispatch: hub.ErrAgentBusy, wantStatus: http.StatusServiceUnavailable, wantCode: "agent_busy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newAgentDistDir(t, "amd64")
			jobs := &fakeAgentUpdateJobs{}
			dispatcher := &fakeDispatcher{err: tt.dispatch}
			deps := AgentUpdateDeps{
				Agents:     fakeAgents{agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"}}},
				Dispatcher: dispatcher,
				Jobs:       jobs,
				Audit:      &fakeAudit{},
				Servers:    &fakeAgentUpdateServers{servers: []store.Server{{ID: "srv-1", Name: "vps-1"}}},
				Packages:   AgentPackages{Version: "1.2.3", DistDir: dir},
			}
			recorder := postAgentUpdate(newAgentUpdateMux(store.StageFull, deps),
				"/api/v1/servers/srv-1/agent/update", `{"confirm":true}`)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if code := errorCodeOf(t, recorder); code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if len(jobs.createdJobs()) != 1 {
				t.Error("the job was not created before dispatch")
			}
			if len(jobs.updates) != 1 || !strings.HasSuffix(jobs.updates[0], ":"+store.JobFailed+":"+tt.dispatch.Error()) {
				t.Errorf("updates = %v, want the job failed", jobs.updates)
			}
		})
	}
}

func TestAgentUpdateAll(t *testing.T) {
	dir := newAgentDistDir(t, "amd64")
	servers := []store.Server{
		{ID: "srv-1", Name: "vps-web-01"},
		{ID: "srv-2", Name: "vps-db-01"},
		{ID: "srv-3", Name: "vps-old-01"},
		{ID: "srv-4", Name: "vps-busy-01"},
	}
	agents := []hub.AgentInfo{
		{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"},
		{ServerID: "srv-3", AgentVersion: "1.2.3", Arch: "amd64"},
		{ServerID: "srv-4", AgentVersion: "1.2.2", Arch: "amd64"},
	}
	jobs := &fakeAgentUpdateJobs{active: map[string]bool{"srv-4": true}}
	dispatcher := &fakeDispatcher{}
	audit := &fakeAudit{}
	deps := AgentUpdateDeps{
		Agents:     fakeAgents{agents: agents},
		Dispatcher: dispatcher,
		Jobs:       jobs,
		Audit:      audit,
		Servers:    &fakeAgentUpdateServers{servers: servers},
		Packages:   AgentPackages{Version: "1.2.3", DistDir: dir},
	}

	recorder := postAgentUpdate(newAgentUpdateMux(store.StageFull, deps),
		"/api/v1/agents/update-all", `{"confirm":true}`)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", recorder.Code, recorder.Body.String())
	}
	var response agentUpdateAllResponse
	decodeRecorder(t, recorder, &response)
	if len(response.Queued) != 1 || response.Queued[0] != "srv-1" {
		t.Errorf("queued = %v, want [srv-1]", response.Queued)
	}
	wantSkipped := []agentUpdateSkipped{
		{ID: "srv-2", Reason: "agent_offline"},
		{ID: "srv-3", Reason: "no_update"},
		{ID: "srv-4", Reason: "update_in_progress"},
	}
	if len(response.Skipped) != len(wantSkipped) {
		t.Fatalf("skipped = %v, want %v", response.Skipped, wantSkipped)
	}
	for index := range wantSkipped {
		if response.Skipped[index] != wantSkipped[index] {
			t.Errorf("skipped[%d] = %+v, want %+v", index, response.Skipped[index], wantSkipped[index])
		}
	}
	if len(jobs.createdJobs()) != 1 || len(dispatcher.sent()) != 1 {
		t.Errorf("jobs = %v, dispatched = %v", jobs.createdJobs(), dispatcher.sent())
	}
	if len(audit.events) != 1 || audit.events[0].Details["server_name"] != "vps-web-01" {
		t.Errorf("audit = %+v, want only the queued server", audit.events)
	}
}

func TestAgentUpdateAllRequiresConfirmation(t *testing.T) {
	jobs := &fakeAgentUpdateJobs{}
	deps := AgentUpdateDeps{
		Agents:     fakeAgents{},
		Dispatcher: &fakeDispatcher{},
		Jobs:       jobs,
		Audit:      &fakeAudit{},
		Servers:    &fakeAgentUpdateServers{},
		Packages:   AgentPackages{Version: "1.2.3", DistDir: t.TempDir()},
	}
	recorder := postAgentUpdate(newAgentUpdateMux(store.StageFull, deps),
		"/api/v1/agents/update-all", `{}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if code := errorCodeOf(t, recorder); code != "confirmation_required" {
		t.Errorf("code = %q, want confirmation_required", code)
	}
	if jobs := jobs.createdJobs(); len(jobs) != 0 {
		t.Errorf("jobs = %v, want none", jobs)
	}
}

func TestAgentUpdateEndpointsRequireAFullSession(t *testing.T) {
	dir := newAgentDistDir(t, "amd64")
	deps := AgentUpdateDeps{
		Agents:     fakeAgents{agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"}}},
		Dispatcher: &fakeDispatcher{},
		Jobs:       &fakeAgentUpdateJobs{},
		Audit:      &fakeAudit{},
		Servers:    &fakeAgentUpdateServers{servers: []store.Server{{ID: "srv-1", Name: "vps-1"}}},
		Packages:   AgentPackages{Version: "1.2.3", DistDir: dir},
	}
	handler := newAgentUpdateMux(store.StageMFAPending, deps)
	for _, path := range []string{"/api/v1/servers/srv-1/agent/update", "/api/v1/agents/update-all"} {
		recorder := postAgentUpdate(handler, path, `{"confirm":true}`)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, recorder.Code)
		}
		if code := errorCodeOf(t, recorder); code != "mfa_required" {
			t.Errorf("%s: code = %q, want mfa_required", path, code)
		}
	}
}

func TestServersAgentUpdateAvailability(t *testing.T) {
	dir := newAgentDistDir(t, "amd64")
	servers := []store.Server{
		{ID: "srv-old", Name: "old"},
		{ID: "srv-same", Name: "same"},
		{ID: "srv-newer", Name: "newer"},
		{ID: "srv-dev", Name: "dev"},
		{ID: "srv-unknown", Name: "unknown"},
		{ID: "srv-arm-missing", Name: "arm-missing"},
	}
	agents := []hub.AgentInfo{
		{ServerID: "srv-old", AgentVersion: "1.2.2", Arch: "x86_64"},
		{ServerID: "srv-same", AgentVersion: "1.2.3", Arch: "amd64"},
		{ServerID: "srv-newer", AgentVersion: "1.3.0", Arch: "amd64"},
		{ServerID: "srv-dev", AgentVersion: "dev", Arch: "amd64"},
		{ServerID: "srv-unknown", AgentVersion: "1.2.2", Arch: "mips"},
		{ServerID: "srv-arm-missing", AgentVersion: "1.0.0", Arch: "arm64"},
	}
	deps := ServerDeps{
		Servers:  fakeServers{servers: servers},
		Agents:   fakeAgents{agents: agents},
		Packages: AgentPackages{Version: "1.2.3", DistDir: dir},
	}
	full := RequireFull(withAdminEffective(NewServersHandler(deps)))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		full.ServeHTTP(w, r.WithContext(withSession(r.Context(), Session{
			User:  store.User{ID: "user-1", Username: "admin"},
			Stage: store.StageFull,
		})))
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var body serversResponse
	decodeRecorder(t, recorder, &body)
	if body.AgentLatestVersion != "1.2.3" {
		t.Errorf("agent_latest_version = %q, want 1.2.3", body.AgentLatestVersion)
	}
	want := map[string]bool{
		"srv-old":         true,
		"srv-same":        false,
		"srv-newer":       false,
		"srv-dev":         true,
		"srv-unknown":     false,
		"srv-arm-missing": false,
	}
	got := make(map[string]bool, len(body.Servers))
	for _, entry := range body.Servers {
		got[entry.ID] = entry.AgentUpdateAvailable
	}
	for id, available := range want {
		if got[id] != available {
			t.Errorf("%s: agent_update_available = %t, want %t", id, got[id], available)
		}
	}
}

func TestServersAgentLatestVersionIsEmptyWithoutASemverCore(t *testing.T) {
	deps := ServerDeps{
		Servers:  fakeServers{},
		Agents:   fakeAgents{},
		Packages: AgentPackages{Version: "dev", DistDir: t.TempDir()},
	}
	handler := withAdminEffective(NewServersHandler(deps))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body serversResponse
	decodeRecorder(t, recorder, &body)
	if body.AgentLatestVersion != "" {
		t.Errorf("agent_latest_version = %q, want empty", body.AgentLatestVersion)
	}
}

func TestAgentUpdateServerLookupFailures(t *testing.T) {
	dir := newAgentDistDir(t, "amd64")
	tests := []struct {
		name    string
		servers *fakeAgentUpdateServers
		want    int
	}{
		{name: "get error", servers: &fakeAgentUpdateServers{getErr: errors.New("database")}, want: http.StatusInternalServerError},
		{name: "list error", servers: &fakeAgentUpdateServers{listErr: errors.New("database")}, want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := AgentUpdateDeps{
				Agents:     fakeAgents{agents: []hub.AgentInfo{{ServerID: "srv-1", AgentVersion: "1.2.2", Arch: "amd64"}}},
				Dispatcher: &fakeDispatcher{},
				Jobs:       &fakeAgentUpdateJobs{},
				Audit:      &fakeAudit{},
				Servers:    tt.servers,
				Packages:   AgentPackages{Version: "1.2.3", DistDir: dir},
			}
			path := "/api/v1/servers/srv-1/agent/update"
			if tt.name == "list error" {
				path = "/api/v1/agents/update-all"
			}
			recorder := postAgentUpdate(newAgentUpdateMux(store.StageFull, deps), path, `{"confirm":true}`)
			if recorder.Code != tt.want {
				t.Errorf("status = %d, want %d", recorder.Code, tt.want)
			}
		})
	}
}
