package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

type noteSetCall struct {
	serverID, kind, name, note, actor string
}

type noteDeleteCall struct {
	serverID, kind, name string
}

type fakeNotesStore struct {
	server      store.Server
	serverErr   error
	notes       map[string][]store.ResourceNote
	listErr     error
	setErr      error
	deleteErr   error
	setCalls    []noteSetCall
	deleteCalls []noteDeleteCall
}

func (f *fakeNotesStore) GetServer(_ context.Context, id string) (store.Server, error) {
	if f.serverErr != nil {
		return store.Server{}, f.serverErr
	}
	if f.server.ID == "" {
		return store.Server{ID: id}, nil
	}
	return f.server, nil
}

func (f *fakeNotesStore) ListNotes(_ context.Context, _ string, kind string) ([]store.ResourceNote, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.notes[kind], nil
}

func (f *fakeNotesStore) SetNote(_ context.Context, serverID, kind, name, note, actor string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.setCalls = append(f.setCalls, noteSetCall{serverID, kind, name, note, actor})
	return nil
}

func (f *fakeNotesStore) DeleteNote(_ context.Context, serverID, kind, name string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleteCalls = append(f.deleteCalls, noteDeleteCall{serverID, kind, name})
	return nil
}

func notesRequest(handler func(http.ResponseWriter, *http.Request), method, name, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/api/v1/servers/s1/notes", strings.NewReader(body))
	route := chi.NewRouteContext()
	route.URLParams.Add("serverID", "s1")
	if name != "" {
		route.URLParams.Add("name", name)
	}
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	request = request.WithContext(withSession(request.Context(), Session{
		User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull,
	}))
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func TestNotesHandlersListAndSet(t *testing.T) {
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		kind       string
		service    bool
		pathName   string
		storedName string
	}{
		{"container", noteKindContainer, false, "web", "web"},
		{"stack", noteKindStack, false, "shop", "shop"},
		{"service", noteKindService, true, "nginx", "nginx.service"},
		{"service with suffix", noteKindService, true, "redis.timer", "redis.timer"},
		{"service with another dot", noteKindService, true, "foo.bar", "foo.bar.service"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := &fakeNotesStore{notes: map[string][]store.ResourceNote{
				test.kind: {{Name: test.storedName, Note: "existing", UpdatedBy: "bob", UpdatedAt: at}},
			}}
			audit := &fakeAudit{}
			handler := newNotesHandler(NotesDeps{Store: st, Audit: audit}, test.kind, test.service)

			recorder := notesRequest(handler.listNotes, http.MethodGet, "", "")
			if recorder.Code != http.StatusOK {
				t.Fatalf("list status = %d", recorder.Code)
			}
			var listed notesResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
				t.Fatal(err)
			}
			if len(listed.Notes) != 1 || listed.Notes[0].Name != test.storedName ||
				listed.Notes[0].Note != "existing" || listed.Notes[0].UpdatedBy != "bob" {
				t.Fatalf("notes = %+v", listed.Notes)
			}

			recorder = notesRequest(handler.setNote, http.MethodPut, test.pathName, `{"note":"  keep me  "}`)
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("set status = %d, body %s", recorder.Code, recorder.Body)
			}
			if len(st.setCalls) != 1 {
				t.Fatalf("set calls = %+v", st.setCalls)
			}
			call := st.setCalls[0]
			if call.serverID != "s1" || call.kind != test.kind || call.name != test.storedName ||
				call.note != "keep me" || call.actor != "alice" {
				t.Fatalf("set call = %+v", call)
			}
			if len(audit.events) != 1 || audit.events[0].Action != "note_update" ||
				audit.events[0].Actor != "alice" ||
				audit.events[0].Details["kind"] != test.kind || audit.events[0].Details["name"] != test.storedName {
				t.Fatalf("audit = %+v", audit.events)
			}
		})
	}
}

