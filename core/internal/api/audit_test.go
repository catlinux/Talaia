package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/catlinux/Talaia/core/internal/store"
)

// fakeAuditReader returns the page it was given. When a request has a cursor it
// returns more, which stands for the look-ahead query of the handler.
type fakeAuditReader struct {
	page  []store.AuditEntry
	more  []store.AuditEntry
	err   error
	calls []store.AuditFilter
}

func (f *fakeAuditReader) ListAuditPage(_ context.Context, filter store.AuditFilter) ([]store.AuditEntry, error) {
	f.calls = append(f.calls, filter)
	if f.err != nil {
		return nil, f.err
	}
	if filter.Before != nil {
		return f.more, nil
	}
	return f.page, nil
}

// getAudit calls the endpoint with an optional session cookie.
func getAudit(t *testing.T, handler http.Handler, query, token string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit?"+query, nil)
	if token != "" {
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestAuditHandlerValidatesTheQuery(t *testing.T) {
	validCursor, err := encodeAuditCursor(store.AuditCursor{
		At: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
		ID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	serverID := uuid.NewString()
	longAction := strings.Repeat("a", 65)
	longActor := strings.Repeat("a", 65)

	tests := []struct {
		name      string
		query     string
		wantCode  int
		wantCalls int
		wantLimit int
	}{
		{name: "defaults", query: "", wantCode: http.StatusOK, wantCalls: 1, wantLimit: 50},
		{
			name:      "valid filters",
			query:     url.Values{"limit": {"2"}, "action": {"service_*"}, "server_id": {"dev:dev-box"}, "actor": {"admin"}}.Encode(),
			wantCode:  http.StatusOK,
			wantCalls: 1,
			wantLimit: 2,
		},
		{
			name:      "valid uuid filter",
			query:     url.Values{"server_id": {serverID}}.Encode(),
			wantCode:  http.StatusOK,
			wantCalls: 1,
			wantLimit: 50,
		},
		{
			name:      "valid cursor",
			query:     url.Values{"before": {validCursor}}.Encode(),
			wantCode:  http.StatusOK,
			wantCalls: 1,
			wantLimit: 50,
		},
		{name: "malformed query", query: "actor=%zz", wantCode: http.StatusBadRequest},
		{name: "zero limit", query: "limit=0", wantCode: http.StatusBadRequest},
		{name: "large limit", query: "limit=201", wantCode: http.StatusBadRequest},
		{name: "negative limit", query: "limit=-1", wantCode: http.StatusBadRequest},
		{name: "non numeric limit", query: "limit=abc", wantCode: http.StatusBadRequest},
		{name: "bad cursor", query: "before=not-base64", wantCode: http.StatusBadRequest},
		{name: "incomplete cursor", query: "before=" + encodeRawCursor(t, "{}"), wantCode: http.StatusBadRequest},
		{name: "bad cursor time", query: "before=" + encodeRawCursor(t, `{"at":"yesterday","id":"`+uuid.NewString()+`"}`), wantCode: http.StatusBadRequest},
		{name: "bad cursor id", query: "before=" + encodeRawCursor(t, `{"at":"2026-10-22T12:00:00Z","id":"nope"}`), wantCode: http.StatusBadRequest},
		{name: "uppercase action", query: "action=Service_*", wantCode: http.StatusBadRequest},
		{name: "action dash", query: "action=service-*", wantCode: http.StatusBadRequest},
		{name: "action two stars", query: "action=service_**", wantCode: http.StatusBadRequest},
		{name: "action too long", query: url.Values{"action": {longAction}}.Encode(), wantCode: http.StatusBadRequest},
		{name: "bad server id", query: "server_id=not-a-uuid", wantCode: http.StatusBadRequest},
		{name: "empty dev server id", query: "server_id=dev:", wantCode: http.StatusBadRequest},
		{name: "actor too long", query: url.Values{"actor": {longActor}}.Encode(), wantCode: http.StatusBadRequest},
		{name: "actor control character", query: url.Values{"actor": {"admin\n"}}.Encode(), wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeAuditReader{}
			recorder := getAudit(t, NewAuditHandler(AuditDeps{Entries: reader}), tt.query, "")
			if recorder.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tt.wantCode, recorder.Body.String())
			}
			if len(reader.calls) != tt.wantCalls {
				t.Fatalf("store calls = %d, want %d", len(reader.calls), tt.wantCalls)
			}
			if tt.wantCalls == 1 && reader.calls[0].Limit != tt.wantLimit {
				t.Errorf("limit = %d, want %d", reader.calls[0].Limit, tt.wantLimit)
			}
			if tt.wantCode == http.StatusBadRequest && errorCodeOf(t, recorder) != "invalid_request" {
				t.Errorf("error code = %q, want invalid_request", errorCodeOf(t, recorder))
			}
		})
	}
}

// encodeRawCursor base64url-encodes a payload the handler should reject.
func encodeRawCursor(t *testing.T, payload string) string {
	t.Helper()

	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func TestAuditHandlerReturnsEntriesAndNext(t *testing.T) {
	serverID := uuid.NewString()
	at := time.Date(2026, 10, 22, 12, 0, 0, 123000000, time.UTC)
	entries := []store.AuditEntry{
		{
			ID:      uuid.NewString(),
			At:      at,
			Actor:   "admin",
			Action:  "service_start",
			IP:      "192.0.2.10",
			Result:  "success",
			Details: map[string]any{"server_id": serverID},
		},
		{
			ID:      uuid.NewString(),
			At:      at.Add(-time.Second),
			Actor:   "cli",
			Action:  "service_stop",
			Result:  "failure",
			Details: nil,
		},
	}
	reader := &fakeAuditReader{
		page: entries,
		more: []store.AuditEntry{{ID: uuid.NewString(), At: at.Add(-2 * time.Second)}},
	}
	query := url.Values{
		"limit":     {"2"},
		"action":    {"service_*"},
		"server_id": {serverID},
		"actor":     {"admin"},
	}.Encode()

	recorder := getAudit(t, NewAuditHandler(AuditDeps{Entries: reader}), query, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}
	var body auditResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(body.Entries))
	}
	if body.Entries[0].ID != entries[0].ID || body.Entries[0].At != at.UTC().Format(time.RFC3339) {
		t.Errorf("first entry = %+v", body.Entries[0])
	}
	if body.Entries[0].Details["server_id"] != serverID {
		t.Errorf("details = %v", body.Entries[0].Details)
	}
	if body.Entries[1].IP != "" || body.Entries[1].Details != nil {
		t.Errorf("second entry = %+v", body.Entries[1])
	}
	if body.Next == "" {
		t.Fatal("next cursor is empty with more entries")
	}
	if strings.ContainsAny(body.Next, "+/=") {
		t.Errorf("next cursor is not base64url: %q", body.Next)
	}
	cursor, err := decodeAuditCursor(body.Next)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	if cursor.ID != entries[1].ID || !cursor.At.Equal(entries[1].At) {
		t.Errorf("cursor = %+v, want the last entry", cursor)
	}

	if len(reader.calls) != 2 {
		t.Fatalf("store calls = %d, want 2", len(reader.calls))
	}
	first := reader.calls[0]
	if first.Before != nil || first.Limit != 2 || first.Action != "service_*" ||
		first.ServerID != serverID || first.Actor != "admin" {
		t.Errorf("first filter = %+v", first)
	}
	lookAhead := reader.calls[1]
	if lookAhead.Before == nil || lookAhead.Before.ID != entries[1].ID ||
		lookAhead.Limit != 1 || lookAhead.Action != "service_*" ||
		lookAhead.ServerID != serverID || lookAhead.Actor != "admin" {
		t.Errorf("look-ahead filter = %+v", lookAhead)
	}
}

