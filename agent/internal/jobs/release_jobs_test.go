package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeAppReleaser struct {
	mu     sync.Mutex
	apps   []*agentv1.AppRelease
	output string
	err    error
}

func (f *fakeAppReleaser) Release(_ context.Context, app *agentv1.AppRelease) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = append(f.apps, app)
	return f.output, f.err
}

func appReleaseCommand(jobID string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_AppRelease{
			AppRelease: &agentv1.AppRelease{Name: "shop", Tag: "abcdef123456", Service: "app", Port: 3000},
		},
	}
}

func TestHandleReleasesAnApp(t *testing.T) {
	releaser := &fakeAppReleaser{output: "released"}
	out := newCollector()
	executor := New(&fakeActions{}).WithRelease(releaser)

	executor.Handle(context.Background(), appReleaseCommand("job-release"), out.emit)

	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || final.GetOutput() != "released" {
		t.Errorf("final event = %v", final)
	}
	if len(releaser.apps) != 1 || releaser.apps[0].GetName() != "shop" {
		t.Errorf("released apps = %v", releaser.apps)
	}
}

func TestHandleReportsAReleaseFailure(t *testing.T) {
	releaser := &fakeAppReleaser{err: errors.New("the new container is unhealthy")}
	out := newCollector()
	executor := New(&fakeActions{}).WithRelease(releaser)

	executor.Handle(context.Background(), appReleaseCommand("job-release-fail"), out.emit)

	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_FAILED || final.GetMessage() != "the new container is unhealthy" {
		t.Errorf("final event = %v", final)
	}
}

func TestHandleRefusesAReleaseWithoutAReleaser(t *testing.T) {
	out := newCollector()
	New(&fakeActions{}).Handle(context.Background(), appReleaseCommand("job-no-release"), out.emit)
	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_FAILED || final.GetMessage() != errNoRelease.Error() {
		t.Errorf("final event = %v", final)
	}
}
