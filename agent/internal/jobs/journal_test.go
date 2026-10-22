package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type journalRunnerCall struct {
	name string
	args []string
}

type fakeJournalRunner struct {
	mu      sync.Mutex
	calls   []journalRunnerCall
	handler func(name string, args []string) ([]byte, error)
}

func (r *fakeJournalRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, journalRunnerCall{name: name, args: append([]string(nil), args...)})
	handler := r.handler
	r.mu.Unlock()
	if handler != nil {
		return handler(name, args)
	}
	return nil, nil
}

func (r *fakeJournalRunner) recorded() []journalRunnerCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]journalRunnerCall(nil), r.calls...)
}

func journalPriorityPtr(value uint32) *uint32 { return &value }

func localJournalTime(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		panic(err)
	}
	return parsed.In(time.Local).Format(journalTimeFormat)
}

func TestJournalReadArgv(t *testing.T) {
	since := "2026-10-22T10:20:30+02:00"
	until := "2026-10-22T11:20:30Z"
	tests := []struct {
		name    string
		request *agentv1.ReadJournal
		want    []string
	}{
		{
			name:    "defaults",
			request: &agentv1.ReadJournal{},
			want:    []string{"--no-pager", "--output=json", "-n", "200"},
		},
		{
			name:    "unit service",
			request: &agentv1.ReadJournal{Unit: "docker.service"},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "-u", "docker.service"},
		},
		{
			name:    "unit socket",
			request: &agentv1.ReadJournal{Unit: "docker.socket"},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "-u", "docker.socket"},
		},
		{
			name:    "unit timer",
			request: &agentv1.ReadJournal{Unit: "logrotate.timer"},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "-u", "logrotate.timer"},
		},
		{
			name:    "priority zero",
			request: &agentv1.ReadJournal{MaxPriority: journalPriorityPtr(0)},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "-p", "0"},
		},
		{
			name:    "priority seven",
			request: &agentv1.ReadJournal{MaxPriority: journalPriorityPtr(7)},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "-p", "7"},
		},
		{
			name:    "since",
			request: &agentv1.ReadJournal{Since: since},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "--since", localJournalTime(since)},
		},
		{
			name:    "until",
			request: &agentv1.ReadJournal{Until: until},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "--until", localJournalTime(until)},
		},
		{
			name:    "search",
			request: &agentv1.ReadJournal{Search: "a.b-c"},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "--grep=a\\.b\\-c", "--case-sensitive=false"},
		},
		{
			name:    "boot current",
			request: &agentv1.ReadJournal{Boot: agentv1.JournalBoot_JOURNAL_BOOT_CURRENT},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "-b", "0"},
		},
		{
			name:    "boot previous",
			request: &agentv1.ReadJournal{Boot: agentv1.JournalBoot_JOURNAL_BOOT_PREVIOUS},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "-b", "-1"},
		},
		{
			name:    "tail default",
			request: &agentv1.ReadJournal{Tail: 0},
			want:    []string{"--no-pager", "--output=json", "-n", "200"},
		},
		{
			name:    "tail clamps high",
			request: &agentv1.ReadJournal{Tail: 1500},
			want:    []string{"--no-pager", "--output=json", "-n", "1000"},
		},
		{
			name:    "tail keeps small",
			request: &agentv1.ReadJournal{Tail: 5},
			want:    []string{"--no-pager", "--output=json", "-n", "5"},
		},
		{
			name:    "after cursor",
			request: &agentv1.ReadJournal{AfterCursor: "s=abc;def_-123"},
			want:    []string{"--no-pager", "--output=json", "-n", "200", "--after-cursor=s=abc;def_-123"},
		},
		{
			name: "all filters",
			request: &agentv1.ReadJournal{
				Unit:        "nginx.timer",
				MaxPriority: journalPriorityPtr(4),
				Since:       since,
				Until:       until,
				Search:      "a.b-c",
				Boot:        agentv1.JournalBoot_JOURNAL_BOOT_PREVIOUS,
				Tail:        50,
				AfterCursor: "s=abc",
			},
			want: []string{
				"--no-pager", "--output=json", "-n", "50",
				"-u", "nginx.timer",
				"-p", "4",
				"--since", localJournalTime(since),
				"--until", localJournalTime(until),
				"--grep=a\\.b\\-c", "--case-sensitive=false",
				"-b", "-1",
				"--after-cursor=s=abc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeJournalRunner{}
			if _, err := NewJournalReader(runner).Read(context.Background(), tt.request); err != nil {
				t.Fatalf("Read: %v", err)
			}
			calls := runner.recorded()
			if len(calls) != 1 {
				t.Fatalf("calls = %v, want one journalctl call", calls)
			}
			if calls[0].name != "journalctl" {
				t.Errorf("command = %q, want journalctl", calls[0].name)
			}
			if !sameArgs(calls[0].args, tt.want) {
				t.Errorf("args = %v, want %v", calls[0].args, tt.want)
			}
		})
	}
}

