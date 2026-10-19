package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
)

// fakeServers lists the given rows.
type fakeServers struct{ servers []store.Server }

func (f fakeServers) ListServers(context.Context) ([]store.Server, error) {
	return f.servers, nil
}

// fakeAgents reports the given connections.
type fakeAgents struct{ agents []hub.AgentInfo }

func (f fakeAgents) Connected() []hub.AgentInfo { return f.agents }

// serversHandler mounts the endpoint the way the router does: behind the session
// middleware and RequireFull.
func serversHandler(st *store.Store, clock *testClock, deps ServerDeps) http.Handler {
	return Sessions(SessionDeps{Store: st, Now: clock.Now})(RequireFull(NewServersHandler(deps)))
}

// getServers calls the endpoint with the session cookie, if any.
func getServers(handler http.Handler, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	if token != "" {
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// errorCodeOf decodes the stable code of an error response.
func errorCodeOf(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var body errorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error body: %v", err)
	}
	return body.Error.Code
}

func TestServersEndpointRequiresAFullSession(t *testing.T) {
	deps := ServerDeps{Servers: fakeServers{}, Agents: fakeAgents{}}

	fullStore, fullClock, _, fullToken := newSessionFixture(t, store.StageFull)
	full := serversHandler(fullStore, fullClock, deps)

	recorder := getServers(full, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("without a session: status = %d, want 401", recorder.Code)
	}
	if code := errorCodeOf(t, recorder); code != "session_required" {
		t.Errorf("code = %q, want session_required", code)
	}

	pendingStore, pendingClock, _, pendingToken := newSessionFixture(t, store.StageMFAPending)
	recorder = getServers(serversHandler(pendingStore, pendingClock, deps), pendingToken)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("with a pending session: status = %d, want 401", recorder.Code)
	}
	if code := errorCodeOf(t, recorder); code != "mfa_required" {
		t.Errorf("code = %q, want mfa_required", code)
	}

	recorder = getServers(full, fullToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("with a full session: status = %d, want 200", recorder.Code)
	}
}

