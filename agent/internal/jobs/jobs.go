// Package jobs runs the typed commands the core sends to the agent and reports
// how they go. A command is one well-known operation with validated parameters;
// the agent never executes free text.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// jobTimeout bounds one command.
const jobTimeout = 90 * time.Second

// rememberedJobs is how many finished jobs the executor remembers to answer a
// repeated job id without running it again.
const rememberedJobs = 256

// Actions are the container operations the executor can run.
type Actions interface {
	Do(ctx context.Context, container string, action agentv1.ContainerActionType) error
}

// Executor runs commands. Every command carries a job id; running the same id
// twice does not repeat the effect, the executor sends the first outcome again.
type Executor struct {
	actions Actions

	mu       sync.Mutex
	finished map[string]*agentv1.JobEvent
	order    []string
	inflight map[string]bool
}

// New returns an Executor that runs container actions with actions.
func New(actions Actions) *Executor {
	return &Executor{
		actions:  actions,
		finished: make(map[string]*agentv1.JobEvent),
		inflight: make(map[string]bool),
	}
}

// Handle starts the command carried by msg, if it carries one, and reports
// whether it did. The job runs in its own goroutine; emit is called from it, so
// it must be safe for concurrent use.
func (e *Executor) Handle(ctx context.Context, msg *agentv1.CoreMsg, emit func(*agentv1.AgentMsg)) bool {
	action := msg.GetContainerAction()
	if action == nil {
		return false
	}

	jobID := msg.GetJobId()
	if jobID == "" {
		slog.Warn("ignoring a command without a job id")
		return true
	}

	e.mu.Lock()
	if event, done := e.finished[jobID]; done {
		e.mu.Unlock()
		emit(jobMsg(event))
		return true
	}
	if e.inflight[jobID] {
		e.mu.Unlock()
		return true
	}
	e.inflight[jobID] = true
	e.mu.Unlock()

	go e.runContainerAction(ctx, jobID, action, emit)
	return true
}

func (e *Executor) runContainerAction(ctx context.Context, jobID string, command *agentv1.ContainerAction, emit func(*agentv1.AgentMsg)) {
	emit(jobMsg(event(jobID, agentv1.JobState_JOB_STATE_RUNNING, "")))

	ctx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()

	var err error
	switch {
	case !ValidContainerRef(command.GetContainer()):
		err = errors.New("invalid container reference")
	default:
		err = e.actions.Do(ctx, command.GetContainer(), command.GetAction())
	}

	final := event(jobID, agentv1.JobState_JOB_STATE_SUCCEEDED, "")
	if err != nil {
		slog.Warn("container action failed", "job_id", jobID, "action", command.GetAction().String(), "error", err)
		final = event(jobID, agentv1.JobState_JOB_STATE_FAILED, err.Error())
	} else {
		slog.Info("container action done", "job_id", jobID, "action", command.GetAction().String())
	}

	e.remember(jobID, final)
	emit(jobMsg(final))
}

// remember stores the outcome of a job and forgets the oldest ones.
func (e *Executor) remember(jobID string, final *agentv1.JobEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.inflight, jobID)
	e.finished[jobID] = final
	e.order = append(e.order, jobID)
	if len(e.order) > rememberedJobs {
		delete(e.finished, e.order[0])
		e.order = e.order[1:]
	}
}

func event(jobID string, state agentv1.JobState, message string) *agentv1.JobEvent {
	return &agentv1.JobEvent{JobId: jobID, State: state, Message: message, At: timestamppb.Now()}
}

func jobMsg(event *agentv1.JobEvent) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_JobEvent{JobEvent: event}}
}