func TestJournalReadRejectsInvalidFilters(t *testing.T) {
	tests := []struct {
		name    string
		request *agentv1.ReadJournal
		want    string
	}{
		{name: "unit without suffix", request: &agentv1.ReadJournal{Unit: "docker"}, want: "invalid journal unit"},
		{name: "unit bad suffix", request: &agentv1.ReadJournal{Unit: "docker.socketx"}, want: "invalid journal unit"},
		{name: "unit leading dash", request: &agentv1.ReadJournal{Unit: "-docker.service"}, want: "invalid journal unit"},
		{name: "unit path", request: &agentv1.ReadJournal{Unit: "/etc/systemd/system/docker.service"}, want: "invalid journal unit"},
		{name: "unit too long", request: &agentv1.ReadJournal{Unit: strings.Repeat("a", 251) + ".service"}, want: "invalid journal unit"},
		{name: "priority", request: &agentv1.ReadJournal{MaxPriority: journalPriorityPtr(8)}, want: "invalid journal priority"},
		{name: "since", request: &agentv1.ReadJournal{Since: "yesterday"}, want: "invalid since timestamp"},
		{name: "until", request: &agentv1.ReadJournal{Until: "2026-13-01T00:00:00Z"}, want: "invalid until timestamp"},
		{name: "search too long", request: &agentv1.ReadJournal{Search: strings.Repeat("x", 201)}, want: "longer than"},
		{name: "search nul", request: &agentv1.ReadJournal{Search: "a\x00b"}, want: "forbidden character"},
		{name: "search newline", request: &agentv1.ReadJournal{Search: "a\nb"}, want: "forbidden character"},
		{name: "search carriage return", request: &agentv1.ReadJournal{Search: "a\rb"}, want: "forbidden character"},
		{name: "cursor space", request: &agentv1.ReadJournal{AfterCursor: "bad cursor"}, want: "invalid journal cursor"},
		{name: "cursor too long", request: &agentv1.ReadJournal{AfterCursor: strings.Repeat("a", 513)}, want: "invalid journal cursor"},
		{name: "boot", request: &agentv1.ReadJournal{Boot: agentv1.JournalBoot(99)}, want: "invalid journal boot"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeJournalRunner{}
			_, err := NewJournalReader(runner).Read(context.Background(), tt.request)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one containing %q", err, tt.want)
			}
			if calls := runner.recorded(); len(calls) != 0 {
				t.Errorf("invalid request reached journalctl: %v", calls)
			}
		})
	}
}

func TestJournalReadParsesEntries(t *testing.T) {
	const firstMicros = int64(1761127200123456)
	const secondMicros = int64(1761127201123456)
	const thirdMicros = int64(1761127202123456)
	const fourthMicros = int64(1761127203123456)
	lines := []string{
		fmt.Sprintf(`{"__REALTIME_TIMESTAMP":"%d","_SYSTEMD_UNIT":"docker.service","PRIORITY":"3","_PID":"42","MESSAGE":"hello","__CURSOR":"s=1"}`, firstMicros),
		fmt.Sprintf(`{"__REALTIME_TIMESTAMP":"%d","SYSLOG_IDENTIFIER":"nginx","PRIORITY":"6","SYSLOG_PID":"7","MESSAGE":[104,105],"__CURSOR":"s=2"}`, secondMicros),
		fmt.Sprintf(`{"__REALTIME_TIMESTAMP":"%d","_SYSTEMD_UNIT":"bad.service","PRIORITY":"1","MESSAGE":[255,254],"__CURSOR":"s=3"}`, thirdMicros),
		fmt.Sprintf(`{"__REALTIME_TIMESTAMP":"%d","_SYSTEMD_UNIT":"empty.service","MESSAGE":null,"__CURSOR":"s=4"}`, fourthMicros),
		`not json`,
		`[]`,
		`null`,
	}

	output, err := parseJournalOutput([]byte(strings.Join(lines, "\n")), "")
	if err != nil {
		t.Fatalf("parseJournalOutput: %v", err)
	}
	var doc journalOutput
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(doc.Entries) != 4 {
		t.Fatalf("entries = %d, want 4: %s", len(doc.Entries), output)
	}
	if doc.Cursor != "s=4" {
		t.Errorf("cursor = %q, want s=4", doc.Cursor)
	}

	first := doc.Entries[0]
	wantTs := time.Unix(0, firstMicros*1000).UTC().Format(time.RFC3339Nano)
	if first.Ts != wantTs {
		t.Errorf("first ts = %q, want %q", first.Ts, wantTs)
	}
	if first.Unit != "docker.service" || first.Priority != 3 || first.Pid != "42" || first.Message != "hello" {
		t.Errorf("first entry = %+v", first)
	}

	second := doc.Entries[1]
	if second.Unit != "nginx" || second.Priority != 6 || second.Pid != "7" || second.Message != "hi" {
		t.Errorf("second entry = %+v", second)
	}

	third := doc.Entries[2]
	if third.Message != "�" {
		t.Errorf("non-UTF8 message = %q, want a replacement character", third.Message)
	}

	fourth := doc.Entries[3]
	if fourth.Message != "" || fourth.Priority != 6 {
		t.Errorf("null message entry = %+v", fourth)
	}
}

