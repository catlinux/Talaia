package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/terminal"
)

func createRecordingServer(t *testing.T, panel *testPanel) string {
	t.Helper()
	name := "recording-server-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	id, err := panel.store.CreateServer(context.Background(), name)
	if err != nil {
		t.Fatalf("create recording server: %v", err)
	}
	return id
}

func createRecordingSession(t *testing.T, panel *testPanel, serverID, container, createdBy string) store.TerminalSession {
	t.Helper()
	session, err := panel.store.CreateTerminalSession(context.Background(), serverID, container, "bash", createdBy)
	if err != nil {
		t.Fatalf("create recording session: %v", err)
	}
	return session
}

func appendRecordingChunk(t *testing.T, panel *testPanel, sessionID string, seq int, plain string) {
	t.Helper()
	bound := []byte(sessionID + "|" + strconv.Itoa(seq))
	sealed := panel.box.SealWithContext([]byte(plain), bound)
	if err := panel.store.AppendTerminalChunk(context.Background(), sessionID, seq, sealed, len(plain)); err != nil {
		t.Fatalf("append recording chunk: %v", err)
	}
}

func closeRecordingSession(t *testing.T, panel *testPanel, sessionID string) {
	t.Helper()
	if err := panel.store.CloseTerminalSession(context.Background(), sessionID, "exited"); err != nil {
		t.Fatalf("close recording session: %v", err)
	}
}

func findRecording(entries []recordingEntry, id string) *recordingEntry {
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i]
		}
	}
	return nil
}

func TestRecordingList(t *testing.T) {
	panel := newTestPanel(t)
	admin := panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := createRecordingServer(t, panel)

	container := createRecordingSession(t, panel, serverID, "web-ct", "alice")
	appendRecordingChunk(t, panel, container.ID, 0, "TOPSECRET recording content")
	closeRecordingSession(t, panel, container.ID)
	time.Sleep(2 * time.Millisecond)

	host := createRecordingSession(t, panel, serverID, terminal.HostContainer, "bob")
	time.Sleep(2 * time.Millisecond)
	limitSession := createRecordingSession(t, panel, serverID, "db-ct", "carol")

	response := panel.do(t, panel.client, http.MethodGet, "/api/v1/recordings", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("recording list status = %d, want 200", response.StatusCode)
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read recording list: %v", err)
	}
	if strings.Contains(string(raw), "TOPSECRET") {
		t.Fatal("the recording list contains recording content")
	}
	var entries []recordingEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decode recording list: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("recording entries = %d, want 3", len(entries))
	}
	if entries[0].ID != limitSession.ID || entries[1].ID != host.ID {
		t.Fatalf("recording order = %s, %s, want %s then %s", entries[0].ID, entries[1].ID, limitSession.ID, host.ID)
	}

	containerEntry := findRecording(entries, container.ID)
	if containerEntry == nil {
		t.Fatal("container recording missing")
	}
	if containerEntry.ServerID != serverID || containerEntry.ServerName == "" ||
		containerEntry.Container != "web-ct" || containerEntry.Kind != "container" ||
		containerEntry.Shell != "bash" || containerEntry.User != "alice" ||
		containerEntry.EndedAt == nil || containerEntry.CloseReason != "exited" ||
		containerEntry.DurationSeconds < 0 || containerEntry.Bytes == 0 {
		t.Fatalf("container recording = %+v", containerEntry)
	}

	hostEntry := findRecording(entries, host.ID)
	if hostEntry == nil {
		t.Fatal("host recording missing")
	}
	if hostEntry.Kind != "host" || hostEntry.Container != "" || hostEntry.User != "bob" || hostEntry.EndedAt != nil {
		t.Fatalf("host recording = %+v", hostEntry)
	}

	limited := decodePanelJSON[[]recordingEntry](t, panel.do(t, panel.client, http.MethodGet, "/api/v1/recordings?limit=1", nil))
	if len(limited) != 1 || limited[0].ID != limitSession.ID {
		t.Fatalf("limited recordings = %+v", limited)
	}
	if response := panel.do(t, panel.client, http.MethodGet, "/api/v1/recordings?limit=201", nil); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("large limit status = %d, want 400", response.StatusCode)
	}

	_, nonAdminClient := createSessionOnlyUser(t, panel, "recording-non-admin")
	if response := panel.do(t, nonAdminClient, http.MethodGet, "/api/v1/recordings", nil); response.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin recording list status = %d, want 403", response.StatusCode)
	}
	_, key := directAPIKey(t, panel, admin.ID, nil, time.Now().Add(time.Hour))
	if response := apiKeyRequest(t, panel, http.MethodGet, "/api/v1/recordings", key, nil, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("api key recording list status = %d, want 403", response.StatusCode)
	}
}

