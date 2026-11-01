package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/catlinux/Talaia/agent/internal/source"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeAppBuilder struct {
	mu     sync.Mutex
	apps   []*agentv1.AppBuild
	output string
	err    error
}

func (f *fakeAppBuilder) Build(_ context.Context, app *agentv1.AppBuild) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = append(f.apps, app)
	return f.output, f.err
}

type fakeSourceFetcher struct {
	mu          sync.Mutex
	apps        []*agentv1.AppSource
	result      source.Result
	probes      []*agentv1.GitProbe
	probeCommit string
	probeErr    error
	err         error
}

func (f *fakeSourceFetcher) Fetch(_ context.Context, app *agentv1.AppSource) (source.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = append(f.apps, app)
	return f.result, f.err
}

func (f *fakeSourceFetcher) Probe(_ context.Context, probe *agentv1.GitProbe) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes = append(f.probes, probe)
	return f.probeCommit, f.probeErr
}

func appBuildCommand(jobID string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_AppBuild{
			AppBuild: &agentv1.AppBuild{Name: "shop", Tag: "abcdef123456"},
		},
	}
}

func appSourceCommand(jobID string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_AppSource{
			AppSource: &agentv1.AppSource{Name: "shop", Branch: "main"},
		},
	}
}

func TestHandleBuildsAnAppImage(t *testing.T) {
	builder := &fakeAppBuilder{output: "image built"}
	out := newCollector()
	executor := New(&fakeActions{}).WithBuild(builder)

	if !executor.Handle(context.Background(), appBuildCommand("job-build"), out.emit) {
		t.Fatal("the build command was not handled")
	}
	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || final.GetOutput() != "image built" {
		t.Errorf("final event = %v", final)
	}
	if len(builder.apps) != 1 || builder.apps[0].GetName() != "shop" {
		t.Errorf("builder apps = %v", builder.apps)
	}
}

func TestHandleReportsAFailedBuild(t *testing.T) {
	builder := &fakeAppBuilder{err: errors.New("no space")}
	out := newCollector()
	executor := New(&fakeActions{}).WithBuild(builder)

	executor.Handle(context.Background(), appBuildCommand("job-build-fail"), out.emit)

	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_FAILED || final.GetMessage() != "no space" {
		t.Errorf("final event = %v", final)
	}
}

func TestHandleFetchesTheAppSource(t *testing.T) {
	fetcher := &fakeSourceFetcher{result: source.Result{Commit: "abcdef123456", Subject: "first"}}
	out := newCollector()
	executor := New(&fakeActions{}).WithSource(fetcher)

	executor.Handle(context.Background(), appSourceCommand("job-source"), out.emit)

	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED {
		t.Fatalf("final event = %v", final)
	}
	if final.GetOutput() != `{"commit":"abcdef123456","subject":"first"}` {
		t.Errorf("output = %q", final.GetOutput())
	}
	if len(fetcher.apps) != 1 || fetcher.apps[0].GetName() != "shop" {
		t.Errorf("fetcher apps = %v", fetcher.apps)
	}
}

func TestHandleRefusesAppCommandsWithoutTheirManager(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  *agentv1.CoreMsg
		want string
	}{
		{name: "build", msg: appBuildCommand("job-no-build"), want: errNoBuild.Error()},
		{name: "source", msg: appSourceCommand("job-no-source"), want: errNoSource.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := newCollector()
			New(&fakeActions{}).Handle(context.Background(), tc.msg, out.emit)
			out.next(t) // running
			final := out.next(t)
			if final.GetState() != agentv1.JobState_JOB_STATE_FAILED || final.GetMessage() != tc.want {
				t.Errorf("final event = %v", final)
			}
		})
	}
}
