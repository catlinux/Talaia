package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/rbac"
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

// fakeServerRemoval records what the remove endpoint asks of the store.
type fakeServerRemoval struct {
	server    store.Server
	getErr    error
	removeErr error
	removed   []string
}

func (f *fakeServerRemoval) GetServer(context.Context, string) (store.Server, error) {
	if f.getErr != nil {
		return store.Server{}, f.getErr
	}
	return f.server, nil
}

func (f *fakeServerRemoval) RemoveServer(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return f.removeErr
}

// fakeDisconnect records the live connections the endpoint closed.
type fakeDisconnect struct{ ids []string }

func (f *fakeDisconnect) Disconnect(serverID string) { f.ids = append(f.ids, serverID) }

// fakeAudit records the audit events of the endpoint.
type fakeAudit struct{ events []store.AuditEvent }

func (f *fakeAudit) AppendAudit(_ context.Context, event store.AuditEvent) error {
	f.events = append(f.events, event)
	return nil
}

// removeServerHandler mounts the DELETE route the way the router does.
func removeServerHandler(deps ServerDeps) http.Handler {
	router := chi.NewRouter()
	router.Delete("/api/v1/servers/{serverID}", NewServerRemoveHandler(deps).ServeHTTP)
	return router
}

// deleteServer calls the endpoint with the given body.
func deleteServer(handler http.Handler, id, body string, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/servers/"+id, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// serversHandler mounts the endpoint the way the router does: behind the session
// middleware and RequireFull.
func serversHandler(st *store.Store, clock *testClock, deps ServerDeps) http.Handler {
	return Sessions(SessionDeps{Store: st, Now: clock.Now})(RequireFull(withAdminEffective(NewServersHandler(deps))))
}

// adminTestEffective stands in for the authorizer of a handler test that does
// not mount the whole router.
func adminTestEffective() rbac.Effective {
	return rbac.Resolve([]rbac.Binding{{
		Scope: rbac.Scope{Type: rbac.ScopeOrganization},
		Role:  rbac.RoleAdmin,
	}})
}

func withAdminEffective(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(withEffective(r.Context(), adminTestEffective())))
	})
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
		AptKnown:          true,
		AptUpgradable:     5,
		AptSecurity:       2,
		RebootRequired:    true,
		AptCheckedAt:      seen,
		AptListsUpdatedAt: seen.Add(-time.Hour),
		HostTerminal:      true,
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
		live.ComposeProjects != 1 || live.Services != 5 || !live.HostTerminal ||
		live.Apt == nil || live.Apt.Upgradable != 5 || live.Apt.Security != 2 ||
		!live.Apt.RebootRequired || live.Apt.CheckedAt == nil || !live.Apt.CheckedAt.Equal(seen) ||
		live.Apt.ListsUpdatedAt == nil || !live.Apt.ListsUpdatedAt.Equal(seen.Add(-time.Hour)) {
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

func TestServersUpdateInfoRemainsForAdminAndServerViewer(t *testing.T) {
	server := store.Server{ID: "server-update-info", Name: "server"}
	for _, test := range []struct {
		name     string
		bindings []rbac.Binding
	}{
		{name: "admin", bindings: []rbac.Binding{{Scope: rbac.Scope{Type: rbac.ScopeOrganization}, Role: rbac.RoleAdmin}}},
		{name: "server viewer", bindings: []rbac.Binding{{Scope: rbac.Scope{Type: rbac.ScopeServer, ServerID: server.ID}, Role: rbac.RoleViewer}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			effective := rbac.Resolve(test.bindings)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				NewServersHandler(ServerDeps{
					Servers:  fakeServers{servers: []store.Server{server}},
					Agents:   fakeAgents{},
					Packages: AgentPackages{Version: "1.2.3", UpgradeCommand: "upgrade-agent"},
				}).ServeHTTP(w, r.WithContext(withEffective(r.Context(), effective)))
			})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil))
			var body serversResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.AgentLatestVersion != "1.2.3" || body.AgentUpgradeCommand != "upgrade-agent" {
				t.Fatalf("update info = %q, %q", body.AgentLatestVersion, body.AgentUpgradeCommand)
			}
		})
	}
}

func TestRemoveServerRequiresConfirmation(t *testing.T) {
	removal := &fakeServerRemoval{server: store.Server{ID: "srv-1", Name: "vps-web-01"}}
	handler := removeServerHandler(ServerDeps{Remover: removal})

	for _, body := range []string{`{}`, `{"confirm":false}`, `not json`} {
		recorder := deleteServer(handler, "srv-1", body, "")
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, recorder.Code)
		}
		if code := errorCodeOf(t, recorder); code != "confirmation_required" {
			t.Errorf("body %q: code = %q, want confirmation_required", body, code)
		}
	}
	if len(removal.removed) != 0 {
		t.Errorf("removed without confirmation: %v", removal.removed)
	}
}

