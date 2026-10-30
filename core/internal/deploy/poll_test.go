package deploy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

type fakePollApps struct {
	apps []store.App
	err  error
}

func (f fakePollApps) ListPollableApps(context.Context) ([]store.App, error) {
	return f.apps, f.err
}

type fakeProber struct {
	mu    sync.Mutex
	calls []string
	probe func(ctx context.Context, app store.App) (string, error)
}

func (f *fakeProber) Probe(ctx context.Context, app store.App) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, app.ID)
	f.mu.Unlock()
	return f.probe(ctx, app)
}

type enqueueCall struct {
	appID   string
	trigger string
	commit  string
}

type fakeEnqueuer struct {
	mu    sync.Mutex
	calls []enqueueCall
	err   error
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, appID, trigger, commit string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, enqueueCall{appID: appID, trigger: trigger, commit: commit})
	return f.err
}

func pollApp(id, serverID string) store.App {
	return store.App{
		ID: id, ServerID: serverID, Source: store.AppSourceGit, Branch: "main",
		AutoDeploy: true, PollEnabled: true,
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPollOnceRecordsThenEnqueuesOnlyOnChanges(t *testing.T) {
	app := pollApp("app-1", "server-1")
	commits := []string{"commit-1", "commit-1", "commit-2", "commit-2"}
	index := 0
	prober := &fakeProber{probe: func(context.Context, store.App) (string, error) {
		commit := commits[index]
		index++
		return commit, nil
	}}
	enqueue := &fakeEnqueuer{}
	poller := NewPoller(PollerDeps{
		Apps: fakePollApps{apps: []store.App{app}}, Prober: prober, Enqueue: enqueue,
		Logger: discardLogger(),
	})
	ctx := context.Background()

	poller.pollOnce(ctx)
	poller.pollOnce(ctx)
	if len(enqueue.calls) != 0 {
		t.Fatalf("first observations enqueued: %+v", enqueue.calls)
	}
	poller.pollOnce(ctx)
	if len(enqueue.calls) != 1 {
		t.Fatalf("changed commit enqueued %d times, want 1", len(enqueue.calls))
	}
	if call := enqueue.calls[0]; call.appID != app.ID || call.trigger != "poll" || call.commit != "commit-2" {
		t.Fatalf("enqueue call = %+v", call)
	}
	poller.pollOnce(ctx)
	if len(enqueue.calls) != 1 {
		t.Fatalf("the same commit enqueued again: %+v", enqueue.calls)
	}
}

func TestPollOnceSkipsAppsThatDoNotOptIn(t *testing.T) {
	apps := []store.App{
		pollApp("enabled", "server-1"),
		{ID: "no-poll", ServerID: "server-1", Source: store.AppSourceGit, AutoDeploy: true},
		{ID: "no-auto", ServerID: "server-1", Source: store.AppSourceGit, PollEnabled: true},
		{ID: "image", ServerID: "server-1", Source: store.AppSourceImage, AutoDeploy: true, PollEnabled: true},
	}
	prober := &fakeProber{probe: func(context.Context, store.App) (string, error) { return "commit", nil }}
	poller := NewPoller(PollerDeps{
		Apps: fakePollApps{apps: apps}, Prober: prober, Enqueue: &fakeEnqueuer{}, Logger: discardLogger(),
	})
	poller.pollOnce(context.Background())
	if len(prober.calls) != 1 || prober.calls[0] != "enabled" {
		t.Fatalf("probed apps = %v, want only enabled", prober.calls)
	}
}

func TestPollOnceProbeErrorDoesNotStopTheOtherApps(t *testing.T) {
	apps := []store.App{pollApp("broken", "server-1"), pollApp("healthy", "server-1")}
	prober := &fakeProber{probe: func(_ context.Context, app store.App) (string, error) {
		if app.ID == "broken" {
			return "", errors.New("probe failed")
		}
		return "commit", nil
	}}
	poller := NewPoller(PollerDeps{
		Apps: fakePollApps{apps: apps}, Prober: prober, Enqueue: &fakeEnqueuer{}, Logger: discardLogger(),
	})
	poller.pollOnce(context.Background())
	if len(prober.calls) != 2 {
		t.Fatalf("probed apps = %v, want both", prober.calls)
	}
}

func TestRunUsesTheTickerSurvivesErrorsAndStopsOnCancel(t *testing.T) {
	app := pollApp("app-1", "server-1")
	probed := make(chan int, 4)
	var mu sync.Mutex
	calls := 0
	prober := &fakeProber{probe: func(context.Context, store.App) (string, error) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		probed <- call
		if call == 1 {
			return "", errors.New("transient failure")
		}
		return "commit-1", nil
	}}
	ticks := make(chan time.Time, 1)
	stopped := make(chan struct{}, 1)
	var interval time.Duration
	poller := NewPoller(PollerDeps{
		Apps:     fakePollApps{apps: []store.App{app}},
		Prober:   prober,
		Enqueue:  &fakeEnqueuer{},
		Interval: 17 * time.Second,
		Logger:   discardLogger(),
		Ticker: func(got time.Duration) (<-chan time.Time, func()) {
			interval = got
			return ticks, func() { stopped <- struct{}{} }
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	if call := <-probed; call != 1 {
		t.Fatalf("first probe call = %d", call)
	}
	ticks <- time.Now()
	if call := <-probed; call != 2 {
		t.Fatalf("second probe call = %d", call)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop its ticker")
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls < 2 {
		t.Fatalf("probe calls = %d, want at least 2", gotCalls)
	}
	if interval != 17*time.Second {
		t.Fatalf("ticker interval = %s, want 17s", interval)
	}
}
