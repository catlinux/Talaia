package updates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	shaHead    = "1111111111111111111111111111111111111111"
	shaGreen   = "2222222222222222222222222222222222222222"
	shaCurrent = "3333333333333333333333333333333333333333"
	shaPending = "4444444444444444444444444444444444444444"
)

// fakeClock is a clock the tests can move.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func newChecker(t *testing.T, server *httptest.Server, current string, clock *fakeClock) *Checker {
	t.Helper()
	checker, err := New(Options{
		Repo:          "o/r",
		APIURL:        server.URL,
		RawURL:        server.URL,
		CurrentCommit: current,
		Version:       "test",
		Client:        server.Client(),
		Now:           clock.Now,
		Logger:        testLogger(),
	})
	if err != nil {
		t.Fatalf("new checker: %v", err)
	}
	return checker
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// newNotesChecker serves a green commit ahead of current and delegates the raw
// changelog requests to raw.
func newNotesChecker(t *testing.T, clock *fakeClock, raw http.HandlerFunc) *Checker {
	t.Helper()
	server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/commits/main":
			if r.Header.Get("If-None-Match") == `"notes"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"notes"`)
			writeJSON(w, map[string]string{"sha": shaGreen})
		case r.URL.Path == "/repos/o/r/commits":
			writeJSON(w, []map[string]string{{"sha": shaGreen}})
		case r.URL.Path == "/repos/o/r/actions/runs":
			writeJSON(w, map[string]any{"workflow_runs": []map[string]string{
				{"name": "CI", "status": "completed", "conclusion": "success"},
			}})
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/compare/"):
			writeJSON(w, map[string]any{"status": "ahead", "commits": []map[string]any{{
				"sha": shaPending,
				"commit": map[string]any{
					"message": "Fix the panel",
					"author":  map[string]string{"name": "Ana"},
				},
			}}})
		case strings.HasPrefix(r.URL.Path, "/o/r/"):
			raw(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	return newChecker(t, server, shaCurrent, clock)
}

func TestCheckOffersTheNewestGreenCommit(t *testing.T) {
	clock := newFakeClock()
	var (
		mu             sync.Mutex
		userAgent      string
		compareQueries int
		etagSent       string
	)

	server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		userAgent = r.Header.Get("User-Agent")
		if r.URL.Path == "/repos/o/r/commits/main" {
			etagSent = r.Header.Get("If-None-Match")
		}
		mu.Unlock()

		switch {
		case r.URL.Path == "/repos/o/r/commits/main":
			w.Header().Set("ETag", `"head-1"`)
			writeJSON(w, map[string]string{"sha": shaHead})
		case r.URL.Path == "/repos/o/r/commits":
			if r.URL.Query().Get("sha") != "main" || r.URL.Query().Get("per_page") != "20" {
				http.Error(w, "unexpected commit query", http.StatusBadRequest)
				return
			}
			writeJSON(w, []map[string]string{{"sha": shaHead}, {"sha": shaGreen}})
		case r.URL.Path == "/repos/o/r/actions/runs":
			switch r.URL.Query().Get("head_sha") {
			case shaHead:
				writeJSON(w, map[string]any{"workflow_runs": []map[string]string{
					{"name": "CI", "status": "completed", "conclusion": "failure"},
					{"name": "Lint", "status": "completed", "conclusion": "failure"},
				}})
			case shaGreen:
				writeJSON(w, map[string]any{"workflow_runs": []map[string]string{
					{"name": "Lint", "status": "completed", "conclusion": "failure"},
					{"name": "CI", "status": "completed", "conclusion": "success"},
				}})
			default:
				http.Error(w, "unexpected sha", http.StatusBadRequest)
			}
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/compare/"):
			mu.Lock()
			compareQueries++
			mu.Unlock()
			if r.URL.Path != "/repos/o/r/compare/"+shaCurrent+"..."+shaGreen {
				http.Error(w, "unexpected comparison", http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]any{"status": "ahead", "commits": []map[string]any{
				{
					"sha": shaPending,
					"commit": map[string]any{
						"message": "Fix the panel\n\nWith more detail",
						"author":  map[string]string{"name": "Ana"},
					},
				},
			}})
		default:
			http.NotFound(w, r)
		}
	}))

	checker := newChecker(t, server, shaCurrent, clock)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	state, ok := checker.State()
	if !ok {
		t.Fatal("the checker has no state after a successful check")
	}
	if state.Current != shaCurrent || state.Latest != shaGreen || state.CI != "green" {
		t.Fatalf("state = %+v", state)
	}
	if !state.UpdateAvailable || state.Error != "" {
		t.Fatalf("state = %+v", state)
	}
	if state.CheckedAt != clock.Now() {
		t.Errorf("checked_at = %v, want %v", state.CheckedAt, clock.Now())
	}
	if len(state.Pending) != 1 {
		t.Fatalf("pending = %+v", state.Pending)
	}
	if pending := state.Pending[0]; pending.SHA != shaPending[:7] || pending.Subject != "Fix the panel" || pending.Author != "Ana" {
		t.Errorf("pending commit = %+v", pending)
	}
	if offered, ok := checker.LatestGreen(); !ok || offered != shaGreen {
		t.Errorf("LatestGreen() = %q, %t", offered, ok)
	}

	mu.Lock()
	defer mu.Unlock()
	if userAgent != "talaia-core/test" {
		t.Errorf("User-Agent = %q", userAgent)
	}
	if etagSent != "" {
		t.Errorf("first request sent If-None-Match %q", etagSent)
	}
	if compareQueries != 1 {
		t.Errorf("compare requests = %d, want 1", compareQueries)
	}
}

