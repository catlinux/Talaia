// Package updates detects the newest commit of the repository whose CI is
// green, reports the commits pending against the running build, and queues an
// update request for the host helper.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultRepo is the GitHub repository checked for updates.
	DefaultRepo = "catlinux/Talaia"
	// DefaultAPIURL is the public GitHub API.
	DefaultAPIURL = "https://api.github.com"
	// DefaultInterval is how often the checker asks GitHub.
	DefaultInterval = 30 * time.Minute

	maxCommits            = 20
	maxPending            = 50
	maxBodyBytes          = 1 << 20
	maxStatusBytes        = 1 << 20
	requestTimeout        = 10 * time.Second
	forcedRefreshAfter    = time.Minute
	shortSHALength        = 7
	maxSubjectBytes       = 500
	maxLogLines           = 50
	maxLogLineBytes       = 500
	requestsDirectory     = "requests"
	requestFileName       = "update.json"
	statusFileName        = "status.json"
	requestsDirectoryMode = 0o750
	requestFileMode       = 0o640
	userAgentPrefix       = "talaia-core/"
	ciWorkflow            = "ci"
)

// ErrRateLimited is returned while GitHub's rate limit has not reset.
var ErrRateLimited = errors.New("the GitHub rate limit is exhausted")

// ErrHostHelperMissing is returned when the directory shared with the host
// helper does not exist.
var ErrHostHelperMissing = errors.New("the host helper directory does not exist")

// ErrRequestExists is returned when an update request is already queued.
var ErrRequestExists = errors.New("an update request is already queued")

// ErrUpdateInProgress is returned when the host helper is running an update.
var ErrUpdateInProgress = errors.New("an update is already running")

// ErrInvalidTarget is returned for a target that is not a 40 character SHA.
var ErrInvalidTarget = errors.New("invalid update target")

var (
	errUnreachable     = errors.New("the GitHub API is unreachable")
	errInvalidResponse = errors.New("the GitHub API answer is not valid")

	repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	shaPattern  = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
)

// Pending is one commit between the running build and the newest green one.
type Pending struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
}

// State is what the panel knows about the updates of the running build.
type State struct {
	// Current is the commit of the running binary; empty when the build did not
	// stamp it.
	Current string `json:"current"`
	// Latest is the newest commit on main whose CI is green.
	Latest string `json:"latest"`
	// Pending are the commits between Current and Latest, in the order GitHub
	// returns them.
	Pending []Pending `json:"pending"`
	// CI is "green" for the offered commit and "red" when no commit has a green
	// CI yet.
	CI string `json:"ci"`
	// CheckedAt is when the last check finished, successfully or not.
	CheckedAt time.Time `json:"checked_at"`
	// Error is a stable code such as rate_limited, unreachable or
	// invalid_response; empty after a successful check.
	Error string `json:"error"`
	// UpdateAvailable is true when there is a green commit different from the
	// running one.
	UpdateAvailable bool `json:"update_available"`
}

// Status is the state the host helper reports in status.json.
type Status struct {
	State      string   `json:"state"`
	From       string   `json:"from"`
	To         string   `json:"to"`
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at"`
	Log        []string `json:"log"`
}

// Options configures a Checker.
type Options struct {
	// Repo is the GitHub repository, owner/name.
	Repo string
	// APIURL is the GitHub API base; empty means DefaultAPIURL.
	APIURL string
	// CurrentCommit is the commit stamped into the binary; empty means unknown.
	CurrentCommit string
	// Version feeds the User-Agent.
	Version string
	// Interval is how often the checker runs; empty means DefaultInterval.
	Interval time.Duration
	// Client is the HTTP client; empty means one with a 10 second timeout.
	Client *http.Client
	// Now defaults to time.Now.
	Now func() time.Time
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// Checker asks GitHub for the newest commit with a green CI and caches the
// answer. It never blocks a request and never crashes on a network error.
type Checker struct {
	repo      string
	apiURL    string
	current   string
	userAgent string
	interval  time.Duration
	client    *http.Client
	now       func() time.Time
	logger    *slog.Logger

	refreshMu sync.Mutex

	mu           sync.Mutex
	state        State
	haveState    bool
	etag         string
	backoffUntil time.Time
	lastForced   time.Time
}

// New creates a checker. It does not contact GitHub.
func New(opts Options) (*Checker, error) {
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		return nil, errors.New("an update repository is required")
	}
	if !repoPattern.MatchString(repo) {
		return nil, fmt.Errorf("invalid update repository %q (expected owner/name)", opts.Repo)
	}

	apiURL := strings.TrimRight(strings.TrimSpace(opts.APIURL), "/")
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid update API URL %q", opts.APIURL)
	}

	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "dev"
	}

	current := strings.ToLower(strings.TrimSpace(opts.CurrentCommit))
	if current != "" && !shaPattern.MatchString(current) {
		logger.Warn("ignoring the build commit: it is not a SHA", "commit", opts.CurrentCommit)
		current = ""
	}

	return &Checker{
		repo:      repo,
		apiURL:    apiURL,
		current:   current,
		userAgent: userAgentPrefix + version,
		interval:  interval,
		client:    client,
		now:       now,
		logger:    logger,
	}, nil
}

