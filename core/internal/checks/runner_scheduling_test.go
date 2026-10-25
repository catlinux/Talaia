package checks

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/netguard"
	"github.com/catlinux/Talaia/core/internal/store"
)

type fakeTicker struct{ ch chan time.Time }

func (t *fakeTicker) C() <-chan time.Time { return t.ch }
func (t *fakeTicker) Stop()               {}

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *fakeClock) NewTicker(time.Duration) Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTicker{ch: make(chan time.Time, 16)}
	c.tickers = append(c.tickers, t)
	return t
}
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	tickers := append([]*fakeTicker(nil), c.tickers...)
	c.mu.Unlock()
	for _, ticker := range tickers {
		select {
		case ticker.ch <- now:
		default:
		}
	}
}

func waitClockTickers(t *testing.T, clock *fakeClock, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		clock.mu.Lock()
		got := len(clock.tickers)
		clock.mu.Unlock()
		if got >= count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("registered tickers=%d, want %d", got, count)
		}
		time.Sleep(time.Millisecond)
	}
}

type schedulingStore struct {
	mu       sync.Mutex
	checks   []store.AlertCheck
	results  []store.AlertCheckResult
	cutoffs  []time.Time
	private  bool
	changed  chan struct{}
	failList bool
}

func newSchedulingStore(checks ...store.AlertCheck) *schedulingStore {
	return &schedulingStore{checks: checks, private: true, changed: make(chan struct{}, 64)}
}
func (s *schedulingStore) ListEnabledAlertChecks(context.Context) ([]store.AlertCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failList {
		return nil, errors.New("database unavailable")
	}
	return append([]store.AlertCheck(nil), s.checks...), nil
}
func (s *schedulingStore) InsertAlertCheckResult(_ context.Context, result store.AlertCheckResult) error {
	s.mu.Lock()
	s.results = append(s.results, result)
	s.mu.Unlock()
	s.changed <- struct{}{}
	return nil
}
func (s *schedulingStore) DeleteCheckResultsBefore(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	s.cutoffs = append(s.cutoffs, before)
	s.mu.Unlock()
	return 0, nil
}
func (s *schedulingStore) GetAllowPrivateTargets(context.Context) (bool, error) {
	return s.private, nil
}
func (s *schedulingStore) replace(checks ...store.AlertCheck) {
	s.mu.Lock()
	s.checks = append([]store.AlertCheck(nil), checks...)
	s.mu.Unlock()
}
func (s *schedulingStore) snapshot() []store.AlertCheckResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.AlertCheckResult(nil), s.results...)
}

type fakeRoundTripper struct {
	started chan string
	release <-chan struct{}
	active  atomic.Int32
	maximum atomic.Int32
	byCheck sync.Map
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	active := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		old := f.maximum.Load()
		if active <= old || f.maximum.CompareAndSwap(old, active) {
			break
		}
	}
	checkID := strings.TrimPrefix(req.URL.Path, "/")
	if checkID == "" {
		checkID = "default"
	}
	var initial atomic.Int32
	value, _ := f.byCheck.LoadOrStore(checkID, &initial)
	perCheck := value.(*atomic.Int32).Add(1)
	if perCheck > 1 {
		return nil, errOverlappingCheck
	}
	defer value.(*atomic.Int32).Add(-1)
	if f.started != nil {
		f.started <- checkID
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

type overlapError struct{}

func (*overlapError) Error() string { return "overlapping check execution" }

var errOverlappingCheck = &overlapError{}

func fakeClientFactory(rt http.RoundTripper) ClientFactory {
	return func(_ netguard.HTTPClientOptions, _ *x509.CertPool) *http.Client {
		return &http.Client{Transport: rt}
	}
}

func schedulingCheck(id string, interval int) store.AlertCheck {
	return store.AlertCheck{ID: id, Kind: "http", Target: "http://example.com/" + id, Config: json.RawMessage(`{}`), IntervalSeconds: interval, Enabled: true}
}

func startRunner(t *testing.T, runner *Runner) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	return cancel, done
}