func TestReleaseNotesAreTheNewChangelogBullets(t *testing.T) {
	raw := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/o/r/" + shaCurrent + "/CHANGELOG.md":
			_, _ = io.WriteString(w, "- Old note\n- Shared note\n")
		case "/o/r/" + shaGreen + "/CHANGELOG.md":
			_, _ = io.WriteString(w, "- Old note\n- Shared note\n- New `code` note\n- New `code` note\n- Another note\n")
		case "/o/r/" + shaCurrent + "/CHANGELOG.es.md":
			_, _ = io.WriteString(w, "- Nota vieja\n")
		case "/o/r/" + shaGreen + "/CHANGELOG.es.md":
			_, _ = io.WriteString(w, "- Nota vieja\n- Nota nueva\n")
		default:
			http.NotFound(w, r)
		}
	}
	checker := newNotesChecker(t, newFakeClock(), raw)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	state, ok := checker.State()
	if !ok {
		t.Fatal("no state")
	}
	en := state.Notes["en"]
	if len(en) != 2 || en[0] != "New `code` note" || en[1] != "Another note" {
		t.Errorf("English notes = %q", en)
	}
	es := state.Notes["es"]
	if len(es) != 1 || es[0] != "Nota nueva" {
		t.Errorf("Spanish notes = %q", es)
	}
}

func TestReleaseNotesFetchFailureLeavesThemEmpty(t *testing.T) {
	raw := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}
	checker := newNotesChecker(t, newFakeClock(), raw)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("a notes failure must not fail the check: %v", err)
	}
	state, ok := checker.State()
	if !ok {
		t.Fatal("no state")
	}
	if len(state.Pending) != 1 {
		t.Errorf("pending = %+v", state.Pending)
	}
	if len(state.Notes) != 0 {
		t.Errorf("notes = %+v, want empty", state.Notes)
	}
}

func TestNotModifiedKeepsTheReleaseNotes(t *testing.T) {
	clock := newFakeClock()
	raw := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/o/r/" + shaCurrent + "/CHANGELOG.md":
			_, _ = io.WriteString(w, "- Old\n")
		case "/o/r/" + shaGreen + "/CHANGELOG.md":
			_, _ = io.WriteString(w, "- Old\n- New\n")
		case "/o/r/" + shaCurrent + "/CHANGELOG.es.md":
			_, _ = io.WriteString(w, "- Viejo\n")
		case "/o/r/" + shaGreen + "/CHANGELOG.es.md":
			_, _ = io.WriteString(w, "- Viejo\n- Nuevo\n")
		default:
			http.NotFound(w, r)
		}
	}
	checker := newNotesChecker(t, clock, raw)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	before, _ := checker.State()
	if len(before.Notes["en"]) != 1 || before.Notes["en"][0] != "New" {
		t.Fatalf("notes before the 304 = %+v", before.Notes)
	}

	clock.Advance(2 * time.Minute)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	after, _ := checker.State()
	if len(after.Notes["en"]) != 1 || after.Notes["en"][0] != "New" || len(after.Notes["es"]) != 1 {
		t.Errorf("notes after the 304 = %+v", after.Notes)
	}
}