func TestServersEndpointMergesTheLiveAgents(t *testing.T) {
	onlineID := "11111111-1111-1111-1111-111111111111"
	offlineID := "22222222-2222-2222-2222-222222222222"
	revokedID := "33333333-3333-3333-3333-333333333333"
	seen := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	expiry := seen.Add(90 * 24 * time.Hour)

	servers := []store.Server{
		{ID: onlineID, Name: "vps-web-01", CreatedAt: seen, LastSeen: seen,
			CertSerial: "serial-of-the-online-server", CertNotAfter: expiry},
		{ID: offlineID, Name: "vps-db-01", CreatedAt: seen},
		{ID: revokedID, Name: "vps-old-01", CreatedAt: seen, RevokedAt: seen,
			CertSerial: "serial-of-the-revoked-server", CertNotAfter: expiry},
	}
	agents := []hub.AgentInfo{{
		ServerID:          onlineID,
		Hostname:          "vps-web-01",
		OSName:            "Debian GNU/Linux",
		OSVersion:         "13",
		Kernel:            "6.12.0-amd64",
		Arch:              "amd64",
		AgentVersion:      "0.2.0",
		CPUCount:          4,
		MemTotalBytes:     8 << 30,
		LastSeen:          seen,
		LastCPUPercent:    12.5,
		MemUsedBytes:      1 << 30,
		DiskUsedBytes:     2 << 30,
		DiskTotalBytes:    4 << 30,
		NetRxBytes:        11,
		NetTxBytes:        22,
		DockerAvailable:   true,
		Containers:        3,
		RunningContainers: 2,
		ComposeProjects:   1,
		Services:          5,
	}}

	st, clock, _, token := newSessionFixture(t, store.StageFull)
	recorder := getServers(serversHandler(st, clock, ServerDeps{
		Servers: fakeServers{servers: servers},
		Agents:  fakeAgents{agents: agents},
	}), token)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); strings.Contains(body, "serial-of-the-") {
		t.Errorf("the response exposes a certificate serial: %s", body)
	}

	var body serversResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the body: %v", err)
	}
	if len(body.Servers) != 3 {
		t.Fatalf("servers = %d, want 3", len(body.Servers))
	}

	online := body.Servers[0]
	if online.ID != onlineID || online.Name != "vps-web-01" || !online.Connected || online.Revoked {
		t.Errorf("online server = %+v", online)
	}
	if online.Live == nil {
		t.Fatal("the online server has no live data")
	}
	live := online.Live
	if live.Hostname != "vps-web-01" || live.AgentVersion != "0.2.0" ||
		live.OSName != "Debian GNU/Linux" || live.OSVersion != "13" ||
		live.Kernel != "6.12.0-amd64" || live.Arch != "amd64" ||
		live.CPUCount != 4 || live.CPUPercent != 12.5 ||
		live.MemUsedBytes != 1<<30 || live.MemTotalBytes != 8<<30 ||
		live.DiskUsedBytes != 2<<30 || live.DiskTotalBytes != 4<<30 ||
		live.NetRxBytes != 11 || live.NetTxBytes != 22 ||
		!live.DockerAvailable || live.Containers != 3 || live.RunningContainers != 2 ||
		live.ComposeProjects != 1 || live.Services != 5 {
		t.Errorf("live = %+v", live)
	}
	if !live.SeenAt.Equal(seen) {
		t.Errorf("seen_at = %s, want %s", live.SeenAt, seen)
	}
	if online.LastSeen == nil || !online.LastSeen.Equal(seen) {
		t.Errorf("last_seen = %v, want %s", online.LastSeen, seen)
	}
	if online.CertNotAfter == nil || !online.CertNotAfter.Equal(expiry) {
		t.Errorf("cert_not_after = %v, want %s", online.CertNotAfter, expiry)
	}

	offline := body.Servers[1]
	if offline.ID != offlineID || offline.Connected || offline.Live != nil {
		t.Errorf("offline server = %+v", offline)
	}
	// A server that never connected has no last_seen in the JSON.
	var raw struct {
		Servers []map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode the raw body: %v", err)
	}
	if value, ok := raw.Servers[1]["last_seen"]; !ok || value != nil {
		t.Errorf("offline last_seen = %v, want null", value)
	}
	if value, ok := raw.Servers[1]["cert_not_after"]; !ok || value != nil {
		t.Errorf("offline cert_not_after = %v, want null", value)
	}

	revoked := body.Servers[2]
	if revoked.ID != revokedID || !revoked.Revoked || revoked.Connected {
		t.Errorf("revoked server = %+v", revoked)
	}
}

func TestServersEndpointShowsTheDevAgents(t *testing.T) {
	seen := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	agents := []hub.AgentInfo{
		{ServerID: "", Hostname: "dev-box", AgentVersion: "dev", CPUCount: 2, LastSeen: seen},
		// An agent whose enrolled row is gone is not listed the second time.
		{ServerID: "99999999-9999-9999-9999-999999999999", Hostname: "orphan", LastSeen: seen},
	}

	st, clock, _, token := newSessionFixture(t, store.StageFull)
	recorder := getServers(serversHandler(st, clock, ServerDeps{
		Servers: fakeServers{},
		Agents:  fakeAgents{agents: agents},
	}), token)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var body serversResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the body: %v", err)
	}
	if len(body.Servers) != 1 {
		t.Fatalf("servers = %d, want only the development agent", len(body.Servers))
	}

	entry := body.Servers[0]
	if entry.ID != "dev:dev-box" || entry.Name != "dev-box" {
		t.Errorf("entry = %+v, want the synthetic development id", entry)
	}
	if !entry.Connected || entry.Live == nil || entry.Live.Hostname != "dev-box" {
		t.Errorf("entry = %+v, want the live data", entry)
	}
	if entry.CreatedAt != nil || entry.LastSeen != nil || entry.CertNotAfter != nil || entry.Revoked {
		t.Errorf("entry = %+v, want no enrolled state", entry)
	}
}