// Start runs a first check and then one every interval, backing off until the
// GitHub rate limit resets. It returns immediately and stops with ctx.
func (c *Checker) Start(ctx context.Context) {
	go func() {
		if err := c.refresh(ctx, false); err != nil {
			c.logger.Warn("update check failed", "error", err)
		}
		for {
			wait := c.interval
			c.mu.Lock()
			if remaining := c.backoffUntil.Sub(c.now()); remaining > 0 {
				wait = remaining
			}
			c.mu.Unlock()
			if wait < time.Second {
				wait = time.Second
			}

			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if err := c.refresh(ctx, false); err != nil {
				c.logger.Warn("update check failed", "error", err)
			}
		}
	}()
}

// Refresh asks GitHub now. At most one forced refresh runs per minute; the
// background checks are not limited by that.
func (c *Checker) Refresh(ctx context.Context) error {
	return c.refresh(ctx, true)
}

// State returns the cached state and whether a check has already finished.
func (c *Checker) State() (State, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.haveState {
		return State{}, false
	}
	state := c.state
	state.Pending = append([]Pending{}, c.state.Pending...)
	return state, true
}

// LatestGreen returns the commit the panel currently offers for an update.
func (c *Checker) LatestGreen() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.haveState || c.state.CI != "green" || c.state.Latest == "" {
		return "", false
	}
	return c.state.Latest, true
}

func (c *Checker) refresh(ctx context.Context, forced bool) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	now := c.now()
	c.mu.Lock()
	if now.Before(c.backoffUntil) {
		c.mu.Unlock()
		return ErrRateLimited
	}
	if forced {
		if now.Sub(c.lastForced) < forcedRefreshAfter {
			c.mu.Unlock()
			return ErrRateLimited
		}
		c.lastForced = now
	}
	etag := c.etag
	c.mu.Unlock()

	return c.check(ctx, etag)
}

func (c *Checker) check(ctx context.Context, etag string) error {
	at := c.now()
	head, nextETag, notModified, err := c.head(ctx, etag)
	if err != nil {
		c.fail(at, err)
		return err
	}
	if notModified {
		// The head did not move: keep the cached commits and CI answer.
		c.mu.Lock()
		if !c.haveState {
			c.state = State{Current: c.current, Pending: []Pending{}}
			c.haveState = true
		}
		c.state.CheckedAt = at
		c.state.Error = ""
		c.mu.Unlock()
		return nil
	}

	latest, green, err := c.findGreen(ctx)
	if err != nil {
		c.fail(at, err)
		return err
	}

	state := State{
		Current:   c.current,
		Latest:    latest,
		Pending:   []Pending{},
		CI:        "red",
		CheckedAt: at,
	}
	if green {
		state.CI = "green"
		state.UpdateAvailable = latest != c.current
		if c.current != "" && c.current != latest {
			pending, ahead, err := c.pending(ctx, c.current, latest)
			switch {
			case err != nil:
				// The offer is still useful without the list of commits; the host
				// helper refuses anything that is not newer anyway.
				c.logger.Warn("could not list the pending commits", "error", err)
			case !ahead:
				// The running build is newer than, or off, the newest green commit:
				// going there would be a downgrade.
				state.UpdateAvailable = false
			default:
				state.Pending = pending
			}
		}
	}

	c.mu.Lock()
	c.state = state
	c.haveState = true
	// A 304 later reuses this answer, so the ETag is only kept when the head
	// itself is the green commit. While its CI is still running, every check
	// looks again, or the head would never be offered until the next push.
	c.etag = ""
	if green && latest == head {
		c.etag = nextETag
	}
	c.mu.Unlock()
	return nil
}

func (c *Checker) fail(at time.Time, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.haveState {
		c.state = State{Current: c.current, Pending: []Pending{}}
		c.haveState = true
	}
	c.state.CheckedAt = at
	c.state.Error = errorCode(err)
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, errInvalidResponse):
		return "invalid_response"
	default:
		return "unreachable"
	}
}