func TestNotModifiedKeepsTheCachedState(t *testing.T) {
	clock := newFakeClock()
	var (
		mu        sync.Mutex
		headCalls int
		listCalls int
	)
	server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/commits/main":
			mu.Lock()
			headCalls++
			mu.Unlock()
			if r.Header.Get("If-None-Match") == `"head-1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"head-1"`)
			writeJSON(w, map[string]string{"sha": shaGreen})
		case "/repos/o/r/commits":
			mu.Lock()
			listCalls++
			mu.Unlock()
			writeJSON(w, []map[string]string{{"sha": shaGreen}})
		case "/repos/o/r/actions/runs":
			writeJSON(w, map[string]any{"workflow_runs": []map[string]string{
				{"name": "CI", "status": "completed", "conclusion": "success"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))

	checker := newChecker(t, server, shaGreen, clock)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	before, _ := checker.State()
	firstCheck := before.CheckedAt

	clock.Advance(2 * time.Minute)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	after, _ := checker.State()
	if after.Latest != before.Latest || after.CI != before.CI || after.UpdateAvailable != before.UpdateAvailable {
		t.Errorf("state changed on 304: before %+v, after %+v", before, after)
	}
	if !after.CheckedAt.After(firstCheck) {
		t.Errorf("checked_at did not move: %v", after.CheckedAt)
	}

	mu.Lock()
	defer mu.Unlock()
	if headCalls != 2 || listCalls != 1 {
		t.Errorf("head calls = %d, list calls = %d; the cached commits should be kept", headCalls, listCalls)
	}
}

func TestNoGreenCommit(t *testing.T) {
	tests := []struct {
		name string
		runs any
	}{
		{name: "red CI", runs: []map[string]string{{"name": "CI", "status": "completed", "conclusion": "failure"}}},
		{name: "missing runs", runs: []map[string]string{}},
		{name: "another workflow only", runs: []map[string]string{{"name": "Docs", "status": "completed", "conclusion": "success"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := newFakeClock()
			server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/o/r/commits/main":
					writeJSON(w, map[string]string{"sha": shaHead})
				case "/repos/o/r/commits":
					writeJSON(w, []map[string]string{{"sha": shaHead}})
				case "/repos/o/r/actions/runs":
					writeJSON(w, map[string]any{"workflow_runs": tt.runs})
				default:
					http.NotFound(w, r)
				}
			}))

			checker := newChecker(t, server, shaCurrent, clock)
			if err := checker.Refresh(context.Background()); err != nil {
				t.Fatalf("refresh: %v", err)
			}
			state, ok := checker.State()
			if !ok {
				t.Fatal("no state")
			}
			if state.Latest != "" || state.CI != "red" || state.UpdateAvailable {
				t.Errorf("state = %+v, want no offered commit", state)
			}
			if _, ok := checker.LatestGreen(); ok {
				t.Error("LatestGreen() offered a commit")
			}
		})
	}
}

func TestUnknownCurrentCommit(t *testing.T) {
	clock := newFakeClock()
	compareCalls := 0
	server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/commits/main":
			writeJSON(w, map[string]string{"sha": shaGreen})
		case "/repos/o/r/commits":
			writeJSON(w, []map[string]string{{"sha": shaGreen}})
		case "/repos/o/r/actions/runs":
			writeJSON(w, map[string]any{"workflow_runs": []map[string]string{
				{"name": "CI", "status": "completed", "conclusion": "success"},
			}})
		default:
			if strings.HasPrefix(r.URL.Path, "/repos/o/r/compare/") {
				compareCalls++
				writeJSON(w, map[string]any{"commits": []any{}})
				return
			}
			http.NotFound(w, r)
		}
	}))

	checker := newChecker(t, server, "", clock)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	state, _ := checker.State()
	if state.Current != "" || state.Latest != shaGreen || !state.UpdateAvailable || len(state.Pending) != 0 {
		t.Errorf("state = %+v", state)
	}
	if compareCalls != 0 {
		t.Errorf("compare requests = %d, want none without a current commit", compareCalls)
	}
}

