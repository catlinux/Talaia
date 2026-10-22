package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	journalTimeout       = 30 * time.Second
	maxJournalOutput     = 256 << 10
	maxJournalMessage    = 4096
	maxJournalSearch     = 200
	maxJournalErrorChars = 300
	journalTimeFormat    = "2006-01-02 15:04:05"
	journalMessageCut    = "…"
)

var (
	journalUnitRE   = regexp.MustCompile(`^[A-Za-z0-9@._:-]{1,250}\.(service|socket|timer)$`)
	journalCursorRE = regexp.MustCompile(`^[A-Za-z0-9=;_-]{1,512}$`)

	// ErrJournalUnavailable means the host has no journalctl.
	ErrJournalUnavailable = errors.New("journalctl is not available")
	// errNoJournal is the failure of a journal job on an executor without a reader.
	errNoJournal = errors.New("journal is not available on this agent")
)

// JournalReader answers a ReadJournal command with the bounded entries JSON.
type JournalReader interface {
	Read(ctx context.Context, request *agentv1.ReadJournal) (string, error)
}

// JournalRunner runs journalctl. The production runner uses os/exec; tests
// inject a fake and inspect the exact arguments.
type JournalRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type journalExitError struct {
	code   int
	stderr string
}

func (e *journalExitError) Error() string {
	// The stderr detail is added by Read; keeping it out here stops a wrapped
	// error from repeating an untrimmed message.
	return fmt.Sprintf("journalctl exited with status %d", e.code)
}

func (e *journalExitError) ExitCode() int { return e.code }

// journalFailure carries a trimmed detail while keeping the cause available to
// errors.Is and errors.As without repeating the whole cause in the message.
type journalFailure struct {
	detail string
	cause  error
}

func (e *journalFailure) Error() string { return e.detail }
func (e *journalFailure) Unwrap() error { return e.cause }

func journalCommandError(detail string, cause error) error {
	detail = trimJournalError(detail)
	if detail == "" {
		detail = "journalctl failed"
	}
	return fmt.Errorf("journalctl: %w", &journalFailure{detail: detail, cause: cause})
}

type journalExecRunner struct{}

func (journalExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return output, ErrJournalUnavailable
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return output, &journalExitError{code: exit.ExitCode(), stderr: strings.TrimSpace(stderr.String())}
	}
	return output, err
}

// Journal reads systemd journal entries through journalctl.
type Journal struct {
	runner JournalRunner
}

// NewJournalReader returns a reader that uses the production journalctl runner
// when runner is nil.
func NewJournalReader(runner JournalRunner) *Journal {
	if runner == nil {
		runner = journalExecRunner{}
	}
	return &Journal{runner: runner}
}

// Read validates the request, runs journalctl and returns bounded JSON with the
// entries and the cursor to continue from.
func (j *Journal) Read(ctx context.Context, request *agentv1.ReadJournal) (string, error) {
	if request == nil {
		request = &agentv1.ReadJournal{}
	}
	args, err := journalArgs(request)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, journalTimeout)
	defer cancel()
	if j == nil || j.runner == nil {
		return "", ErrJournalUnavailable
	}
	output, err := j.runner.Output(ctx, "journalctl", args...)
	if err != nil {
		if errors.Is(err, ErrJournalUnavailable) || errors.Is(err, exec.ErrNotFound) {
			return "", ErrJournalUnavailable
		}
		if code, ok := journalExitCode(err); ok && code == 1 && len(bytes.TrimSpace(output)) == 0 {
			return marshalJournal(nil, request.GetAfterCursor())
		}
		var exit *journalExitError
		if errors.As(err, &exit) {
			detail := exit.stderr
			if detail == "" {
				detail = exit.Error()
			} else {
				detail += ": " + exit.Error()
			}
			return "", journalCommandError(detail, err)
		}
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return "", journalCommandError(detail, err)
	}
	return parseJournalOutput(output, request.GetAfterCursor())
}

// journalExitCode recognizes the fake and real forms of a nonzero exit.
func journalExitCode(err error) (int, bool) {
	var exit *journalExitError
	if errors.As(err, &exit) {
		return exit.code, true
	}
	var coder interface{ ExitCode() int }
	if errors.As(err, &coder) {
		return coder.ExitCode(), true
	}
	if strings.HasSuffix(strings.TrimSpace(err.Error()), "exit status 1") {
		return 1, true
	}
	return 0, false
}

