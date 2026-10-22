package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/hub"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func journalPath(serverID string) string {
	return "/api/v1/servers/" + serverID + "/journal"
}

// newJournalPanel creates the signed-in panel, one enrolled server and a
// connected agent. The agent has no services and no containers: the journal
// endpoint must not require an inventory entry.
func newJournalPanel(t *testing.T) (*testPanel, string) {
	t.Helper()

	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(hub.AgentInfo{ServerID: serverID, Hostname: "vps-journal"})
	return panel, serverID
}

func TestJournalCreatesAJobAndSendsTheCommand(t *testing.T) {
	panel, serverID := newJournalPanel(t)
	search := "VerySecretNeedle.*"
	body := map[string]any{
		"unit":         "docker.service",
		"max_priority": 3,
		"since":        "2026-10-20T10:00:00Z",
		"until":        "2026-10-20T11:00:00Z",
		"search":       search,
		"boot":         "current",
		"tail":         50,
		"after_cursor": "s=abc;def",
	}

	response := panel.post(t, journalPath(serverID), body)
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
	journal := command.GetReadJournal()
	if journal == nil {
		t.Fatal("the command does not carry a journal request")
	}
	if command.GetJobId() != created.JobID || panel.dispatcher.serverID != serverID {
		t.Errorf("command = %v for %s", command, panel.dispatcher.serverID)
	}
	if journal.GetUnit() != "docker.service" ||
		journal.MaxPriority == nil || journal.GetMaxPriority() != 3 ||
		journal.GetSince() != "2026-10-20T10:00:00Z" ||
		journal.GetUntil() != "2026-10-20T11:00:00Z" ||
		journal.GetSearch() != search ||
		journal.GetBoot() != agentv1.JournalBoot_JOURNAL_BOOT_CURRENT ||
		journal.GetTail() != 50 ||
		journal.GetAfterCursor() != "s=abc;def" {
		t.Errorf("journal command = %v", journal)
	}

	var job jobResponse
	decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
	if job.Kind != journalReadKind || job.Params["unit"] != "docker.service" || job.Params["boot"] != "current" {
		t.Errorf("job = %+v", job)
	}
	if _, stored := job.Params["search"]; stored {
		t.Error("the search text was stored in the job parameters")
	}

	// The agent's answer is the job output. It must never reach the audit log.
	recorder := JobRecorder{Jobs: panel.store}
	recorder.RecordJobEvent(context.Background(), serverID, &agentv1.JobEvent{JobId: created.JobID, State: agentv1.JobState_JOB_STATE_RUNNING})
	journalContent := "journal-secret-line"
	recorder.RecordJobEvent(context.Background(), serverID, &agentv1.JobEvent{
		JobId: created.JobID, State: agentv1.JobState_JOB_STATE_SUCCEEDED,
		Output: `{"entries":[{"message":"` + journalContent + `"}],"cursor":""}`,
	})

	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != "journal_read" || entry.Actor != "admin" {
			continue
		}
		found = true
		if entry.Details["server_id"] != serverID || entry.Details["job_id"] != created.JobID ||
			entry.Details["unit"] != "docker.service" || entry.Details["boot"] != "current" {
			t.Errorf("audit details = %v", entry.Details)
		}
		if _, stored := entry.Details["search"]; stored {
			t.Error("the audit log holds the search text")
		}
		details, err := json.Marshal(entry.Details)
		if err != nil {
			t.Fatalf("marshal audit details: %v", err)
		}
		if strings.Contains(string(details), search) || strings.Contains(string(details), journalContent) {
			t.Errorf("the audit log holds journal or search text: %s", details)
		}
	}
	if !found {
		t.Error("the journal read was not audited")
	}
}