func TestRecordingDownloadAndCorruption(t *testing.T) {
	panel := newTestPanel(t)
	admin := panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	ctx := context.Background()
	serverID := createRecordingServer(t, panel)

	session := createRecordingSession(t, panel, serverID, "web-ct", "alice")
	header := `{"version":2,"width":80,"height":24}` + "\n"
	event := `[0.1,"o","hello"]` + "\n"
	appendRecordingChunk(t, panel, session.ID, 0, header)
	appendRecordingChunk(t, panel, session.ID, 1, event)
	closeRecordingSession(t, panel, session.ID)

	response := panel.do(t, panel.client, http.MethodGet, "/api/v1/recordings/"+session.ID+"/download", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("recording download status = %d, want 200", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "application/x-asciicast" {
		t.Fatalf("download content type = %q", got)
	}
	if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "talaia-"+session.ID+".cast") {
		t.Fatalf("download disposition = %q", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("download cache control = %q", got)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read recording download: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("asciicast lines = %q", lines)
	}
	var castHeader map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &castHeader); err != nil || castHeader["version"] != float64(2) {
		t.Fatalf("asciicast header = %q, %v", lines[0], err)
	}
	var castEvent []any
	if err := json.Unmarshal([]byte(lines[1]), &castEvent); err != nil || len(castEvent) != 3 || castEvent[1] != "o" {
		t.Fatalf("asciicast event = %q, %v", lines[1], err)
	}

	entries, err := panel.store.ListAudit(ctx, 100)
	if err != nil {
		t.Fatalf("list recording audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != "recording_download" {
			continue
		}
		found = true
		if entry.Actor != admin.Username || entry.ActorID != admin.ID || entry.Details["session_id"] != session.ID {
			t.Fatalf("recording audit = %+v", entry)
		}
	}
	if !found {
		t.Fatal("the recording download was not audited")
	}

	open := createRecordingSession(t, panel, serverID, "open-ct", "alice")
	if response := panel.do(t, panel.client, http.MethodGet, "/api/v1/recordings/"+open.ID+"/download", nil); response.StatusCode != http.StatusConflict ||
		errorCode(t, response) != "session_open" {
		t.Fatalf("open recording download = %d %q", response.StatusCode, errorCode(t, response))
	}

	if response := panel.do(t, panel.client, http.MethodGet,
		"/api/v1/recordings/00000000-0000-0000-0000-000000000000/download", nil); response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown recording status = %d, want 404", response.StatusCode)
	}
	if response := panel.do(t, panel.client, http.MethodGet,
		"/api/v1/recordings/not-a-uuid/download", nil); response.StatusCode != http.StatusNotFound {
		t.Fatalf("invalid recording id status = %d, want 404", response.StatusCode)
	}

	corrupt := createRecordingSession(t, panel, serverID, "corrupt-ct", "alice")
	if err := panel.store.AppendTerminalChunk(ctx, corrupt.ID, 0, []byte("not a sealed chunk"), 5); err != nil {
		t.Fatalf("append corrupt chunk: %v", err)
	}
	closeRecordingSession(t, panel, corrupt.ID)
	corruptResponse := panel.do(t, panel.client, http.MethodGet,
		"/api/v1/recordings/"+corrupt.ID+"/download", nil)
	corruptBody, err := io.ReadAll(corruptResponse.Body)
	if err != nil {
		t.Fatalf("read corrupt response: %v", err)
	}
	var corruptError errorBody
	if err := json.Unmarshal(corruptBody, &corruptError); err != nil {
		t.Fatalf("decode corrupt response: %v", err)
	}
	if corruptResponse.StatusCode != http.StatusInternalServerError || corruptError.Error.Code != "recording_corrupt" {
		t.Fatalf("corrupt recording = %d %q", corruptResponse.StatusCode, corruptError.Error.Code)
	}
	if bytes.Contains(corruptBody, []byte(`"version":2`)) {
		t.Fatal("a corrupt download leaked recording content")
	}

	_, key := directAPIKey(t, panel, admin.ID, nil, time.Now().Add(time.Hour))
	if response := apiKeyRequest(t, panel, http.MethodGet,
		"/api/v1/recordings/"+session.ID+"/download", key, nil, false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("api key recording download status = %d, want 403", response.StatusCode)
	}
	_, nonAdminClient := createSessionOnlyUser(t, panel, "download-non-admin")
	if response := panel.do(t, nonAdminClient, http.MethodGet, "/api/v1/recordings/"+session.ID+"/download", nil); response.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin recording download status = %d, want 403", response.StatusCode)
	}
}