// journalArgs validates the filters and builds the argv. Nothing from the
// request reaches journalctl before this succeeds.
func journalArgs(request *agentv1.ReadJournal) ([]string, error) {
	if request == nil {
		request = &agentv1.ReadJournal{}
	}
	tail := clampTail(int(request.GetTail()))
	args := []string{"--no-pager", "--output=json", "-n", strconv.Itoa(tail)}

	if unit := request.GetUnit(); unit != "" {
		if strings.HasPrefix(unit, "-") || !journalUnitRE.MatchString(unit) {
			return nil, fmt.Errorf("invalid journal unit %q", unit)
		}
		args = append(args, "-u", unit)
	}

	if request.MaxPriority != nil {
		priority := *request.MaxPriority
		if priority > 7 {
			return nil, fmt.Errorf("invalid journal priority %d", priority)
		}
		args = append(args, "-p", strconv.Itoa(int(priority)))
	}

	if since := request.GetSince(); since != "" {
		value, err := journalTime(since, "since")
		if err != nil {
			return nil, err
		}
		args = append(args, "--since", value)
	}
	if until := request.GetUntil(); until != "" {
		value, err := journalTime(until, "until")
		if err != nil {
			return nil, err
		}
		args = append(args, "--until", value)
	}

	if search := request.GetSearch(); search != "" {
		if err := validateJournalSearch(search); err != nil {
			return nil, err
		}
		args = append(args, "--grep="+journalSearchPattern(search), "--case-sensitive=false")
	}

	switch request.GetBoot() {
	case agentv1.JournalBoot_JOURNAL_BOOT_UNSPECIFIED:
	case agentv1.JournalBoot_JOURNAL_BOOT_CURRENT:
		args = append(args, "-b", "0")
	case agentv1.JournalBoot_JOURNAL_BOOT_PREVIOUS:
		args = append(args, "-b", "-1")
	default:
		return nil, fmt.Errorf("invalid journal boot %s", request.GetBoot())
	}

	if cursor := request.GetAfterCursor(); cursor != "" {
		if !journalCursorRE.MatchString(cursor) {
			return nil, fmt.Errorf("invalid journal cursor")
		}
		args = append(args, "--after-cursor="+cursor)
	}
	return args, nil
}

func journalTime(value, label string) (string, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", fmt.Errorf("invalid %s timestamp: %w", label, err)
	}
	return parsed.In(time.Local).Format(journalTimeFormat), nil
}

// journalSearchPattern turns the literal search into a PCRE2 pattern. QuoteMeta
// escapes regex metacharacters; the second pass escapes the remaining
// non-alphanumeric characters so PCRE2 cannot give them a special meaning.
func journalSearchPattern(search string) string {
	quoted := regexp.QuoteMeta(search)
	var pattern strings.Builder
	escaped := false
	for _, char := range quoted {
		if escaped {
			pattern.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' {
			pattern.WriteRune(char)
			escaped = true
			continue
		}
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == ' ' {
			pattern.WriteRune(char)
			continue
		}
		pattern.WriteRune('\\')
		pattern.WriteRune(char)
	}
	if escaped {
		pattern.WriteRune('\\')
	}
	return pattern.String()
}

func validateJournalSearch(search string) error {
	if utf8.RuneCountInString(search) > maxJournalSearch {
		return fmt.Errorf("the journal search is longer than %d characters", maxJournalSearch)
	}
	if strings.ContainsAny(search, "\x00\r\n") {
		return errors.New("the journal search contains a forbidden character")
	}
	return nil
}

func trimJournalError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxJournalErrorChars {
		value = value[:maxJournalErrorChars]
	}
	return value
}

type journalEntry struct {
	Ts       string `json:"ts"`
	Unit     string `json:"unit"`
	Priority int    `json:"priority"`
	Pid      string `json:"pid"`
	Message  string `json:"message"`
}

type journalParsedEntry struct {
	entry  journalEntry
	cursor string
	at     time.Time
}

type journalOutput struct {
	Entries []journalEntry `json:"entries"`
	Cursor  string         `json:"cursor"`
}

