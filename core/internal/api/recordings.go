package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/terminal"
)

const (
	defaultRecordingLimit = 50
	maxRecordingLimit     = 200
)

// RecordingStore is the part of the store used by the recordings API.
type RecordingStore interface {
	ListTerminalSessions(ctx context.Context, limit int) ([]store.TerminalSession, error)
	GetTerminalSession(ctx context.Context, id string) (store.TerminalSession, error)
	TerminalChunks(ctx context.Context, id string) ([]store.TerminalChunk, error)
}

// RecordingDeps are the collaborators of the recordings API.
type RecordingDeps struct {
	Recordings RecordingStore
	Opener     terminal.Opener
	Servers    ServerLister
	Audit      Auditor
}

type recordingEntry struct {
	ID              string     `json:"id"`
	ServerID        string     `json:"server_id"`
	ServerName      string     `json:"server_name"`
	Container       string     `json:"container"`
	Kind            string     `json:"kind"`
	Shell           string     `json:"shell"`
	User            string     `json:"user"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
	DurationSeconds int64      `json:"duration_seconds"`
	CloseReason     string     `json:"close_reason"`
	Privileged      bool       `json:"privileged"`
	Bytes           int64      `json:"bytes"`
}

func parseRecordingLimit(raw string) (int, error) {
	if raw == "" {
		return defaultRecordingLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxRecordingLimit {
		return 0, fmt.Errorf("invalid recording limit")
	}
	return limit, nil
}

// NewRecordingListHandler returns GET /api/v1/recordings. It exposes metadata
// only: the recording itself is downloaded through the other endpoint.
func NewRecordingListHandler(deps RecordingDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Recordings == nil {
			internal(w, "list recordings without a store", errors.New("nil recording store"))
			return
		}
		limit, err := parseRecordingLimit(r.URL.Query().Get("limit"))
		if err != nil {
			writeInvalid(w, "limit", "out_of_range")
			return
		}
		sessions, err := deps.Recordings.ListTerminalSessions(r.Context(), limit)
		if err != nil {
			slog.Error("list terminal sessions", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}

		serverNames := map[string]string{}
		if deps.Servers != nil {
			servers, err := deps.Servers.ListServers(r.Context())
			if err != nil {
				slog.Error("list servers for recordings", "error", err)
				writeError(w, http.StatusInternalServerError, "internal")
				return
			}
			for _, server := range servers {
				serverNames[server.ID] = server.Name
			}
		}

		entries := make([]recordingEntry, 0, len(sessions))
		for _, session := range sessions {
			entries = append(entries, recordingEntryOf(session, serverNames[session.ServerID]))
		}
		writeJSON(w, http.StatusOK, entries)
	})
}

func recordingEntryOf(session store.TerminalSession, serverName string) recordingEntry {
	entry := recordingEntry{
		ID: session.ID, ServerID: session.ServerID, ServerName: serverName,
		Shell: session.Shell, User: session.CreatedBy, StartedAt: session.OpenedAt,
		CloseReason: session.CloseReason, Privileged: session.Privileged,
		Bytes: session.RecordedBytes, Kind: "container", Container: session.Container,
	}
	if session.Container == terminal.HostContainer {
		entry.Kind = "host"
		entry.Container = ""
	}
	if !session.ClosedAt.IsZero() {
		ended := session.ClosedAt
		entry.EndedAt = &ended
		if duration := ended.Sub(session.OpenedAt); duration > 0 {
			entry.DurationSeconds = int64(duration / time.Second)
		}
	}
	return entry
}

// NewRecordingDownloadHandler returns GET /api/v1/recordings/{sessionID}/download.
// The asciicast is built in memory before any header or byte reaches the client,
// so a corrupt recording is still a clean 500 instead of a truncated download.
func NewRecordingDownloadHandler(deps RecordingDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Recordings == nil || deps.Opener == nil {
			internal(w, "download a recording without its dependencies", errors.New("missing recording dependencies"))
			return
		}
		id := chi.URLParam(r, "sessionID")
		if _, err := uuid.Parse(id); err != nil {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}

		session, err := deps.Recordings.GetTerminalSession(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			slog.Error("get terminal session to download", "session_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if session.Open() {
			writeError(w, http.StatusConflict, "session_open")
			return
		}

		var cast bytes.Buffer
		if err := terminal.Export(r.Context(), deps.Recordings, deps.Opener, id, &cast); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			slog.Error("export terminal recording", "session_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "recording_corrupt")
			return
		}

		auditRecordingDownload(r, deps.Audit, id)
		w.Header().Set("Content-Type", "application/x-asciicast")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "talaia-"+id+".cast"))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(cast.Len()))
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(cast.Bytes()); err != nil {
			slog.Warn("write terminal recording", "session_id", id, "error", err)
		}
	})
}

func auditRecordingDownload(r *http.Request, audit Auditor, id string) {
	if audit == nil {
		return
	}
	session, _ := sessionFromContext(r.Context())
	event := store.AuditEvent{
		Actor:   session.User.Username,
		ActorID: session.User.ID,
		Action:  "recording_download",
		IP:      clientIP(r),
		Result:  "success",
		Details: map[string]any{"session_id": id},
		Scope:   map[string]any{"type": "terminal_session", "session_id": id},
	}
	if err := audit.AppendAudit(r.Context(), event); err != nil {
		slog.Error("append recording download audit", "session_id", id, "error", err)
	}
}
