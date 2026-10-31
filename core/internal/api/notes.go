package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/store"
)

// Note kinds are the three resource families with their own permission.
const (
	noteKindContainer = "container"
	noteKindStack     = "stack"
	noteKindService   = "service"
)

const maxNoteRunes = 500

// serviceSuffixes are the systemd unit suffixes accepted without appending .service.
var serviceSuffixes = []string{
	".service", ".socket", ".timer", ".path", ".mount", ".automount",
	".swap", ".target", ".slice", ".scope", ".device",
}

// NotesStore is the part of the store used by the notes endpoints.
type NotesStore interface {
	GetServer(ctx context.Context, id string) (store.Server, error)
	ListNotes(ctx context.Context, serverID, kind string) ([]store.ResourceNote, error)
	SetNote(ctx context.Context, serverID, kind, name, note, actor string) error
	DeleteNote(ctx context.Context, serverID, kind, name string) error
}

// NotesDeps are the collaborators of the resource notes endpoints.
type NotesDeps struct {
	Store NotesStore
	Audit Auditor
}

type notesHandler struct {
	deps    NotesDeps
	kind    string
	service bool
}

type noteView struct {
	Name      string    `json:"name"`
	Note      string    `json:"note"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

type notesResponse struct {
	Notes []noteView `json:"notes"`
}

type noteRequest struct {
	Note string `json:"note"`
}

func newNotesHandler(deps NotesDeps, kind string, service bool) *notesHandler {
	return &notesHandler{deps: deps, kind: kind, service: service}
}

func (h *notesHandler) listNotes(w http.ResponseWriter, r *http.Request) {
	serverID, ok := h.serverOr404(w, r)
	if !ok {
		return
	}
	notes, err := h.deps.Store.ListNotes(r.Context(), serverID, h.kind)
	if err != nil {
		internal(w, "list resource notes", err)
		return
	}
	response := notesResponse{Notes: make([]noteView, 0, len(notes))}
	for _, note := range notes {
		response.Notes = append(response.Notes, noteView{
			Name: note.Name, Note: note.Note, UpdatedBy: note.UpdatedBy, UpdatedAt: note.UpdatedAt,
		})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, response)
}

func (h *notesHandler) setNote(w http.ResponseWriter, r *http.Request) {
	serverID, ok := h.serverOr404(w, r)
	if !ok {
		return
	}
	name, err := url.PathUnescape(chi.URLParam(r, "name"))
	if err != nil {
		writeInvalid(w, "name", "invalid_value")
		return
	}
	if h.service && !hasServiceSuffix(name) {
		name += ".service"
	}
	if !validNoteName(name) {
		writeInvalid(w, "name", "invalid_value")
		return
	}
	var request noteRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalid(w, "", "body")
		return
	}
	note := strings.TrimSpace(request.Note)
	if utf8.RuneCountInString(note) > maxNoteRunes {
		writeInvalid(w, "note", "out_of_range")
		return
	}
	actor := sessionUsername(r)
	if note == "" {
		if err := h.deps.Store.DeleteNote(r.Context(), serverID, h.kind, name); err != nil {
			internal(w, "delete resource note", err)
			return
		}
		h.audit(r, "note_delete", serverID, name)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.deps.Store.SetNote(r.Context(), serverID, h.kind, name, note, actor); err != nil {
		internal(w, "set resource note", err)
		return
	}
	h.audit(r, "note_update", serverID, name)
	w.WriteHeader(http.StatusNoContent)
}

func (h *notesHandler) serverOr404(w http.ResponseWriter, r *http.Request) (string, bool) {
	serverID := chi.URLParam(r, "serverID")
	server, err := h.deps.Store.GetServer(r.Context(), serverID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return "", false
		}
		internal(w, "get server for resource notes", err)
		return "", false
	}
	if !server.RemovedAt.IsZero() {
		writeError(w, http.StatusNotFound, "not_found")
		return "", false
	}
	return serverID, true
}

func (h *notesHandler) audit(r *http.Request, action, serverID, name string) {
	if h.deps.Audit == nil {
		return
	}
	session, _ := sessionFromContext(r.Context())
	event := store.AuditEvent{
		Actor:   session.User.Username,
		ActorID: session.User.ID,
		Action:  action,
		IP:      clientIP(r),
		Result:  "success",
		Scope:   map[string]any{"type": "server", "server_id": serverID},
		Details: map[string]any{"kind": h.kind, "name": name},
	}
	_ = h.deps.Audit.AppendAudit(r.Context(), event)
}

func validNoteName(name string) bool {
	count := utf8.RuneCountInString(name)
	if count < 1 || count > 255 {
		return false
	}
	for _, char := range name {
		if char == '/' || unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func hasServiceSuffix(name string) bool {
	for _, suffix := range serviceSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
