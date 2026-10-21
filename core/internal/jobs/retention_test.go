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
	purger := &fakePurger{calls: make(chan purgeCall, 4)}

	go PurgeLoop(ctx, purger, 15*time.Minute, 10*time.Millisecond)

	for index := 0; index < 2; index++ {
		select {
		case call := <-purger.calls:
			if call.kind != ContainerLogsKind || call.olderThan != 15*time.Minute {
				t.Errorf("call = %+v", call)
			}
		case <-time.After(time.Second):
			t.Fatalf("purge %d did not run", index+1)
		}
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