func TestJournalSendsEachValidField(t *testing.T) {
	panel, serverID := newJournalPanel(t)

	tests := []struct {
		name  string
		body  map[string]any
		check func(t *testing.T, journal *agentv1.ReadJournal)
	}{
		{
			name: "defaults",
			body: map[string]any{},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetUnit() != "" || journal.MaxPriority != nil ||
					journal.GetSince() != "" || journal.GetUntil() != "" ||
					journal.GetSearch() != "" || journal.GetBoot() != agentv1.JournalBoot_JOURNAL_BOOT_UNSPECIFIED ||
					journal.GetTail() != 0 || journal.GetAfterCursor() != "" {
					t.Errorf("defaults = %v", journal)
				}
			},
		},
		{
			name: "removed unit is accepted",
			body: map[string]any{"unit": "ghost.service"},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetUnit() != "ghost.service" {
					t.Errorf("unit = %q", journal.GetUnit())
				}
			},
		},
		{
			name: "socket unit",
			body: map[string]any{"unit": "docker.socket"},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetUnit() != "docker.socket" {
					t.Errorf("unit = %q", journal.GetUnit())
				}
			},
		},
		{
			name: "timer unit",
			body: map[string]any{"unit": "logrotate.timer"},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetUnit() != "logrotate.timer" {
					t.Errorf("unit = %q", journal.GetUnit())
				}
			},
		},
		{
			name: "unit maximum length",
			body: map[string]any{"unit": strings.Repeat("a", 250) + ".service"},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetUnit() != strings.Repeat("a", 250)+".service" {
					t.Errorf("unit = %q", journal.GetUnit())
				}
			},
		},
		{
			name: "max priority absent",
			body: map[string]any{"unit": "nginx.service"},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.MaxPriority != nil {
					t.Errorf("max priority = %d, want absent", journal.GetMaxPriority())
				}
			},
		},
		{
			name: "max priority zero",
			body: map[string]any{"max_priority": 0},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.MaxPriority == nil || journal.GetMaxPriority() != 0 {
					t.Errorf("max priority = %v, want 0", journal.MaxPriority)
				}
			},
		},
		{
			name: "max priority seven",
			body: map[string]any{"max_priority": 7},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.MaxPriority == nil || journal.GetMaxPriority() != 7 {
					t.Errorf("max priority = %v, want 7", journal.MaxPriority)
				}
			},
		},
		{
			name: "boot all",
			body: map[string]any{"boot": "all"},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetBoot() != agentv1.JournalBoot_JOURNAL_BOOT_UNSPECIFIED {
					t.Errorf("boot = %s", journal.GetBoot())
				}
			},
		},
		{
			name: "boot previous",
			body: map[string]any{"boot": "previous"},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetBoot() != agentv1.JournalBoot_JOURNAL_BOOT_PREVIOUS {
					t.Errorf("boot = %s", journal.GetBoot())
				}
			},
		},
		{
			name: "tail maximum",
			body: map[string]any{"tail": maxJournalTail},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetTail() != maxJournalTail {
					t.Errorf("tail = %d", journal.GetTail())
				}
			},
		},
		{
			name: "after cursor",
			body: map[string]any{"after_cursor": "s=abc;def_-123"},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetAfterCursor() != "s=abc;def_-123" {
					t.Errorf("cursor = %q", journal.GetAfterCursor())
				}
			},
		},
		{
			name: "after cursor maximum length",
			body: map[string]any{"after_cursor": strings.Repeat("a", 512)},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetAfterCursor() != strings.Repeat("a", 512) {
					t.Errorf("cursor = %q", journal.GetAfterCursor())
				}
			},
		},
		{
			name: "search",
			body: map[string]any{"search": "a.* literal b"},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetSearch() != "a.* literal b" {
					t.Errorf("search = %q", journal.GetSearch())
				}
			},
		},
		{
			name: "search maximum length",
			body: map[string]any{"search": strings.Repeat("x", maxJournalSearch)},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetSearch() != strings.Repeat("x", maxJournalSearch) {
					t.Errorf("search length = %d", len(journal.GetSearch()))
				}
			},
		},
		{
			name: "times are trimmed",
			body: map[string]any{
				"since": " 2026-10-20T10:00:00.500Z ",
				"until": " 2026-10-20T11:00:00Z ",
			},
			check: func(t *testing.T, journal *agentv1.ReadJournal) {
				if journal.GetSince() != "2026-10-20T10:00:00.500Z" || journal.GetUntil() != "2026-10-20T11:00:00Z" {
					t.Errorf("since/until = %q/%q", journal.GetSince(), journal.GetUntil())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(panel.dispatcher.sent())
			response := panel.post(t, journalPath(serverID), tt.body)
			if response.StatusCode != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", response.StatusCode)
			}
			sent := panel.dispatcher.sent()
			if len(sent) != before+1 {
				t.Fatalf("sent %d commands, want %d", len(sent), before+1)
			}
			tt.check(t, sent[before].GetReadJournal())
		})
	}
}