func TestRateLimitBacksOffUntilTheReset(t *testing.T) {
	clock := newFakeClock()
	var (
		mu       sync.Mutex
		requests int
		limited  = true
	)
	server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		isLimited := limited
		mu.Unlock()
		if isLimited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(clock.Now().Add(time.Hour).Unix(), 10))
			http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/repos/o/r/commits/main":
			writeJSON(w, map[string]string{"sha": shaGreen})
		case "/repos/o/r/commits":
			writeJSON(w, []map[string]string{{"sha": shaGreen}})
		case "/repos/o/r/actions/runs":
			writeJSON(w, map[string]any{"workflow_runs": []map[string]string{
				{"name": "CI", "status": "completed", "conclusion": "success"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))

	checker := newChecker(t, server, shaGreen, clock)
	if err := checker.Refresh(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("first refresh = %v, want ErrRateLimited", err)
	}
	state, _ := checker.State()
	if state.Error != "rate_limited" {
		t.Errorf("state error = %q, want rate_limited", state.Error)
	}

	clock.Advance(2 * time.Minute)
	if err := checker.Refresh(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("refresh inside the backoff = %v, want ErrRateLimited", err)
	}
	mu.Lock()
	if requests != 1 {
		t.Errorf("requests during the backoff = %d, want 1", requests)
	}
	limited = false
	mu.Unlock()

	clock.Advance(time.Hour)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh after the reset: %v", err)
	}
	state, _ = checker.State()
	if state.Error != "" || state.Latest != shaGreen {
		t.Errorf("state after the reset = %+v", state)
	}
}

