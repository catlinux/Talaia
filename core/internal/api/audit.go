package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/catlinux/Talaia/core/internal/store"
)

// AuditReader reads pages of the audit log.
type AuditReader interface {
	ListAuditPage(ctx context.Context, filter store.AuditFilter) ([]store.AuditEntry, error)
}

// AuditDeps are the collaborators of the audit endpoint.
type AuditDeps struct {
	Entries AuditReader
}

// auditActionFilter is the shape of an action filter: a lower-case action name,
// optionally followed by "*" to match a prefix.
var auditActionFilter = regexp.MustCompile(`^[a-z0-9_]{1,64}[*]?$`)

// auditCursorPayload is the opaque cursor the API hands back in "next". It is
// base64url-encoded JSON; clients must treat it as opaque.
type auditCursorPayload struct {
	At string `json:"at"`
	ID string `json:"id"`
}

type auditEntryResponse struct {
	ID      string         `json:"id"`
	At      string         `json:"at"`
	Actor   string         `json:"actor"`
	ActorID string         `json:"actor_id,omitempty"`
	Action  string         `json:"action"`
	IP      string         `json:"ip"`
	Result  string         `json:"result"`
	Details map[string]any `json:"details"`
	Scope   map[string]any `json:"scope,omitempty"`
}

type auditResponse struct {
	Entries []auditEntryResponse `json:"entries"`
	Next    string               `json:"next"`
}

// NewAuditHandler returns GET /api/v1/audit. It is mounted behind the session
// middleware and RequireFull, so only a full session reaches it. Reading the
// audit log is not audited: that would flood the log with reads.
func NewAuditHandler(deps AuditDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Entries == nil {
			slog.Error("read the audit log without a store")
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}

		filter, err := parseAuditFilter(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}

		entries, err := deps.Entries.ListAuditPage(r.Context(), filter)
		if err != nil {
			slog.Error("list audit events", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if len(entries) > filter.Limit {
			entries = entries[:filter.Limit]
		}

		response := auditResponse{
			Entries: make([]auditEntryResponse, 0, len(entries)),
			Next:    "",
		}
		for _, entry := range entries {
			response.Entries = append(response.Entries, auditEntryResponse{
				ID:      entry.ID,
				At:      entry.At.UTC().Format(time.RFC3339),
				Actor:   entry.Actor,
				ActorID: entry.ActorID,
				Action:  entry.Action,
				IP:      entry.IP,
				Result:  entry.Result,
				Details: entry.Details,
				Scope:   entry.Scope,
			})
		}

		// A full page may be the last one. Ask for one more row so an exact
		// multiple of the page size does not hand back a cursor to an empty page.
		if len(entries) == filter.Limit {
			last := entries[len(entries)-1]
			cursor := store.AuditCursor{At: last.At, ID: last.ID}
			more, err := deps.Entries.ListAuditPage(r.Context(), store.AuditFilter{
				Before:   &cursor,
				Limit:    1,
				Action:   filter.Action,
				ServerID: filter.ServerID,
				Actor:    filter.Actor,
			})
			if err != nil {
				slog.Error("check the next audit page", "error", err)
				writeError(w, http.StatusInternalServerError, "internal")
				return
			}
			if len(more) > 0 {
				next, err := encodeAuditCursor(cursor)
				if err != nil {
					slog.Error("encode the audit cursor", "error", err)
					writeError(w, http.StatusInternalServerError, "internal")
					return
				}
				response.Next = next
			}
		}

		writeJSON(w, http.StatusOK, response)
	})
}

// parseAuditFilter validates the query string and turns it into a store filter.
func parseAuditFilter(r *http.Request) (store.AuditFilter, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return store.AuditFilter{}, fmt.Errorf("parse audit query: %w", err)
	}

	limit, err := parseAuditLimit(query.Get("limit"))
	if err != nil {
		return store.AuditFilter{}, err
	}
	filter := store.AuditFilter{Limit: limit}

	if raw := query.Get("before"); raw != "" {
		cursor, err := decodeAuditCursor(raw)
		if err != nil {
			return store.AuditFilter{}, err
		}
		filter.Before = &cursor
	}
	if action := query.Get("action"); action != "" {
		if !auditActionFilter.MatchString(action) {
			return store.AuditFilter{}, fmt.Errorf("invalid action filter")
		}
		filter.Action = action
	}
	if serverID := query.Get("server_id"); serverID != "" {
		canonical, ok := canonicalAuditServerID(serverID)
		if !ok {
			return store.AuditFilter{}, fmt.Errorf("invalid server id filter")
		}
		filter.ServerID = canonical
	}
	if actor := query.Get("actor"); actor != "" {
		if !validAuditActor(actor) {
			return store.AuditFilter{}, fmt.Errorf("invalid actor filter")
		}
		filter.Actor = actor
	}
	return filter, nil
}

// parseAuditLimit reads the optional page size. The default is 50.
func parseAuditLimit(raw string) (int, error) {
	if raw == "" {
		return store.DefaultAuditLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > store.MaxAuditLimit {
		return 0, fmt.Errorf("invalid audit limit")
	}
	return limit, nil
}

// canonicalAuditServerID accepts a UUID or the dev: entries that only exist in
// insecure development mode.
func canonicalAuditServerID(value string) (string, bool) {
	if parsed, err := uuid.Parse(value); err == nil {
		return parsed.String(), true
	}
	if !strings.HasPrefix(value, devServerPrefix) {
		return "", false
	}
	hostname := strings.TrimPrefix(value, devServerPrefix)
	if hostname == "" || len(hostname) > 253 {
		return "", false
	}
	for _, r := range hostname {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", false
		}
	}
	return value, true
}

// validAuditActor accepts an exact actor filter of at most 64 characters
// without control characters.
func validAuditActor(value string) bool {
	if utf8.RuneCountInString(value) > 64 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// encodeAuditCursor turns the last row of a page into an opaque base64url
// cursor.
func encodeAuditCursor(cursor store.AuditCursor) (string, error) {
	payload, err := json.Marshal(auditCursorPayload{
		At: cursor.At.UTC().Format(time.RFC3339Nano),
		ID: cursor.ID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal audit cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// decodeAuditCursor reads and validates the opaque cursor of "before".
func decodeAuditCursor(value string) (store.AuditCursor, error) {
	if len(value) > 1024 {
		return store.AuditCursor{}, fmt.Errorf("audit cursor is too long")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(value)
	}
	if err != nil {
		return store.AuditCursor{}, fmt.Errorf("decode audit cursor: %w", err)
	}

	var payload auditCursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return store.AuditCursor{}, fmt.Errorf("decode audit cursor: %w", err)
	}
	if payload.At == "" || payload.ID == "" {
		return store.AuditCursor{}, fmt.Errorf("audit cursor is incomplete")
	}
	at, err := time.Parse(time.RFC3339Nano, payload.At)
	if err != nil {
		return store.AuditCursor{}, fmt.Errorf("decode audit cursor time: %w", err)
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return store.AuditCursor{}, fmt.Errorf("decode audit cursor id: %w", err)
	}
	return store.AuditCursor{At: at, ID: id.String()}, nil
}