func TestJournalReadSortsEntriesOldestFirst(t *testing.T) {
	lines := []string{
		`{"__REALTIME_TIMESTAMP":"2000000","MESSAGE":"new","__CURSOR":"s=new"}`,
		`{"__REALTIME_TIMESTAMP":"1000000","MESSAGE":"old","__CURSOR":"s=old"}`,
	}
	output, err := parseJournalOutput([]byte(strings.Join(lines, "\n")), "")
	if err != nil {
		t.Fatalf("parseJournalOutput: %v", err)
	}
	var doc journalOutput
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(doc.Entries) != 2 || doc.Entries[0].Message != "old" || doc.Entries[1].Message != "new" {
		t.Errorf("entries = %+v, want old then new", doc.Entries)
	}
	if doc.Cursor != "s=new" {
		t.Errorf("cursor = %q, want s=new", doc.Cursor)
	}
}

func TestJournalReadTruncatesLongMessages(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{name: "ascii", message: strings.Repeat("x", 5000)},
		{name: "unicode", message: strings.Repeat("á", 3000)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, err := json.Marshal(map[string]any{
				"__REALTIME_TIMESTAMP": "1000000",
				"MESSAGE":              tt.message,
			})
			if err != nil {
				t.Fatalf("encode line: %v", err)
			}
			output, err := parseJournalOutput(line, "")
			if err != nil {
				t.Fatalf("parseJournalOutput: %v", err)
			}
			var doc journalOutput
			if err := json.Unmarshal([]byte(output), &doc); err != nil {
				t.Fatalf("decode output: %v", err)
			}
			message := doc.Entries[0].Message
			if !strings.HasSuffix(message, "…") {
				t.Errorf("message does not end with the truncation marker: %q", message[len(message)-10:])
			}
			if len(message) > maxJournalMessage {
				t.Errorf("message is %d bytes, want at most %d", len(message), maxJournalMessage)
			}
			if !utf8.ValidString(message) {
				t.Errorf("message is not valid UTF-8")
			}
		})
	}
}

func TestJournalReadBoundsOutput(t *testing.T) {
	lines := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		line, err := json.Marshal(map[string]any{
			"__REALTIME_TIMESTAMP": fmt.Sprintf("%d", 1000000+i),
			"MESSAGE":              fmt.Sprintf("%04d:%s", i, strings.Repeat("x", 4000)),
			"__CURSOR":             fmt.Sprintf("c%d", i),
		})
		if err != nil {
			t.Fatalf("encode line %d: %v", i, err)
		}
		lines = append(lines, string(line))
	}

	output, err := parseJournalOutput([]byte(strings.Join(lines, "\n")), "")
	if err != nil {
		t.Fatalf("parseJournalOutput: %v", err)
	}
	if len(output) > maxJournalOutput {
		t.Fatalf("output is %d bytes, want at most %d", len(output), maxJournalOutput)
	}
	var doc journalOutput
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(doc.Entries) == 0 {
		t.Fatal("all entries were dropped")
	}
	last := doc.Entries[len(doc.Entries)-1]
	if !strings.HasPrefix(last.Message, "0099:") {
		t.Errorf("last message = %q, want entry 0099", last.Message[:5])
	}
	if doc.Cursor != "c99" {
		t.Errorf("cursor = %q, want c99", doc.Cursor)
	}
	if strings.HasPrefix(doc.Entries[0].Message, "0000:") {
		t.Errorf("the oldest entry was not dropped")
	}
}