func TestJournalRejectsInvalidRequests(t *testing.T) {
	panel, serverID := newJournalPanel(t)

	tests := []struct {
		name string
		body any
	}{
		{name: "missing body", body: nil},
		{name: "malformed body", body: "not-an-object"},
		{name: "unit without suffix", body: map[string]any{"unit": "docker"}},
		{name: "unit bad suffix", body: map[string]any{"unit": "docker.mount"}},
		{name: "unit path", body: map[string]any{"unit": "/etc/systemd/system/docker.service"}},
		{name: "unit leading dash", body: map[string]any{"unit": "-docker.service"}},
		{name: "unit injection", body: map[string]any{"unit": "docker.service;rm"}},
		{name: "unit too long", body: map[string]any{"unit": strings.Repeat("a", 251) + ".service"}},
		{name: "priority negative", body: map[string]any{"max_priority": -1}},
		{name: "priority high", body: map[string]any{"max_priority": 8}},
		{name: "priority not a number", body: map[string]any{"max_priority": "3"}},
		{name: "since invalid", body: map[string]any{"since": "yesterday"}},
		{name: "since date only", body: map[string]any{"since": "2026-10-20"}},
		{name: "since too long", body: map[string]any{"since": strings.Repeat("2", 41)}},
		{name: "until invalid", body: map[string]any{"until": "tomorrow"}},
		{name: "search too long", body: map[string]any{"search": strings.Repeat("x", 201)}},
		{name: "search nul", body: map[string]any{"search": "a\x00b"}},
		{name: "search newline", body: map[string]any{"search": "a\nb"}},
		{name: "search tab", body: map[string]any{"search": "a\tb"}},
		{name: "boot unknown", body: map[string]any{"boot": "later"}},
		{name: "tail negative", body: map[string]any{"tail": -1}},
		{name: "tail high", body: map[string]any{"tail": 1001}},
		{name: "cursor space", body: map[string]any{"after_cursor": "bad cursor"}},
		{name: "cursor too long", body: map[string]any{"after_cursor": strings.Repeat("a", 513)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(panel.dispatcher.sent())
			response := panel.post(t, journalPath(serverID), tt.body)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", response.StatusCode)
			}
			if code := apiCodeOf(t, response); code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", code)
			}
			if sent := panel.dispatcher.sent(); len(sent) != before {
				t.Errorf("a rejected request sent a command: %v", sent)
			}
		})
	}
}

func TestJournalRejectsAnOfflineAgent(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)

	response := panel.post(t, journalPath(serverID), map[string]any{"unit": "docker.service"})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.StatusCode)
	}
	if code := apiCodeOf(t, response); code != "agent_offline" {
		t.Errorf("code = %q, want agent_offline", code)
	}
	if sent := panel.dispatcher.sent(); len(sent) != 0 {
		t.Errorf("an offline request sent a command: %v", sent)
	}
}

func TestJournalJobTimeoutOutlastsTheAgent(t *testing.T) {
	if got := jobTimeout(journalReadKind); got <= time.Minute {
		t.Errorf("jobTimeout(%s) = %s, want more than 60s", journalReadKind, got)
	}
}