func TestAuditHandlerStopsTheCursorAtTheEnd(t *testing.T) {
	reader := &fakeAuditReader{
		page: []store.AuditEntry{
			{ID: uuid.NewString(), At: time.Now(), Action: "login"},
			{ID: uuid.NewString(), At: time.Now().Add(-time.Second), Action: "login"},
		},
	}

	recorder := getAudit(t, NewAuditHandler(AuditDeps{Entries: reader}), "limit=2", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body auditResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Next != "" {
		t.Errorf("next = %q, want empty", body.Next)
	}
	if len(reader.calls) != 2 {
		t.Errorf("store calls = %d, want 2", len(reader.calls))
	}
}

func TestAuditHandlerDoesNotLookAheadOnAShortPage(t *testing.T) {
	reader := &fakeAuditReader{
		page: []store.AuditEntry{{ID: uuid.NewString(), At: time.Now(), Action: "login"}},
	}

	recorder := getAudit(t, NewAuditHandler(AuditDeps{Entries: reader}), "limit=2", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body auditResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Next != "" || len(body.Entries) != 1 {
		t.Errorf("body = %+v", body)
	}
	if len(reader.calls) != 1 {
		t.Errorf("store calls = %d, want 1", len(reader.calls))
	}
}

func TestAuditHandlerReportsReadErrors(t *testing.T) {
	reader := &fakeAuditReader{err: errors.New("database unavailable")}

	recorder := getAudit(t, NewAuditHandler(AuditDeps{Entries: reader}), "", "")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if code := errorCodeOf(t, recorder); code != "internal" {
		t.Errorf("code = %q, want internal", code)
	}
}

func TestAuditHandlerWithoutAStore(t *testing.T) {
	recorder := getAudit(t, NewAuditHandler(AuditDeps{}), "", "")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if code := errorCodeOf(t, recorder); code != "internal" {
		t.Errorf("code = %q, want internal", code)
	}
}

func TestAuditEndpointRequiresAFullSession(t *testing.T) {
	fullStore, fullClock, _, fullToken := newSessionFixture(t, store.StageFull)
	reader := &fakeAuditReader{}
	full := Sessions(SessionDeps{Store: fullStore, Now: fullClock.Now})(RequireFull(
		NewAuditHandler(AuditDeps{Entries: reader})))

	recorder := getAudit(t, full, "", "")
	if recorder.Code != http.StatusUnauthorized || errorCodeOf(t, recorder) != "session_required" {
		t.Errorf("anonymous = %d %q, want 401 session_required", recorder.Code, errorCodeOf(t, recorder))
	}

	pendingStore, pendingClock, _, pendingToken := newSessionFixture(t, store.StageMFAPending)
	pending := Sessions(SessionDeps{Store: pendingStore, Now: pendingClock.Now})(RequireFull(
		NewAuditHandler(AuditDeps{Entries: reader})))
	recorder = getAudit(t, pending, "", pendingToken)
	if recorder.Code != http.StatusUnauthorized || errorCodeOf(t, recorder) != "mfa_required" {
		t.Errorf("pending = %d %q, want 401 mfa_required", recorder.Code, errorCodeOf(t, recorder))
	}

	recorder = getAudit(t, full, "", fullToken)
	if recorder.Code != http.StatusOK {
		t.Errorf("full = %d, want 200", recorder.Code)
	}
}

func TestAuditHandlerExposesActorIDAndScope(t *testing.T) {
	reader := &fakeAuditReader{page: []store.AuditEntry{{
		ID: "entry-1", At: time.Now(), Actor: "admin", ActorID: "user-1",
		Action: "authz_denied", Result: "denied",
		Details: map[string]any{"permission": "host.reboot"},
		Scope:   map[string]any{"type": "server", "server_id": "srv-1"},
	}}}
	recorder := getAudit(t, NewAuditHandler(AuditDeps{Entries: reader}), "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body auditResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Entries) != 1 {
		t.Fatalf("entries = %+v", body.Entries)
	}
	entry := body.Entries[0]
	if entry.ActorID != "user-1" || entry.Scope["type"] != "server" || entry.Scope["server_id"] != "srv-1" {
		t.Fatalf("entry = %+v", entry)
	}
}
