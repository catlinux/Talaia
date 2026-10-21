package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// controlledAgents is the list of connected agents the tests control.
type controlledAgents struct {
	mu     sync.Mutex
	agents []hub.AgentInfo
}

func (f *controlledAgents) Connected() []hub.AgentInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]hub.AgentInfo(nil), f.agents...)
}

func (f *controlledAgents) set(agents ...hub.AgentInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents = agents
}

// fakeDispatcher records the commands and answers with err.
type fakeDispatcher struct {
	mu       sync.Mutex
	messages []*agentv1.CoreMsg
	serverID string
	err      error
}

func (f *fakeDispatcher) Dispatch(serverID string, msg *agentv1.CoreMsg) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.serverID = serverID
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeDispatcher) sent() []*agentv1.CoreMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*agentv1.CoreMsg(nil), f.messages...)
}

// enrollTestServer enrolls a throw-away server so jobs have a server row.
func enrollTestServer(t *testing.T, panel *testPanel) string {
	t.Helper()

	ctx := context.Background()
	name := "ctr-" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
	token, err := panel.store.CreateEnrollmentToken(ctx, name, time.Minute)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	id, _, err := panel.store.EnrollServer(ctx, token, func(string) ([]byte, string, time.Time, error) {
		return []byte("cert"), "0a0b", time.Now().Add(time.Hour), nil
	})
	if err != nil {
		t.Fatalf("enroll server: %v", err)
	}
	return id
}

func agentWithContainers(serverID string) hub.AgentInfo {
	return hub.AgentInfo{
		ServerID:        serverID,
		Hostname:        "vps-test",
		DockerAvailable: true,
		ContainerList: []*agentv1.Container{
			{Id: "aaaaaaaaaaaa", Name: "web", Image: "nginx:1.27", State: "running", Status: "Up 2 hours", ComposeProject: "shop", ComposeService: "web"},
			{Id: "bbbbbbbbbbbb", Name: "db", Image: "postgres:16", State: "exited", Status: "Exited (0) 1 hour ago"},
		},
	}
}

func containerPath(serverID, container string) string {
	return "/api/v1/servers/" + serverID + "/containers/" + container + "/actions"
}

func TestContainerEndpointsNeedAFullSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)

	if response := panel.get(t, "/api/v1/servers/x/containers"); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("list status = %d, want 401", response.StatusCode)
	}
	if response := panel.post(t, containerPath("x", "web"), map[string]any{"action": "stop"}); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("action status = %d, want 401", response.StatusCode)
	}
	if response := panel.get(t, "/api/v1/jobs/x"); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("job status = %d, want 401", response.StatusCode)
	}
}

func TestListContainers(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := "11111111-1111-1111-1111-111111111111"

	var offline containersResponse
	decodeBody(t, panel.get(t, "/api/v1/servers/"+serverID+"/containers"), &offline)
	if offline.Connected || len(offline.Containers) != 0 {
		t.Errorf("offline answer = %+v", offline)
	}

	panel.agents.set(agentWithContainers(serverID))
	var online containersResponse
	decodeBody(t, panel.get(t, "/api/v1/servers/"+serverID+"/containers"), &online)
	if !online.Connected || !online.DockerAvailable || len(online.Containers) != 2 {
		t.Fatalf("online answer = %+v", online)
	}
	if first := online.Containers[0]; first.Name != "web" || first.ComposeProject != "shop" || first.State != "running" {
		t.Errorf("first container = %+v", first)
	}
}

func TestContainerActionCreatesAJobAndSendsTheCommand(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithContainers(serverID))

	response := panel.post(t, containerPath(serverID, "web"), map[string]any{"action": "restart"})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.StatusCode)
	}
	var created jobCreatedResponse
	decodeBody(t, response, &created)

	sent := panel.dispatcher.sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d commands, want 1", len(sent))
	}
	command := sent[0]
	if command.GetJobId() != created.JobID ||
		command.GetContainerAction().GetContainer() != "web" ||
		command.GetContainerAction().GetAction() != agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_RESTART ||
		panel.dispatcher.serverID != serverID {
		t.Errorf("command = %v for %s", command, panel.dispatcher.serverID)
	}

	// The job is readable, pending until the agent reports.
	var job jobResponse
	decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
	if job.State != store.JobPending || job.CreatedBy != "admin" || job.Params["container"] != "web" {
		t.Errorf("job = %+v", job)
	}

	// The agent's events move it forward.
	recorder := JobRecorder{Jobs: panel.store}
	recorder.RecordJobEvent(context.Background(), serverID, &agentv1.JobEvent{JobId: created.JobID, State: agentv1.JobState_JOB_STATE_RUNNING})
	recorder.RecordJobEvent(context.Background(), serverID, &agentv1.JobEvent{JobId: created.JobID, State: agentv1.JobState_JOB_STATE_SUCCEEDED})
	decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
	if job.State != store.JobSucceeded || job.FinishedAt == nil {
		t.Errorf("finished job = %+v", job)
	}

	// The audit log records who asked, not the output.
	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action == "container_restart" && entry.Actor == "admin" && entry.Details["container"] == "web" {
			found = true
		}
	}
	if !found {
		t.Error("the container action was not audited")
	}
}

