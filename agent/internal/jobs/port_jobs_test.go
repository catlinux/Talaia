package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/catlinux/Talaia/agent/internal/proxy"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakePortPicker struct {
	port  proxy.Port
	err   error
	calls int
}

func (f *fakePortPicker) Pick(context.Context) (proxy.Port, error) {
	f.calls++
	return f.port, f.err
}

func appPortPickCommand(jobID, name string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_AppPortPick{
			AppPortPick: &agentv1.AppPortPick{Name: name},
		},
	}
}

func TestHandlePicksAnAppPort(t *testing.T) {
	picker := &fakePortPicker{port: proxy.Port{Host: proxy.ExternalHost, Number: 23456}}
	out := newCollector()
	executor := New(&fakeActions{}).WithPorts(picker)

	executor.Handle(context.Background(), appPortPickCommand("job-port", "shop"), out.emit)

	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || final.GetOutput() != "23456" {
		t.Errorf("final event = %v", final)
	}
	if picker.calls != 1 {
		t.Errorf("picker calls = %d", picker.calls)
	}
}

func TestHandleRefusesAnInvalidAppPort(t *testing.T) {
	for _, tc := range []struct {
		name   string
		picker *fakePortPicker
	}{
		{name: "outside range", picker: &fakePortPicker{port: proxy.Port{Host: proxy.ExternalHost, Number: 80}}},
		{name: "not loopback", picker: &fakePortPicker{port: proxy.Port{Host: "0.0.0.0", Number: 23456}}},
		{name: "picker error", picker: &fakePortPicker{err: errors.New("no port")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := newCollector()
			executor := New(&fakeActions{}).WithPorts(tc.picker)
			executor.Handle(context.Background(), appPortPickCommand("job-port-bad", "shop"), out.emit)
			out.next(t) // running
			if final := out.next(t); final.GetState() != agentv1.JobState_JOB_STATE_FAILED {
				t.Errorf("final event = %v", final)
			}
		})
	}
}

func TestHandleRefusesAnInvalidStackNameAndMissingPicker(t *testing.T) {
	picker := &fakePortPicker{port: proxy.Port{Host: proxy.ExternalHost, Number: 23456}}
	out := newCollector()
	executor := New(&fakeActions{}).WithPorts(picker)
	executor.Handle(context.Background(), appPortPickCommand("job-port-name", "../shop"), out.emit)
	out.next(t)
	if final := out.next(t); final.GetState() != agentv1.JobState_JOB_STATE_FAILED {
		t.Errorf("final event = %v", final)
	}
	if picker.calls != 0 {
		t.Errorf("picker ran for an invalid name")
	}

	out = newCollector()
	New(&fakeActions{}).Handle(context.Background(), appPortPickCommand("job-port-none", "shop"), out.emit)
	out.next(t)
	if final := out.next(t); final.GetState() != agentv1.JobState_JOB_STATE_FAILED || final.GetMessage() != errNoPorts.Error() {
		t.Errorf("final event = %v", final)
	}
}
