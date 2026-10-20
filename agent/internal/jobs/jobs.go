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
	// Logs returns the last lines of the output of a container.
	Logs(ctx context.Context, container string, tail int) (string, error)
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
	action, logs := msg.GetContainerAction(), msg.GetReadLogs()
	if action == nil && logs == nil {
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

	switch {
	case action != nil:
		go e.execute(ctx, jobID, "container action "+action.GetAction().String(), emit, func(ctx context.Context) (string, error) {
			if !ValidContainerRef(action.GetContainer()) {
				return "", errors.New("invalid container reference")
			}
			return "", e.actions.Do(ctx, action.GetContainer(), action.GetAction())
		})
	default:
		go e.execute(ctx, jobID, "read logs", emit, func(ctx context.Context) (string, error) {
			if !ValidContainerRef(logs.GetContainer()) {
				return "", errors.New("invalid container reference")
			}
			return e.actions.Logs(ctx, logs.GetContainer(), int(logs.GetTail()))
		})
	}
	return true
}

// execute runs one piece of work as a job: it reports the start, runs it with a
// timeout and reports and remembers the outcome.
func (e *Executor) execute(ctx context.Context, jobID, what string, emit func(*agentv1.AgentMsg), work func(context.Context) (string, error)) {
	emit(jobMsg(event(jobID, agentv1.JobState_JOB_STATE_RUNNING, "")))

	ctx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()

	output, err := work(ctx)

	final := event(jobID, agentv1.JobState_JOB_STATE_SUCCEEDED, "")
	final.Output = output
	if err != nil {
		slog.Warn("job failed", "job_id", jobID, "job", what, "error", err)
		final = event(jobID, agentv1.JobState_JOB_STATE_FAILED, err.Error())
	} else {
		slog.Info("job done", "job_id", jobID, "job", what)
	}

	e.remember(jobID, final)
	emit(jobMsg(final))
}

// remember stores the outcome of a job and forgets the oldest ones.
func (e *Executor) remember(jobID string, final *agentv1.JobEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.inflight, jobID)
	// The output can be large and is not needed to answer a repeated job id.
	kept := &agentv1.JobEvent{JobId: final.GetJobId(), State: final.GetState(), Message: final.GetMessage(), At: final.GetAt()}
	e.finished[jobID] = kept
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
