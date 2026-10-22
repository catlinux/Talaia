package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	// journalReadKind is the job kind of a journal read. Live journal reads
	// create a job every few seconds, so it uses the short retention in
	// internal/jobs.
	journalReadKind = "journal_read"

	maxJournalTime   = 40
	maxJournalSearch = 200
	maxJournalTail   = 1000
)

var (
	journalUnitRE        = regexp.MustCompile(`^[A-Za-z0-9@._:-]{1,250}\.(service|socket|timer)$`)
	journalAfterCursorRE = regexp.MustCompile(`^[A-Za-z0-9=;_-]{1,512}$`)

	journalBoots = map[string]agentv1.JournalBoot{
		"all":      agentv1.JournalBoot_JOURNAL_BOOT_UNSPECIFIED,
		"current":  agentv1.JournalBoot_JOURNAL_BOOT_CURRENT,
		"previous": agentv1.JournalBoot_JOURNAL_BOOT_PREVIOUS,
	}
)

type journalRequest struct {
	Unit        string `json:"unit"`
	MaxPriority *int   `json:"max_priority"`
	Since       string `json:"since"`
	Until       string `json:"until"`
	Search      string `json:"search"`
	Boot        string `json:"boot"`
	Tail        int    `json:"tail"`
	AfterCursor string `json:"after_cursor"`
}

// journalCommand is the validated form of a journal request.
type journalCommand struct {
	unit        string
	maxPriority *uint32
	since       string
	until       string
	search      string
	boot        string
	bootValue   agentv1.JournalBoot
	tail        uint32
	afterCursor string
}

// NewJournalHandler returns POST /api/v1/servers/{serverID}/journal. The entries
// and the cursor come back in the output of the job it creates. The journal
// text and the search text are never written to the audit log.
func NewJournalHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")

		var request journalRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		command, valid := validateJournalRequest(request)
		if !valid {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}

		if _, connected := connectedAgent(deps.Agents, serverID); !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}

		params := map[string]any{
			"unit": command.unit,
			"boot": command.boot,
			"tail": int(command.tail),
		}

		submitJob(w, r, deps, serverID, journalReadKind, "journal_read",
			params,
			map[string]any{"unit": command.unit, "boot": command.boot},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_ReadJournal{
						ReadJournal: &agentv1.ReadJournal{
							Unit:        command.unit,
							MaxPriority: command.maxPriority,
							Since:       command.since,
							Until:       command.until,
							Search:      command.search,
							Boot:        command.bootValue,
							Tail:        command.tail,
							AfterCursor: command.afterCursor,
						},
					},
				}
			})
	})
}

// validateJournalRequest checks every field before anything reaches the agent.
// A non-empty unit does not have to be in the inventory: reading the journal of
// a unit that was removed is useful.
func validateJournalRequest(request journalRequest) (journalCommand, bool) {
	var command journalCommand

	if request.Unit != "" {
		if strings.HasPrefix(request.Unit, "-") || !journalUnitRE.MatchString(request.Unit) {
			return command, false
		}
	}
	command.unit = request.Unit

	if request.MaxPriority != nil {
		if *request.MaxPriority < 0 || *request.MaxPriority > 7 {
			return command, false
		}
		priority := uint32(*request.MaxPriority)
		command.maxPriority = &priority
	}

	since, ok := validJournalTime(request.Since)
	if !ok {
		return command, false
	}
	command.since = since

	until, ok := validJournalTime(request.Until)
	if !ok {
		return command, false
	}
	command.until = until

	if !validJournalSearch(request.Search) {
		return command, false
	}
	command.search = request.Search

	boot := request.Boot
	if boot == "" {
		boot = "all"
	}
	bootValue, known := journalBoots[boot]
	if !known {
		return command, false
	}
	command.boot = boot
	command.bootValue = bootValue

	if request.Tail < 0 || request.Tail > maxJournalTail {
		return command, false
	}
	command.tail = uint32(request.Tail)

	if request.AfterCursor != "" && !journalAfterCursorRE.MatchString(request.AfterCursor) {
		return command, false
	}
	command.afterCursor = request.AfterCursor

	return command, true
}

// validJournalTime accepts an empty value or an RFC 3339 timestamp of at most
// 40 characters and returns it without surrounding spaces.
func validJournalTime(value string) (string, bool) {
	if len(value) > maxJournalTime {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return "", false
	}
	return value, true
}

// validJournalSearch accepts literal text up to 200 characters with no control
// characters.
func validJournalSearch(search string) bool {
	if utf8.RuneCountInString(search) > maxJournalSearch {
		return false
	}
	for _, char := range search {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}