func TestBadAnswersAreReported(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"sha":`},
		{name: "oversized", body: strings.Repeat("x", maxBodyBytes+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := newFakeClock()
			server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tt.body)
			}))
			checker := newChecker(t, server, shaCurrent, clock)
			if err := checker.Refresh(context.Background()); err == nil {
				t.Fatal("expected an error")
			}
			state, ok := checker.State()
			if !ok || state.Error != "invalid_response" {
				t.Fatalf("state = %+v, %t", state, ok)
			}
		})
	}
}

func TestUnreachableAPI(t *testing.T) {
	clock := newFakeClock()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()

	checker, err := New(Options{
		Repo:          "o/r",
		APIURL:        server.URL,
		CurrentCommit: shaCurrent,
		Version:       "test",
		Client:        &http.Client{Timeout: time.Second},
		Now:           clock.Now,
		Logger:        testLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := checker.Refresh(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	state, ok := checker.State()
	if !ok || state.Error != "unreachable" {
		t.Fatalf("state = %+v, %t", state, ok)
	}
}

func TestForcedRefreshIsLimitedToOnePerMinute(t *testing.T) {
	clock := newFakeClock()
	var (
		mu       sync.Mutex
		requests int
	)
	server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		switch r.URL.Path {
		case "/repos/o/r/commits/main":
			writeJSON(w, map[string]string{"sha": shaGreen})
		case "/repos/o/r/commits":
			writeJSON(w, []map[string]string{{"sha": shaGreen}})
		case "/repos/o/r/actions/runs":
			writeJSON(w, map[string]any{"workflow_runs": []map[string]string{
				{"name": "CI", "status": "completed", "conclusion": "success"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))

	checker := newChecker(t, server, shaGreen, clock)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	first := requests
	mu.Unlock()

	if err := checker.Refresh(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second refresh = %v, want ErrRateLimited", err)
	}
	mu.Lock()
	if requests != first {
		t.Errorf("requests = %d, want %d", requests, first)
	}
	mu.Unlock()

	clock.Advance(61 * time.Second)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh after a minute: %v", err)
	}
}

func TestNewValidatesItsOptions(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{name: "missing repository", opts: Options{}},
		{name: "repository without owner", opts: Options{Repo: "Talaia"}},
		{name: "repository with a path", opts: Options{Repo: "a/b/c"}},
		{name: "invalid API URL", opts: Options{Repo: "o/r", APIURL: "not a url"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.opts); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	checker, err := New(Options{Repo: "o/r", CurrentCommit: "not-a-sha", Logger: testLogger()})
	if err != nil {
		t.Fatalf("valid options: %v", err)
	}
	if checker.current != "" {
		t.Errorf("current = %q, want an unknown version", checker.current)
	}
}

func TestRequesterWritesTheRequest(t *testing.T) {
	hostDir := helperDir(t)
	requester := NewRequester(hostDir)
	if !requester.Available() {
		t.Fatal("the host helper directory is not available")
	}
	if err := requester.Request(shaCurrent); err != nil {
		t.Fatalf("request: %v", err)
	}

	path := filepath.Join(hostDir, requestsDirectory, requestFileName)
	checkMode(t, filepath.Join(hostDir, requestsDirectory), requestsDirectoryMode)
	checkMode(t, path, requestFileMode)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != `{"target":"`+shaCurrent+`"}` {
		t.Fatalf("request file = %q", got)
	}

	if err := requester.Request(shaCurrent); !errors.Is(err, ErrRequestExists) {
		t.Fatalf("second request = %v, want ErrRequestExists", err)
	}
}

func TestRequesterRefusesWhileAnUpdateRuns(t *testing.T) {
	hostDir := helperDir(t)
	if err := os.WriteFile(filepath.Join(hostDir, statusFileName), []byte(`{"state":"running"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	requester := NewRequester(hostDir)
	if err := requester.Request(shaCurrent); !errors.Is(err, ErrUpdateInProgress) {
		t.Fatalf("request = %v, want ErrUpdateInProgress", err)
	}
}

func TestRequesterNeedsTheHostHelper(t *testing.T) {
	requester := NewRequester(filepath.Join(t.TempDir(), "missing"))
	if requester.Available() {
		t.Fatal("a missing directory was reported as available")
	}
	if err := requester.Request(shaCurrent); !errors.Is(err, ErrHostHelperMissing) {
		t.Fatalf("request = %v, want ErrHostHelperMissing", err)
	}
}

func TestRequesterRejectsInvalidTargets(t *testing.T) {
	hostDir := helperDir(t)
	requester := NewRequester(hostDir)
	for _, target := range []string{"", "abc", strings.Repeat("g", 40), shaCurrent + "0"} {
		if err := requester.Request(target); !errors.Is(err, ErrInvalidTarget) {
			t.Errorf("target %q = %v, want ErrInvalidTarget", target, err)
		}
	}
}

func TestStatus(t *testing.T) {
	long := strings.Repeat("x", maxLogLineBytes+20)
	statusJSON := map[string]any{
		"state":       "succeeded",
		"from":        shaCurrent,
		"to":          shaGreen,
		"started_at":  "2026-10-21T12:00:00Z",
		"finished_at": "2026-10-21T12:05:00Z",
		"log":         append([]string{long}, make([]string, maxLogLines+5)...),
	}
	data, err := json.Marshal(statusJSON)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		write     func(string) error
		wantState string
		check     func(*testing.T, Status)
	}{
		{
			name:      "missing file is idle",
			write:     func(string) error { return nil },
			wantState: "idle",
			check: func(t *testing.T, status Status) {
				if len(status.Log) != 0 {
					t.Errorf("log = %v, want empty", status.Log)
				}
			},
		},
		{
			name:      "malformed file is unknown",
			write:     func(path string) error { return os.WriteFile(path, []byte(`{"state":`), 0o600) },
			wantState: "unknown",
		},
		{
			name:      "a valid file is capped",
			write:     func(path string) error { return os.WriteFile(path, data, 0o600) },
			wantState: "succeeded",
			check: func(t *testing.T, status Status) {
				if status.From != shaCurrent || status.To != shaGreen || status.StartedAt == "" || status.FinishedAt == "" {
					t.Errorf("status = %+v", status)
				}
				if len(status.Log) != maxLogLines {
					t.Errorf("log lines = %d, want %d", len(status.Log), maxLogLines)
				}
				for _, line := range status.Log {
					if len(line) > maxLogLineBytes {
						t.Fatalf("log line of %d bytes", len(line))
					}
				}
			},
		},
		{
			name:      "an unknown state is not trusted",
			write:     func(path string) error { return os.WriteFile(path, []byte(`{"state":"exploded"}`), 0o600) },
			wantState: "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostDir := t.TempDir()
			if err := tt.write(filepath.Join(hostDir, statusFileName)); err != nil {
				t.Fatal(err)
			}
			status := NewRequester(hostDir).Status()
			if status.State != tt.wantState {
				t.Fatalf("state = %q, want %q", status.State, tt.wantState)
			}
			if tt.check != nil {
				tt.check(t, status)
			}
		})
	}
}

func checkMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}

