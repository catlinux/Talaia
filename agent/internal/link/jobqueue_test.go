package link

import (
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func jobEvent(id string, state agentv1.JobState) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_JobEvent{JobEvent: &agentv1.JobEvent{JobId: id, State: state}}}
}

func queuedIDs(l *Link) []string {
	l.jobMu.Lock()
	defer l.jobMu.Unlock()
	ids := make([]string, 0, len(l.jobQueue))
	for _, msg := range l.jobQueue {
		ids = append(ids, msg.GetJobEvent().GetJobId()+":"+msg.GetJobEvent().GetState().String())
	}
	return ids
}

// A job that ends while the agent is between two streams must still reach the
// core, or the core waits for it until the job times out.
func TestJobEventsOutliveTheStream(t *testing.T) {
	l := &Link{jobSignal: make(chan struct{}, 1)}

	// No stream: progress is dropped, the final event waits.
	l.emitJob(jobEvent("a", agentv1.JobState_JOB_STATE_RUNNING))
	l.emitJob(jobEvent("a", agentv1.JobState_JOB_STATE_SUCCEEDED))
	if got := queuedIDs(l); len(got) != 1 || got[0] != "a:JOB_STATE_SUCCEEDED" {
		t.Fatalf("queue without a stream = %v, want only the final event", got)
	}

	// A stream is attached and then lost before it sent anything: only the
	// final events are kept for the next one.
	l.attachJobs()
	l.emitJob(jobEvent("b", agentv1.JobState_JOB_STATE_RUNNING))
	l.emitJob(jobEvent("c", agentv1.JobState_JOB_STATE_FAILED))
	l.detachJobs()
	got := queuedIDs(l)
	want := []string{"a:JOB_STATE_SUCCEEDED", "c:JOB_STATE_FAILED"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("queue after a lost stream = %v, want %v", got, want)
	}

	// A failed send puts the unsent final events back in front.
	l.jobMu.Lock()
	unsent := l.jobQueue
	l.jobQueue = []*agentv1.AgentMsg{jobEvent("d", agentv1.JobState_JOB_STATE_SUCCEEDED)}
	l.jobMu.Unlock()
	l.requeueFinal(append(unsent, jobEvent("e", agentv1.JobState_JOB_STATE_RUNNING)))
	got = queuedIDs(l)
	want = []string{"a:JOB_STATE_SUCCEEDED", "c:JOB_STATE_FAILED", "d:JOB_STATE_SUCCEEDED"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("queue after a failed send = %v, want %v", got, want)
	}
}

func TestJobQueueDropsProgressFirst(t *testing.T) {
	l := &Link{jobSignal: make(chan struct{}, 1)}
	l.attachJobs()
	l.emitJob(jobEvent("final", agentv1.JobState_JOB_STATE_SUCCEEDED))
	for range maxQueuedJobEvents {
		l.emitJob(jobEvent("progress", agentv1.JobState_JOB_STATE_RUNNING))
	}
	got := queuedIDs(l)
	if len(got) != maxQueuedJobEvents {
		t.Fatalf("queue length = %d, want %d", len(got), maxQueuedJobEvents)
	}
	if got[0] != "final:JOB_STATE_SUCCEEDED" {
		t.Errorf("the final event was dropped: first = %s", got[0])
	}
}