func (c *Checker) head(ctx context.Context, etag string) (sha, nextETag string, notModified bool, err error) {
	endpoint := c.apiURL + "/repos/" + c.repo + "/commits/main"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: %w", errUnreachable, err)
	}
	c.setHeaders(request)
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: %w", errUnreachable, err)
	}
	defer func() { _ = response.Body.Close() }()

	if err := c.observeRateLimit(response); err != nil {
		return "", "", false, err
	}
	if response.StatusCode == http.StatusNotModified {
		return "", "", true, nil
	}
	if response.StatusCode != http.StatusOK {
		return "", "", false, fmt.Errorf("%w: status %d", errUnreachable, response.StatusCode)
	}

	var body struct {
		SHA string `json:"sha"`
	}
	if err := decodeLimited(response.Body, &body); err != nil {
		return "", "", false, err
	}
	if !shaPattern.MatchString(body.SHA) {
		return "", "", false, fmt.Errorf("%w: the head commit has no SHA", errInvalidResponse)
	}
	return strings.ToLower(body.SHA), response.Header.Get("ETag"), false, nil
}

func (c *Checker) findGreen(ctx context.Context) (string, bool, error) {
	var commits []struct {
		SHA string `json:"sha"`
	}
	endpoint := c.apiURL + "/repos/" + c.repo + "/commits?sha=main&per_page=" + strconv.Itoa(maxCommits)
	if err := c.get(ctx, endpoint, &commits); err != nil {
		return "", false, err
	}

	// The list starts at the head and walks back; at most one page is read.
	for _, commit := range commits {
		if !shaPattern.MatchString(commit.SHA) {
			continue
		}
		green, err := c.green(ctx, commit.SHA)
		if err != nil {
			return "", false, err
		}
		if green {
			return strings.ToLower(commit.SHA), true, nil
		}
	}
	return "", false, nil
}