func TestContainerActionValidation(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithContainers(serverID))

	tests := []struct {
		name      string
		container string
		body      map[string]any
		want      int
	}{
		{"unknown action", "web", map[string]any{"action": "explode"}, http.StatusBadRequest},
		{"missing action", "web", map[string]any{}, http.StatusBadRequest},
		{"remove without confirmation", "web", map[string]any{"action": "remove"}, http.StatusBadRequest},
		{"unknown container", "ghost", map[string]any{"action": "stop"}, http.StatusNotFound},
		{"bad reference", "web;rm", map[string]any{"action": "stop"}, http.StatusBadRequest},
		{"remove with confirmation", "db", map[string]any{"action": "remove", "confirm": true}, http.StatusAccepted},
		{"full id of a known container", "aaaaaaaaaaaa0000000000000000000000000000000000000000000000000000", map[string]any{"action": "stop"}, http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := panel.post(t, containerPath(serverID, tt.container), tt.body).StatusCode; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestContainerActionWhenTheAgentIsGoneOrBusy(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	body := map[string]any{"action": "stop"}

	// Not connected.
	if got := panel.post(t, containerPath(serverID, "web"), body).StatusCode; got != http.StatusConflict {
		t.Errorf("offline status = %d, want 409", got)
	}

	// Connected, but the dispatch fails: the job must not stay pending.
	panel.agents.set(agentWithContainers(serverID))
	panel.dispatcher.err = hub.ErrAgentBusy
	response := panel.post(t, containerPath(serverID, "web"), body)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("busy status = %d, want 503", response.StatusCode)
	}
}

func TestStaleJobsAreClosed(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)

	job, err := panel.store.CreateJob(context.Background(), serverID, containerActionKind, nil, "admin")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	// Age the job past the timeout.
	if _, err := panel.store.FailStaleJob(context.Background(), job.ID, 0); err != nil {
		t.Fatalf("FailStaleJob: %v", err)
	}

	var got jobResponse
	decodeBody(t, panel.get(t, "/api/v1/jobs/"+job.ID), &got)
	if got.State != store.JobFailed || got.Message != "timed out" {
		t.Errorf("job = %+v", got)
	}

	if response := panel.get(t, "/api/v1/jobs/00000000-0000-0000-0000-000000000000"); response.StatusCode != http.StatusNotFound {
		t.Errorf("unknown job status = %d, want 404", response.StatusCode)
	}
}

func TestJobTimeoutOutlastsTheAgent(t *testing.T) {
	// The longest time the agent gives each kind of work.
	agent := map[string]time.Duration{
		containerActionKind: 90 * time.Second,
		stackReadKind:       90 * time.Second,
		stackAdoptKind:      90 * time.Second,
		resourcesListKind:   90 * time.Second,
		stackWriteKind:      2 * time.Minute,
		resourcesRemoveKind: 5 * time.Minute,
		stackActionKind:     10 * time.Minute,
	}
	for kind, limit := range agent {
		if got := jobTimeout(kind); got <= limit {
			t.Errorf("jobTimeout(%s) = %s, want more than %s", kind, got, limit)
		}
	}
}

// get sends a GET with the session of the panel client.
func (p *testPanel) get(t *testing.T, path string) *http.Response {
	t.Helper()
	return p.do(t, p.client, http.MethodGet, path, nil)
}

func logsPath(serverID, container string) string {
	return "/api/v1/servers/" + serverID + "/containers/" + container + "/logs"
}

func TestContainerLogsCreateAJobWhoseOutputIsReadable(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithContainers(serverID))

	response := panel.post(t, logsPath(serverID, "web"), map[string]any{"tail": 50})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.StatusCode)
	}
	var created jobCreatedResponse
	decodeBody(t, response, &created)

	sent := panel.dispatcher.sent()
	if len(sent) != 1 || sent[0].GetReadLogs().GetContainer() != "web" || sent[0].GetReadLogs().GetTail() != 50 ||
		sent[0].GetJobId() != created.JobID {
		t.Fatalf("sent = %v", sent)
	}
	if sent[0].GetReadLogs().GetSince() != "" || sent[0].GetReadLogs().GetTimestamps() {
		t.Errorf("an old request changed: %v", sent[0].GetReadLogs())
	}

	// The agent answers with the lines; they come back in the job.
	recorder := JobRecorder{Jobs: panel.store}
	recorder.RecordJobEvent(context.Background(), serverID, &agentv1.JobEvent{JobId: created.JobID, State: agentv1.JobState_JOB_STATE_RUNNING})
	recorder.RecordJobEvent(context.Background(), serverID, &agentv1.JobEvent{
		JobId: created.JobID, State: agentv1.JobState_JOB_STATE_SUCCEEDED, Output: "2026-10-20T10:00:00Z hello\n",
	})
	var job jobResponse
	decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
	if job.State != store.JobSucceeded || job.Output != "2026-10-20T10:00:00Z hello\n" {
		t.Errorf("job = %+v", job)
	}

	// Only the fact is audited, never the text.
	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action == "container_logs" && entry.Actor == "admin" {
			found = true
			if strings.Contains(strings.Join(stringValues(entry.Details), ""), "hello") {
				t.Error("the audit log holds the log text")
			}
		}
	}
	if !found {
		t.Error("the log read was not audited")
	}
}