func TestJournalReadNoMatchesIsEmpty(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "typed exit", err: &journalExitError{code: 1, stderr: "-- No entries --"}},
		{name: "generic exit", err: errors.New("exit status 1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeJournalRunner{handler: func(string, []string) ([]byte, error) {
				return nil, tt.err
			}}
			output, err := NewJournalReader(runner).Read(context.Background(), &agentv1.ReadJournal{AfterCursor: "s=next"})
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			var doc journalOutput
			if err := json.Unmarshal([]byte(output), &doc); err != nil {
				t.Fatalf("decode output: %v", err)
			}
			if len(doc.Entries) != 0 || doc.Cursor != "s=next" {
				t.Errorf("output = %s, want no entries and cursor s=next", output)
			}
		})
	}
}

func TestJournalReadMissingJournalctl(t *testing.T) {
	for _, cause := range []error{ErrJournalUnavailable, exec.ErrNotFound} {
		runner := &fakeJournalRunner{handler: func(string, []string) ([]byte, error) {
			return nil, cause
		}}
		_, err := NewJournalReader(runner).Read(context.Background(), &agentv1.ReadJournal{})
		if !errors.Is(err, ErrJournalUnavailable) || err.Error() != "journalctl is not available" {
			t.Errorf("error = %v, want journalctl is not available", err)
		}
	}
}

func TestJournalReadTrimsStderr(t *testing.T) {
	detail := strings.Repeat("e", 1000)
	tests := []struct {
		name   string
		output []byte
		err    error
	}{
		{name: "typed", err: &journalExitError{code: 2, stderr: detail}},
		{name: "output and generic", output: []byte(detail), err: errors.New("exit status 2")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeJournalRunner{handler: func(string, []string) ([]byte, error) {
				return tt.output, tt.err
			}}
			_, err := NewJournalReader(runner).Read(context.Background(), &agentv1.ReadJournal{})
			if err == nil {
				t.Fatal("Read succeeded, want an error")
			}
			if strings.Contains(err.Error(), detail) {
				t.Errorf("error kept the whole stderr: %d bytes", len(err.Error()))
			}
			if len(err.Error()) > maxJournalErrorChars+80 {
				t.Errorf("error is %d bytes, want it trimmed to about %d", len(err.Error()), maxJournalErrorChars)
			}
		})
	}
}

func TestJournalSearchPattern(t *testing.T) {
	tests := []struct {
		search string
		want   string
	}{
		{search: "hello world", want: "hello world"},
		{search: `a.b`, want: `a\.b`},
		{search: "a-b", want: `a\-b`},
		{search: "100%", want: `100\%`},
		{search: `a\b`, want: `a\\b`},
		{search: `a+b*c?`, want: `a\+b\*c\?`},
	}
	for _, tt := range tests {
		t.Run(tt.search, func(t *testing.T) {
			if got := journalSearchPattern(tt.search); got != tt.want {
				t.Errorf("journalSearchPattern(%q) = %q, want %q", tt.search, got, tt.want)
			}
		})
	}
}

func TestJournalTimeUsesLocalZone(t *testing.T) {
	value := "2026-10-22T10:20:30.123456+02:00"
	got, err := journalTime(value, "since")
	if err != nil {
		t.Fatalf("journalTime: %v", err)
	}
	if want := localJournalTime(value); got != want {
		t.Errorf("time = %q, want %q", got, want)
	}
	if _, err := journalTime("no", "until"); err == nil {
		t.Error("an invalid time was accepted")
	}
}

func journalReadCommand(jobID string, request *agentv1.ReadJournal) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_ReadJournal{
			ReadJournal: request,
		},
	}
}

type fakeJournalReader struct {
	mu     sync.Mutex
	calls  []*agentv1.ReadJournal
	output string
	err    error
}

func (f *fakeJournalReader) Read(_ context.Context, request *agentv1.ReadJournal) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request)
	return f.output, f.err
}

func TestHandleRunsJournalRead(t *testing.T) {
	journal := &fakeJournalReader{output: `{"entries":[],"cursor":""}`}
	out := newCollector()
	executor := New(&fakeActions{}).WithJournal(journal)
	request := &agentv1.ReadJournal{Unit: "docker.service", Tail: 10}

	if !executor.Handle(context.Background(), journalReadCommand("job-journal-1", request), out.emit) {
		t.Fatal("the journal command was not handled")
	}
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_RUNNING {
		t.Errorf("first event = %v", got)
	}
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetOutput() != journal.output {
		t.Errorf("final event = %v", got)
	}
	if len(journal.calls) != 1 || journal.calls[0] != request {
		t.Errorf("calls = %v, want the journal request", journal.calls)
	}
}

func TestHandleFailsWithoutAJournalReader(t *testing.T) {
	out := newCollector()
	New(&fakeActions{}).Handle(context.Background(), journalReadCommand("job-journal-2", &agentv1.ReadJournal{}), out.emit)
	out.next(t) // running
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() != errNoJournal.Error() {
		t.Errorf("final event = %v", got)
	}
}
