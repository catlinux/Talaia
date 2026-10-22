package jobs

import (
	"context"
	"testing"
	"time"
)

type purgeCall struct {
	kind      string
	olderThan time.Duration
}

type fakePurger struct {
	calls chan purgeCall
	err   error
}

func (f *fakePurger) PurgeFinishedJobs(_ context.Context, kind string, olderThan time.Duration) (int64, error) {
	f.calls <- purgeCall{kind: kind, olderThan: olderThan}
	return 0, f.err
}

func TestPurgeLoopRunsAtStartAndEveryInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	purger := &fakePurger{calls: make(chan purgeCall, 16)}

	go PurgeLoop(ctx, purger, 15*time.Minute, 10*time.Millisecond)

	for cycle := 0; cycle < 2; cycle++ {
		seen := make(map[string]time.Duration, len(retainedKinds))
		for index := 0; index < len(retainedKinds); index++ {
			select {
			case call := <-purger.calls:
				seen[call.kind] = call.olderThan
			case <-time.After(time.Second):
				t.Fatalf("purge %d did not run", cycle+1)
			}
		}
		for _, kind := range retainedKinds {
			if seen[kind] != 15*time.Minute {
				t.Errorf("cycle %d purged %s with retention %s, want 15m", cycle+1, kind, seen[kind])
			}
		}
	}
}

func TestPurgeLoopPurgesJournalReadJobs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	purger := &fakePurger{calls: make(chan purgeCall, 4)}

	go PurgeLoop(ctx, purger, 15*time.Minute, time.Hour)

	seen := make(map[string]bool)
	for index := 0; index < len(retainedKinds); index++ {
		select {
		case call := <-purger.calls:
			seen[call.kind] = true
			if call.olderThan != 15*time.Minute {
				t.Errorf("retention for %s = %s, want 15m", call.kind, call.olderThan)
			}
		case <-time.After(time.Second):
			t.Fatal("the initial purge did not run")
		}
	}
	if !seen[JournalReadKind] {
		t.Error("journal_read jobs are not purged")
	}
	if !seen[ContainerLogsKind] {
		t.Error("container_logs jobs are not purged")
	}
}

func TestPurgeLoopStopsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	purger := &fakePurger{calls: make(chan purgeCall, 1024)}

	go PurgeLoop(ctx, purger, time.Minute, time.Millisecond)
	select {
	case <-purger.calls:
	case <-time.After(time.Second):
		t.Fatal("the first purge did not run")
	}

	cancel()
	time.Sleep(20 * time.Millisecond)
	for len(purger.calls) > 0 {
		<-purger.calls
	}
	select {
	case <-purger.calls:
		t.Error("the loop kept purging after the context ended")
	case <-time.After(50 * time.Millisecond):
	}
}
