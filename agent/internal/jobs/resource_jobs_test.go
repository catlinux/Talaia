package jobs

import (
	"context"
	"errors"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// fakeResources answers the resource commands with fixed output.
type fakeResources struct {
	listed  agentv1.ResourceKind
	removed []*agentv1.ResourceRef
	output  string
	err     error
}

func (f *fakeResources) ListResources(_ context.Context, kind agentv1.ResourceKind) (string, error) {
	f.listed = kind
	return f.output, f.err
}

func (f *fakeResources) RemoveResources(_ context.Context, items []*agentv1.ResourceRef) (string, error) {
	f.removed = items
	return f.output, f.err
}

func TestHandleListsResources(t *testing.T) {
	resources := &fakeResources{output: `{"kind":"image","items":[]}`}
	out := newCollector()

	New(&fakeActions{}).WithResources(resources).Handle(context.Background(), &agentv1.CoreMsg{
		JobId: "r1",
		Cmd:   &agentv1.CoreMsg_ListResources{ListResources: &agentv1.ListResources{Kind: agentv1.ResourceKind_RESOURCE_KIND_IMAGE}},
	}, out.emit)

	out.next(t)
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetOutput() != resources.output {
		t.Errorf("final event = %v", got)
	}
	if resources.listed != agentv1.ResourceKind_RESOURCE_KIND_IMAGE {
		t.Errorf("listed = %v", resources.listed)
	}
}

func TestHandleRemovesResourcesAndCarriesFailures(t *testing.T) {
	resources := &fakeResources{output: `{"results":[]}`}
	out := newCollector()
	items := []*agentv1.ResourceRef{{Kind: agentv1.ResourceKind_RESOURCE_KIND_VOLUME, Id: "old"}}

	New(&fakeActions{}).WithResources(resources).Handle(context.Background(), &agentv1.CoreMsg{
		JobId: "r2",
		Cmd:   &agentv1.CoreMsg_RemoveResources{RemoveResources: &agentv1.RemoveResources{Items: items}},
	}, out.emit)

	out.next(t)
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED {
		t.Errorf("final event = %v", got)
	}
	if len(resources.removed) != 1 || resources.removed[0].GetId() != "old" {
		t.Errorf("removed = %v", resources.removed)
	}

	failing := &fakeResources{err: errors.New("invalid resource reference")}
	out = newCollector()
	New(&fakeActions{}).WithResources(failing).Handle(context.Background(), &agentv1.CoreMsg{
		JobId: "r3",
		Cmd:   &agentv1.CoreMsg_RemoveResources{RemoveResources: &agentv1.RemoveResources{Items: items}},
	}, out.emit)
	out.next(t)
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() != "invalid resource reference" {
		t.Errorf("failing event = %v", got)
	}
}

func TestResourceCommandsFailCleanlyWithoutAManager(t *testing.T) {
	out := newCollector()

	New(&fakeActions{}).Handle(context.Background(), &agentv1.CoreMsg{
		JobId: "r4",
		Cmd:   &agentv1.CoreMsg_ListResources{ListResources: &agentv1.ListResources{Kind: agentv1.ResourceKind_RESOURCE_KIND_VOLUME}},
	}, out.emit)

	out.next(t)
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() == "" {
		t.Errorf("final event = %v", got)
	}
}