func parseJournalOutput(output []byte, afterCursor string) (string, error) {
	parsed := make([]journalParsedEntry, 0)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			continue
		}
		if fields == nil {
			// "null" is valid JSON but not a journal entry.
			continue
		}
		ts, at := journalTimestamp(fields["__REALTIME_TIMESTAMP"])
		entry := journalEntry{
			Ts:       ts,
			Unit:     journalString(fields["_SYSTEMD_UNIT"]),
			Priority: journalPriority(fields["PRIORITY"]),
			Pid:      journalPID(fields),
			Message:  truncateJournalMessage(journalMessage(fields["MESSAGE"])),
		}
		if entry.Unit == "" {
			entry.Unit = journalString(fields["SYSLOG_IDENTIFIER"])
		}
		parsed = append(parsed, journalParsedEntry{
			entry:  entry,
			cursor: journalString(fields["__CURSOR"]),
			at:     at,
		})
	}

	// journalctl normally returns oldest-first, but sorting here keeps that
	// invariant when all entries carry a timestamp.
	timed := true
	for _, item := range parsed {
		if item.at.IsZero() {
			timed = false
			break
		}
	}
	if timed {
		sort.SliceStable(parsed, func(i, j int) bool { return parsed[i].at.Before(parsed[j].at) })
	}

	if len(parsed) == 0 {
		return marshalJournal(nil, afterCursor)
	}
	return boundJournalOutput(parsed, afterCursor)
}

// boundJournalOutput drops the oldest entries until the JSON fits, using a
// binary search so a large batch does not re-encode once per dropped entry.
func boundJournalOutput(parsed []journalParsedEntry, afterCursor string) (string, error) {
	low, high := 0, len(parsed)
	for low < high {
		middle := low + (high-low)/2
		output, err := marshalJournalEntries(parsed[middle:], afterCursor)
		if err != nil {
			return "", err
		}
		if len(output) <= maxJournalOutput {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return marshalJournalEntries(parsed[low:], afterCursor)
}

func marshalJournalEntries(parsed []journalParsedEntry, afterCursor string) (string, error) {
	entries := make([]journalEntry, 0, len(parsed))
	for _, item := range parsed {
		entries = append(entries, item.entry)
	}
	return marshalJournal(entries, journalCursor(parsed, afterCursor))
}

func marshalJournal(entries []journalEntry, cursor string) (string, error) {
	if entries == nil {
		entries = []journalEntry{}
	}
	data, err := json.Marshal(journalOutput{Entries: entries, Cursor: cursor})
	if err != nil {
		return "", fmt.Errorf("encode the journal entries: %w", err)
	}
	return string(data), nil
}

func journalCursor(parsed []journalParsedEntry, afterCursor string) string {
	if len(parsed) == 0 {
		return afterCursor
	}
	if cursor := parsed[len(parsed)-1].cursor; cursor != "" {
		return cursor
	}
	return afterCursor
}

func journalString(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func journalTimestamp(raw json.RawMessage) (string, time.Time) {
	value := journalString(raw)
	if value == "" {
		return "", time.Time{}
	}
	micros, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return "", time.Time{}
	}
	at := time.Unix(0, micros*1000).UTC()
	return at.Format(time.RFC3339Nano), at
}

func journalPriority(raw json.RawMessage) int {
	value := journalString(raw)
	if value == "" {
		return 6
	}
	priority, err := strconv.Atoi(value)
	if err != nil {
		return 6
	}
	return priority
}

func journalPID(fields map[string]json.RawMessage) string {
	if pid := journalString(fields["_PID"]); pid != "" {
		return pid
	}
	return journalString(fields["SYSLOG_PID"])
}

func journalMessage(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var values []int
	if json.Unmarshal(raw, &values) == nil {
		bytesValue := make([]byte, 0, len(values))
		for _, value := range values {
			bytesValue = append(bytesValue, byte(value))
		}
		return strings.ToValidUTF8(string(bytesValue), "�")
	}
	return strings.ToValidUTF8(string(raw), "�")
}

func truncateJournalMessage(message string) string {
	if len(message) <= maxJournalMessage {
		return message
	}
	cut := maxJournalMessage - len(journalMessageCut)
	if cut < 0 {
		cut = 0
	}
	message = message[:cut]
	for !utf8.ValidString(message) && len(message) > 0 {
		message = message[:len(message)-1]
	}
	return message + journalMessageCut
}