func waitResults(t *testing.T, s *schedulingStore, count int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for len(s.snapshot()) < count {
		select {
		case <-s.changed:
		case <-deadline:
			t.Fatalf("got %d results, want %d", len(s.snapshot()), count)
		}
	}
}

func waitCutoffs(t *testing.T, s *schedulingStore, count int) []time.Time {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		s.mu.Lock()
		cutoffs := append([]time.Time(nil), s.cutoffs...)
		s.mu.Unlock()
		if len(cutoffs) >= count {
			return cutoffs
		}
		select {
		case <-deadline:
			t.Fatalf("got %d purges, want %d", len(cutoffs), count)
		case <-time.After(time.Millisecond):
		}
	}
}

func stopRunner(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not stop")
	}
}

func TestRunnerSchedulingIntervalsAndCallbacks(t *testing.T) {
	tests := []struct {
		name     string
		interval int
		early    time.Duration
	}{
		{name: "minimum", interval: 10, early: 29 * time.Second},
		{name: "default", interval: 0, early: 59 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newFakeClock()
			storeFake := newSchedulingStore(schedulingCheck("check", test.interval))
			rt := &fakeRoundTripper{}
			var callbacks atomic.Int32
			callbackDone := make(chan struct{}, 2)
			runner := New(Options{Store: storeFake, Clock: clock, ClientFactory: fakeClientFactory(rt), OnResult: func(store.AlertCheckResult) {
				callbacks.Add(1)
				callbackDone <- struct{}{}
			}})
			cancel, done := startRunner(t, runner)
			defer stopRunner(t, cancel, done)
			waitClockTickers(t, clock, 2)
			clock.advance(time.Second)
			waitResults(t, storeFake, 1)
			clock.advance(test.early)
			time.Sleep(10 * time.Millisecond)
			if got := len(storeFake.snapshot()); got != 1 {
				t.Fatalf("early result count=%d", got)
			}
			clock.advance(time.Second)
			waitResults(t, storeFake, 2)
			for i := 0; i < 2; i++ {
				select {
				case <-callbackDone:
				case <-time.After(time.Second):
					t.Fatal("OnResult was not called for each stored result")
				}
			}
			if callbacks.Load() != 2 {
				t.Fatalf("callbacks=%d", callbacks.Load())
			}
		})
	}
}

func TestRunnerNeverOverlapsAndLimitsConcurrency(t *testing.T) {
	clock := newFakeClock()
	checks := make([]store.AlertCheck, 20)
	for i := range checks {
		checks[i] = schedulingCheck(strconv.Itoa(i), 30)
	}
	storeFake := newSchedulingStore(checks...)
	release := make(chan struct{}, 40)
	rt := &fakeRoundTripper{started: make(chan string, 40), release: release}
	runner := New(Options{Store: storeFake, Clock: clock, ClientFactory: fakeClientFactory(rt)})
	cancel, done := startRunner(t, runner)
	defer stopRunner(t, cancel, done)
	waitClockTickers(t, clock, 2)
	clock.advance(time.Second)
	for i := 0; i < 8; i++ {
		select {
		case <-rt.started:
		case <-time.After(time.Second):
			t.Fatal("fewer than 8 checks started")
		}
	}
	for i := 0; i < 5; i++ {
		clock.advance(time.Second)
	}
	if got := rt.active.Load(); got != 8 {
		t.Fatalf("active checks=%d, want 8", got)
	}
	for i := 0; i < 8; i++ {
		release <- struct{}{}
	}
	waitResults(t, storeFake, 8)
	for i := 0; i < 8; i++ {
		clock.advance(time.Second)
		select {
		case <-rt.started:
		case <-time.After(time.Second):
			t.Fatal("next checks did not start")
		}
	}
	if got := rt.maximum.Load(); got > 8 {
		t.Fatalf("maximum concurrency=%d", got)
	}
	for i := 0; i < 8; i++ {
		release <- struct{}{}
	}
}