func TestContainerLogsPassSinceAndTimestamps(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithContainers(serverID))

	response := panel.post(t, logsPath(serverID, "web"), map[string]any{
		"tail": 20, "since": " 2026-10-20T10:00:00.500Z ", "timestamps": true,
	})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.StatusCode)
	}

	sent := panel.dispatcher.sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %v", sent)
	}
	logs := sent[0].GetReadLogs()
	if logs.GetTail() != 20 || logs.GetSince() != "2026-10-20T10:00:00.500Z" || !logs.GetTimestamps() {
		t.Errorf("read logs = %v", logs)
	}
}

func TestContainerLogsValidation(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)

	if got := panel.post(t, logsPath(serverID, "web"), map[string]any{}).StatusCode; got != http.StatusConflict {
		t.Errorf("offline status = %d, want 409", got)
	}

	panel.agents.set(agentWithContainers(serverID))
	tests := []struct {
		name      string
		container string
		body      map[string]any
		want      int
	}{
		{"default tail", "web", map[string]any{}, http.StatusAccepted},
		{"tail too large", "web", map[string]any{"tail": 5000}, http.StatusBadRequest},
		{"negative tail", "web", map[string]any{"tail": -1}, http.StatusBadRequest},
		{"invalid since", "web", map[string]any{"since": "yesterday"}, http.StatusBadRequest},
		{"since without time", "web", map[string]any{"since": "2026-10-20"}, http.StatusBadRequest},
		{"since too long", "web", map[string]any{"since": strings.Repeat("2", 41)}, http.StatusBadRequest},
		{"valid since", "web", map[string]any{"since": "2026-10-20T10:00:00Z"}, http.StatusAccepted},
		{"unknown container", "ghost", map[string]any{}, http.StatusNotFound},
		{"bad reference", "web;rm", map[string]any{}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := panel.post(t, logsPath(serverID, tt.container), tt.body).StatusCode; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestContainerLogsNeedAFullSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)

	if got := panel.post(t, logsPath("x", "web"), map[string]any{}).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", got)
	}
}

func TestListContainersShowsTheFreshUseOfRunningOnes(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := "44444444-4444-4444-4444-444444444444"

	agent := agentWithContainers(serverID)
	agent.StatsAt = time.Now()
	agent.ContainerStats = map[string]*agentv1.ContainerStat{
		"aaaaaaaaaaaa": {Id: "aaaaaaaaaaaa", CpuPercent: 12.5, MemUsedBytes: 1000, MemLimitBytes: 4000, NetRxBytes: 7, NetTxBytes: 9},
		// The container is stopped: a leftover sample must not be shown.
		"bbbbbbbbbbbb": {Id: "bbbbbbbbbbbb", CpuPercent: 99},
	}
	panel.agents.set(agent)

	var list containersResponse
	decodeBody(t, panel.get(t, "/api/v1/servers/"+serverID+"/containers"), &list)

	var web, db containerEntry
	for _, entry := range list.Containers {
		switch entry.Name {
		case "web":
			web = entry
		case "db":
			db = entry
		}
	}
	if web.Use == nil || web.Use.CPUPercent != 12.5 || web.Use.MemUsedBytes != 1000 || web.Use.MemLimitBytes != 4000 || web.Use.NetTxBytes != 9 {
		t.Errorf("web use = %+v", web.Use)
	}
	if db.Use != nil {
		t.Errorf("a stopped container shows use: %+v", db.Use)
	}

	// An old sample is not shown: the agent may have stopped reporting.
	agent.StatsAt = time.Now().Add(-5 * time.Minute)
	panel.agents.set(agent)
	// A fresh value: decoding into the old one would keep its pointers.
	var stale containersResponse
	decodeBody(t, panel.get(t, "/api/v1/servers/"+serverID+"/containers"), &stale)
	for _, entry := range stale.Containers {
		if entry.Use != nil {
			t.Errorf("a stale sample is shown for %s", entry.Name)
		}
	}
}