// helperDir returns a host directory as the host helper installer leaves it.
func helperDir(t *testing.T) string {
	t.Helper()
	hostDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(hostDir, requestsDirectory), requestsDirectoryMode); err != nil {
		t.Fatal(err)
	}
	return hostDir
}

func TestRequesterNeedsTheRequestsDirectory(t *testing.T) {
	// Docker creates the bind-mounted host directory empty when the helper was
	// never installed: the core must not offer updates then.
	requester := NewRequester(t.TempDir())
	if requester.Available() {
		t.Fatal("an empty host directory was reported as available")
	}
	if err := requester.Request(shaCurrent); !errors.Is(err, ErrHostHelperMissing) {
		t.Fatalf("request = %v, want ErrHostHelperMissing", err)
	}
}

func TestOlderGreenCommitIsNotOffered(t *testing.T) {
	// The running build is ahead of the newest green commit (its own CI has not
	// finished): offering the green one would be a downgrade.
	clock := newFakeClock()
	server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/commits/main":
			writeJSON(w, map[string]string{"sha": shaCurrent})
		case r.URL.Path == "/repos/o/r/commits":
			writeJSON(w, []map[string]string{{"sha": shaCurrent}, {"sha": shaGreen}})
		case r.URL.Path == "/repos/o/r/actions/runs":
			if r.URL.Query().Get("head_sha") == shaGreen {
				writeJSON(w, map[string]any{"workflow_runs": []map[string]string{
					{"name": "CI", "status": "completed", "conclusion": "success"},
				}})
				return
			}
			writeJSON(w, map[string]any{"workflow_runs": []map[string]string{}})
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/compare/"):
			writeJSON(w, map[string]any{"status": "behind", "commits": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))

	checker := newChecker(t, server, shaCurrent, clock)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	state, _ := checker.State()
	if state.UpdateAvailable || len(state.Pending) != 0 {
		t.Fatalf("an older commit was offered: %+v", state)
	}
}

func TestHeadIsOfferedOnceItsCIFinishes(t *testing.T) {
	// The first check runs while the CI of the head is still running. The ETag
	// must not be kept then: a 304 would hide the head once its CI is green.
	clock := newFakeClock()
	var (
		mu       sync.Mutex
		headDone bool
	)
	server := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		done := headDone
		mu.Unlock()
		switch {
		case r.URL.Path == "/repos/o/r/commits/main":
			if r.Header.Get("If-None-Match") == `"head-1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"head-1"`)
			writeJSON(w, map[string]string{"sha": shaHead})
		case r.URL.Path == "/repos/o/r/commits":
			writeJSON(w, []map[string]string{{"sha": shaHead}, {"sha": shaGreen}})
		case r.URL.Path == "/repos/o/r/actions/runs":
			if r.URL.Query().Get("head_sha") == shaGreen || done {
				writeJSON(w, map[string]any{"workflow_runs": []map[string]string{
					{"name": "CI", "status": "completed", "conclusion": "success"},
				}})
				return
			}
			writeJSON(w, map[string]any{"workflow_runs": []map[string]string{}})
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/compare/"):
			writeJSON(w, map[string]any{"status": "ahead", "commits": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))

	checker := newChecker(t, server, shaCurrent, clock)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if state, _ := checker.State(); state.Latest != shaGreen {
		t.Fatalf("first check offered %q, want the older green commit", state.Latest)
	}

	mu.Lock()
	headDone = true
	mu.Unlock()
	clock.Advance(2 * time.Minute)
	if err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if state, _ := checker.State(); state.Latest != shaHead {
		t.Fatalf("second check offered %q, want the head once its CI is green", state.Latest)
	}
}

func TestRequesterReportsAStuckRequest(t *testing.T) {
	hostDir := helperDir(t)
	requester := NewRequester(hostDir)
	if err := requester.Request(shaCurrent); err != nil {
		t.Fatalf("request: %v", err)
	}
	if !requester.Available() {
		t.Fatal("a fresh request made the helper unavailable")
	}

	// Nobody took the request for ten minutes: the helper is not running.
	path := filepath.Join(hostDir, requestsDirectory, requestFileName)
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if requester.Available() {
		t.Fatal("a request waiting for ten minutes was not reported")
	}
	if err := requester.Request(shaCurrent); !errors.Is(err, ErrHostHelperMissing) {
		t.Fatalf("request = %v, want ErrHostHelperMissing", err)
	}
}