func TestRunnerDoesNotOverlapAnIndividualCheck(t *testing.T) {
	clock := newFakeClock()
	storeFake := newSchedulingStore(schedulingCheck("single", 30))
	release := make(chan struct{}, 1)
	rt := &fakeRoundTripper{started: make(chan string, 4), release: release}
	runner := New(Options{Store: storeFake, Clock: clock, ClientFactory: fakeClientFactory(rt)})
	cancel, done := startRunner(t, runner)
	defer stopRunner(t, cancel, done)
	waitClockTickers(t, clock, 2)
	clock.advance(time.Second)
	select {
	case <-rt.started:
	case <-time.After(time.Second):
		t.Fatal("check did not start")
	}
	for i := 0; i < 40; i++ {
		clock.advance(time.Second)
	}
	select {
	case id := <-rt.started:
		t.Fatalf("overlapping execution started for %q", id)
	case <-time.After(10 * time.Millisecond):
	}
	if got := rt.active.Load(); got != 1 {
		t.Fatalf("active executions=%d, want 1", got)
	}
	release <- struct{}{}
	waitResults(t, storeFake, 1)
}

func TestRunnerReloadAndDailyPurge(t *testing.T) {
	clock := newFakeClock()
	storeFake := newSchedulingStore(schedulingCheck("old", 30))
	rt := &fakeRoundTripper{started: make(chan string, 10)}
	runner := New(Options{Store: storeFake, Clock: clock, ClientFactory: fakeClientFactory(rt)})
	cancel, done := startRunner(t, runner)
	defer stopRunner(t, cancel, done)
	waitClockTickers(t, clock, 2)
	clock.advance(time.Second)
	waitResults(t, storeFake, 1)
	storeFake.replace(schedulingCheck("new", 30))
	runner.Reload()
	clock.advance(time.Second)
	deadline := time.After(time.Second)
	for {
		select {
		case id := <-rt.started:
			if id == "new" {
				goto reloaded
			}
		case <-deadline:
			t.Fatal("new check was not run after Reload")
		}
	}
reloaded:
	waitResults(t, storeFake, 2)
	clock.advance(24 * time.Hour)
	waitResults(t, storeFake, 3)
	cutoffs := waitCutoffs(t, storeFake, 2)
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Add(-30 * 24 * time.Hour); !cutoffs[0].Equal(want) {
		t.Fatalf("initial purge cutoff=%s, want %s", cutoffs[0], want)
	}
	if want := clock.Now().Add(-30 * 24 * time.Hour); !cutoffs[len(cutoffs)-1].Equal(want) {
		t.Fatalf("purge cutoff=%s, want %s", cutoffs[len(cutoffs)-1], want)
	}
	clock.advance(30 * time.Second)
	waitResults(t, storeFake, 4)
	oldResults := 0
	for _, result := range storeFake.snapshot() {
		if result.CheckID == "old" {
			oldResults++
		}
	}
	if oldResults != 1 {
		t.Fatalf("disabled check result count=%d, want initial run only", oldResults)
	}
}

func TestRunnerKeepsRunningWhenAReloadFails(t *testing.T) {
	clock := newFakeClock()
	storeFake := newSchedulingStore(schedulingCheck("kept", 30))
	rt := &fakeRoundTripper{started: make(chan string, 10)}
	runner := New(Options{Store: storeFake, Clock: clock, ClientFactory: fakeClientFactory(rt)})
	cancel, done := startRunner(t, runner)
	defer stopRunner(t, cancel, done)
	waitClockTickers(t, clock, 2)
	clock.advance(time.Second)
	waitResults(t, storeFake, 1)
	storeFake.mu.Lock()
	storeFake.failList = true
	storeFake.mu.Unlock()
	runner.Reload()
	clock.advance(30 * time.Second)
	waitResults(t, storeFake, 2)
	select {
	case err := <-done:
		t.Fatalf("runner stopped after a failed reload: %v", err)
	default:
	}
}