func (c *Checker) green(ctx context.Context, sha string) (bool, error) {
	var body struct {
		WorkflowRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"workflow_runs"`
	}
	endpoint := c.apiURL + "/repos/" + c.repo + "/actions/runs?head_sha=" + sha + "&status=completed"
	if err := c.get(ctx, endpoint, &body); err != nil {
		return false, err
	}

	found := false
	for _, run := range body.WorkflowRuns {
		if !strings.EqualFold(run.Name, ciWorkflow) {
			continue
		}
		if !strings.EqualFold(run.Status, "completed") {
			continue
		}
		found = true
		if !strings.EqualFold(run.Conclusion, "success") {
			return false, nil
		}
	}
	return found, nil
}

// pending lists the commits from current to latest and reports whether latest
// is ahead of current, as GitHub's comparison says.
func (c *Checker) pending(ctx context.Context, current, latest string) ([]Pending, bool, error) {
	if !shaPattern.MatchString(current) || !shaPattern.MatchString(latest) {
		return nil, false, fmt.Errorf("%w: invalid comparison", errInvalidResponse)
	}
	endpoint := c.apiURL + "/repos/" + c.repo + "/compare/" +
		strings.ToLower(current) + "..." + strings.ToLower(latest)

	var body struct {
		Status  string `json:"status"`
		Commits []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
				Author  struct {
					Name string `json:"name"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"commits"`
	}
	if err := c.get(ctx, endpoint, &body); err != nil {
		return nil, false, err
	}
	if body.Status != "ahead" {
		return nil, false, nil
	}

	pending := make([]Pending, 0, min(len(body.Commits), maxPending))
	for _, commit := range body.Commits {
		if len(pending) >= maxPending {
			break
		}
		pending = append(pending, Pending{
			SHA:     shortSHA(commit.SHA),
			Subject: summarize(commit.Commit.Message),
			Author:  commit.Commit.Author.Name,
		})
	}
	return pending, true, nil
}

func (c *Checker) get(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", errUnreachable, err)
	}
	c.setHeaders(request)

	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %w", errUnreachable, err)
	}
	defer func() { _ = response.Body.Close() }()

	if err := c.observeRateLimit(response); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", errUnreachable, response.StatusCode)
	}
	return decodeLimited(response.Body, target)
}

func (c *Checker) setHeaders(request *http.Request) {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", c.userAgent)
}

// observeRateLimit records when GitHub's budget runs out and turns the answer
// into ErrRateLimited. It only acts on the documented signal: 403 or 429 with
// no requests left.
func (c *Checker) observeRateLimit(response *http.Response) error {
	if response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	if response.Header.Get("X-RateLimit-Remaining") != "0" {
		return nil
	}

	until := c.now().Add(time.Hour)
	if value := response.Header.Get("X-RateLimit-Reset"); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			until = time.Unix(seconds, 0)
		}
	}
	c.mu.Lock()
	c.backoffUntil = until
	c.mu.Unlock()
	return ErrRateLimited
}

func decodeLimited(body io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidResponse, err)
	}
	if len(data) > maxBodyBytes {
		return fmt.Errorf("%w: the answer is larger than %d bytes", errInvalidResponse, maxBodyBytes)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%w: %w", errInvalidResponse, err)
	}
	return nil
}

func shortSHA(sha string) string {
	sha = strings.ToLower(sha)
	if len(sha) > shortSHALength {
		return sha[:shortSHALength]
	}
	return sha
}

// summarize returns the first line of a commit message.
func summarize(message string) string {
	subject, _, _ := strings.Cut(message, "\n")
	return truncateBytes(strings.TrimSpace(subject), maxSubjectBytes)
}

func truncateBytes(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return strings.ToValidUTF8(value[:max], "")
}

// Requester writes the update request for the host helper and reads its status.
type Requester struct {
	hostDir string
	mu      sync.Mutex
}

// NewRequester creates the client of the host helper directory.
func NewRequester(hostDir string) *Requester {
	return &Requester{hostDir: hostDir}
}

// Available reports whether the host helper is installed: its installer creates
// the requests directory for the core. Docker creates an empty host directory
// on its own when the helper is missing, so that alone proves nothing.
func (r *Requester) Available() bool {
	info, err := os.Lstat(filepath.Join(r.hostDir, requestsDirectory))
	return err == nil && info.IsDir()
}

// Request queues an update to target. The helper watches the request file; the
// core only ever writes a validated SHA.
func (r *Requester) Request(target string) error {
	target = strings.ToLower(strings.TrimSpace(target))
	if !shaPattern.MatchString(target) {
		return ErrInvalidTarget
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.Available() {
		return ErrHostHelperMissing
	}
	if r.Status().State == "running" {
		return ErrUpdateInProgress
	}

	directory := filepath.Join(r.hostDir, requestsDirectory)

	path := filepath.Join(directory, requestFileName)
	if _, err := os.Stat(path); err == nil {
		return ErrRequestExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	payload, err := json.Marshal(struct {
		Target string `json:"target"`
	}{Target: target})
	if err != nil {
		return fmt.Errorf("encode the update request: %w", err)
	}

	// Write a temporary file and link it into place: the helper never sees a
	// half written request, and the link fails if another request got there
	// first.
	temporary, err := os.CreateTemp(directory, ".update-*")
	if err != nil {
		return fmt.Errorf("create the update request: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()

	if err := temporary.Chmod(requestFileMode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set the update request mode: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write the update request: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close the update request: %w", err)
	}
	if err := os.Link(temporaryName, path); err != nil {
		if os.IsExist(err) {
			return ErrRequestExists
		}
		return fmt.Errorf("publish the update request: %w", err)
	}
	return nil
}

// Status reads the state the host helper reports. A missing file is idle; an
// unreadable or malformed one is unknown.
func (r *Requester) Status() Status {
	path := filepath.Join(r.hostDir, statusFileName)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Status{State: "idle", Log: []string{}}
		}
		return Status{State: "unknown", Log: []string{}}
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxStatusBytes+1))
	if err != nil || len(data) > maxStatusBytes {
		return Status{State: "unknown", Log: []string{}}
	}

	var raw struct {
		State      string   `json:"state"`
		From       string   `json:"from"`
		To         string   `json:"to"`
		StartedAt  string   `json:"started_at"`
		FinishedAt string   `json:"finished_at"`
		Log        []string `json:"log"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Status{State: "unknown", Log: []string{}}
	}

	log := make([]string, 0, min(len(raw.Log), maxLogLines))
	for _, line := range raw.Log {
		if len(log) >= maxLogLines {
			break
		}
		log = append(log, truncateBytes(line, maxLogLineBytes))
	}

	return Status{
		State:      normalizeState(raw.State),
		From:       raw.From,
		To:         raw.To,
		StartedAt:  raw.StartedAt,
		FinishedAt: raw.FinishedAt,
		Log:        log,
	}
}

func normalizeState(state string) string {
	normalized := strings.ToLower(strings.TrimSpace(state))
	switch normalized {
	case "":
		return "idle"
	case "idle", "running", "succeeded", "failed", "rolled_back":
		return normalized
	default:
		return "unknown"
	}
}