func TestRemoveServerClosesTheConnectionAndAudits(t *testing.T) {
	removal := &fakeServerRemoval{server: store.Server{ID: "srv-1", Name: "vps-web-01"}}
	disconnect := &fakeDisconnect{}
	audit := &fakeAudit{}
	handler := removeServerHandler(ServerDeps{
		Remover:    removal,
		Disconnect: disconnect,
		Audit:      audit,
	})

	recorder := deleteServer(handler, "srv-1", `{"confirm":true}`, "")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", recorder.Body.String())
	}
	if len(removal.removed) != 1 || removal.removed[0] != "srv-1" {
		t.Errorf("removed = %v, want srv-1", removal.removed)
	}
	if len(disconnect.ids) != 1 || disconnect.ids[0] != "srv-1" {
		t.Errorf("disconnected = %v, want srv-1", disconnect.ids)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	event := audit.events[0]
	if event.Action != "server_removed" || event.Result != "success" {
		t.Errorf("audit event = %+v", event)
	}
	if event.Details["server_name"] != "vps-web-01" {
		t.Errorf("audit details = %v, want the server name", event.Details)
	}
}

func TestRemoveServerUnknownOrAlreadyRemoved(t *testing.T) {
	tests := []struct {
		name    string
		removal *fakeServerRemoval
	}{
		{
			name:    "unknown",
			removal: &fakeServerRemoval{getErr: store.ErrNotFound},
		},
		{
			name: "already removed",
			removal: &fakeServerRemoval{
				server:    store.Server{ID: "srv-1", Name: "vps-web-01"},
				removeErr: store.ErrNotFound,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			disconnect := &fakeDisconnect{}
			audit := &fakeAudit{}
			handler := removeServerHandler(ServerDeps{
				Remover:    test.removal,
				Disconnect: disconnect,
				Audit:      audit,
			})

			recorder := deleteServer(handler, "srv-1", `{"confirm":true}`, "")
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", recorder.Code)
			}
			if code := errorCodeOf(t, recorder); code != "not_found" {
				t.Errorf("code = %q, want not_found", code)
			}
			if len(disconnect.ids) != 0 || len(audit.events) != 0 {
				t.Errorf("a failed removal disconnected %v or audited %v", disconnect.ids, audit.events)
			}
		})
	}
}

func TestRemoveServerRequiresAFullSession(t *testing.T) {
	removal := &fakeServerRemoval{server: store.Server{ID: "srv-1", Name: "vps-web-01"}}
	audit := &fakeAudit{}
	deps := ServerDeps{Remover: removal, Audit: audit}

	fullStore, fullClock, _, fullToken := newSessionFixture(t, store.StageFull)
	full := Sessions(SessionDeps{Store: fullStore, Now: fullClock.Now})(
		RequireFull(removeServerHandler(deps)))

	recorder := deleteServer(full, "srv-1", `{"confirm":true}`, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("without a session: status = %d, want 401", recorder.Code)
	}

	pendingStore, pendingClock, _, pendingToken := newSessionFixture(t, store.StageMFAPending)
	pending := Sessions(SessionDeps{Store: pendingStore, Now: pendingClock.Now})(
		RequireFull(removeServerHandler(deps)))
	recorder = deleteServer(pending, "srv-1", `{"confirm":true}`, pendingToken)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("with a pending session: status = %d, want 401", recorder.Code)
	}

	recorder = deleteServer(full, "srv-1", `{"confirm":true}`, fullToken)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("with a full session: status = %d, want 204", recorder.Code)
	}
	if len(audit.events) != 1 || audit.events[0].Actor != "admin" {
		t.Errorf("audit = %+v, want the signed-in actor", audit.events)
	}
}

func TestLiveOfCarriesAptOnlyWhenTheAgentReportedIt(t *testing.T) {
	checked := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	lists := checked.Add(-45 * time.Minute)
	live := liveOf(hub.AgentInfo{
		AptKnown:          true,
		AptUpgradable:     4,
		AptSecurity:       2,
		RebootRequired:    true,
		AptCheckedAt:      checked,
		AptListsUpdatedAt: lists,
		HostTerminal:      true,
	})
	if live.Apt == nil || live.Apt.Upgradable != 4 || live.Apt.Security != 2 ||
		!live.Apt.RebootRequired || live.Apt.CheckedAt == nil || !live.Apt.CheckedAt.Equal(checked) ||
		live.Apt.ListsUpdatedAt == nil || !live.Apt.ListsUpdatedAt.Equal(lists) || !live.HostTerminal {
		t.Errorf("live = %+v, want the apt summary and host terminal flag", live)
	}

	unknown := liveOf(hub.AgentInfo{})
	if unknown.Apt != nil {
		t.Errorf("apt = %+v, want nil when the agent did not report one", unknown.Apt)
	}
	encoded, err := json.Marshal(unknown)
	if err != nil {
		t.Fatalf("encode the live data: %v", err)
	}
	if strings.Contains(string(encoded), `"apt"`) {
		t.Errorf("the JSON of an agent without apt has one: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"host_terminal":false`) {
		t.Errorf("the JSON of an agent without a host terminal = %s", encoded)
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