func TestNotesHandlersDeleteWithAnEmptyNote(t *testing.T) {
	st := &fakeNotesStore{}
	audit := &fakeAudit{}
	handler := newNotesHandler(NotesDeps{Store: st, Audit: audit}, noteKindContainer, false)

	recorder := notesRequest(handler.setNote, http.MethodPut, "web", `{"note":"   "}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", recorder.Code)
	}
	if len(st.setCalls) != 0 || len(st.deleteCalls) != 1 {
		t.Fatalf("set = %+v, delete = %+v", st.setCalls, st.deleteCalls)
	}
	if call := st.deleteCalls[0]; call.kind != noteKindContainer || call.name != "web" {
		t.Fatalf("delete call = %+v", call)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "note_delete" {
		t.Fatalf("audit = %+v", audit.events)
	}
}

func TestNotesHandlersEnforceTheLengthLimitInRunes(t *testing.T) {
	for _, test := range []struct {
		name   string
		length int
		status int
	}{
		{"500 runes", 500, http.StatusNoContent},
		{"501 runes", 501, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			st := &fakeNotesStore{}
			handler := newNotesHandler(NotesDeps{Store: st}, noteKindContainer, false)
			body := `{"note":"` + strings.Repeat("á", test.length) + `"}`
			recorder := notesRequest(handler.setNote, http.MethodPut, "web", body)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
			if test.length == 500 && (len(st.setCalls) != 1 || len([]rune(st.setCalls[0].note)) != 500) {
				t.Fatalf("set calls = %+v", st.setCalls)
			}
		})
	}
}

func TestNotesHandlersRejectInvalidServersAndNames(t *testing.T) {
	t.Run("unknown server", func(t *testing.T) {
		st := &fakeNotesStore{serverErr: store.ErrNotFound}
		handler := newNotesHandler(NotesDeps{Store: st}, noteKindStack, false)
		if recorder := notesRequest(handler.listNotes, http.MethodGet, "", ""); recorder.Code != http.StatusNotFound {
			t.Fatalf("unknown server list = %d, want 404", recorder.Code)
		}
		if recorder := notesRequest(handler.setNote, http.MethodPut, "shop", `{"note":"x"}`); recorder.Code != http.StatusNotFound {
			t.Fatalf("unknown server set = %d, want 404", recorder.Code)
		}
	})

	t.Run("removed server", func(t *testing.T) {
		st := &fakeNotesStore{server: store.Server{ID: "s1", RemovedAt: time.Now()}}
		handler := newNotesHandler(NotesDeps{Store: st}, noteKindStack, false)
		if recorder := notesRequest(handler.listNotes, http.MethodGet, "", ""); recorder.Code != http.StatusNotFound {
			t.Fatalf("removed server = %d, want 404", recorder.Code)
		}
	})

	for _, name := range []string{"", strings.Repeat("a", 256), "bad/name", "bad\nname"} {
		t.Run("invalid name "+name, func(t *testing.T) {
			st := &fakeNotesStore{}
			handler := newNotesHandler(NotesDeps{Store: st}, noteKindContainer, false)
			if recorder := notesRequest(handler.setNote, http.MethodPut, name, `{"note":"x"}`); recorder.Code != http.StatusBadRequest {
				t.Fatalf("name %q = %d, want 400", name, recorder.Code)
			}
			if len(st.setCalls) != 0 {
				t.Fatalf("invalid name reached the store: %+v", st.setCalls)
			}
		})
	}
}

func TestNotesHandlersWrapStoreErrors(t *testing.T) {
	st := &fakeNotesStore{listErr: errors.New("down")}
	recorder := notesRequest(newNotesHandler(NotesDeps{Store: st}, noteKindContainer, false).listNotes,
		http.MethodGet, "", "")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("list error = %d, want 500", recorder.Code)
	}
}

func TestNotesRoutePermissions(t *testing.T) {
	session := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}
	tests := []struct {
		name       string
		role       rbac.Role
		method     string
		pattern    string
		wantStatus int
	}{
		{"viewer lists containers", rbac.RoleViewer, http.MethodGet, "/servers/{serverID}/notes/containers", http.StatusNoContent},
		{"viewer cannot write containers", rbac.RoleViewer, http.MethodPut, "/servers/{serverID}/notes/containers/{name}", http.StatusForbidden},
		{"viewer lists stacks", rbac.RoleViewer, http.MethodGet, "/servers/{serverID}/notes/stacks", http.StatusNoContent},
		{"viewer cannot write stacks", rbac.RoleViewer, http.MethodPut, "/servers/{serverID}/notes/stacks/{name}", http.StatusForbidden},
		{"viewer lists services", rbac.RoleViewer, http.MethodGet, "/servers/{serverID}/notes/services", http.StatusNoContent},
		{"viewer cannot write services", rbac.RoleViewer, http.MethodPut, "/servers/{serverID}/notes/services/{name}", http.StatusForbidden},
		{"app operator writes containers", rbac.RoleAppOperator, http.MethodPut, "/servers/{serverID}/notes/containers/{name}", http.StatusNoContent},
		{"app operator writes stacks", rbac.RoleAppOperator, http.MethodPut, "/servers/{serverID}/notes/stacks/{name}", http.StatusNoContent},
		{"app operator cannot write services", rbac.RoleAppOperator, http.MethodPut, "/servers/{serverID}/notes/services/{name}", http.StatusForbidden},
		{"server operator writes services", rbac.RoleServerOperator, http.MethodPut, "/servers/{serverID}/notes/services/{name}", http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := NewAuthorizer(AuthorizerDeps{
				Bindings: stubBindings{byUser: map[string][]store.RoleBinding{"u1": {{
					SubjectType: store.SubjectUser, SubjectID: "u1",
					ScopeType: store.ScopeOrganization, Role: string(test.role),
				}}}},
				Servers: authzServers(),
				Audit:   &fakeAudit{},
			})
			handler := routeTestRouter(t, authorizer)
			if got := doAuthorized(t, handler, authzRequest(test.method, routePath(test.method, test.pattern), session)); got != test.wantStatus {
				t.Fatalf("status = %d, want %d", got, test.wantStatus)
			}
		})
	}
}

func TestNotesAPIKeyCanReadButNotWrite(t *testing.T) {
	authorizer := NewAuthorizer(AuthorizerDeps{
		Bindings: stubBindings{
			byUser: map[string][]store.RoleBinding{"u1": {{
				SubjectType: store.SubjectUser, SubjectID: "u1",
				ScopeType: store.ScopeOrganization, Role: string(rbac.RoleAdmin),
			}}},
			bySubject: map[string][]store.RoleBinding{"api_key/key1": {{
				SubjectType: store.SubjectAPIKey, SubjectID: "key1",
				ScopeType: store.ScopeOrganization, Role: string(rbac.RoleAdmin),
			}}},
		},
		Servers: authzServers(),
		Audit:   &fakeAudit{},
	})
	handler := routeTestRouter(t, authorizer)
	session := Session{
		User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull,
		AuthKind: AuthKindAPIKey, APIKeyID: "key1",
	}
	if got := doAuthorized(t, handler, authzRequest(http.MethodGet, "/api/v1/servers/s1/notes/containers", session)); got != http.StatusNoContent {
		t.Fatalf("API key GET = %d, want 204", got)
	}
	if got := doAuthorized(t, handler, authzRequest(http.MethodPut, "/api/v1/servers/s1/notes/containers/web", session)); got != http.StatusForbidden {
		t.Fatalf("API key PUT = %d, want 403", got)
	}
}
