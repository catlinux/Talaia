// Package jobs runs the typed commands the core sends to the agent and reports
// how they go. A command is one well-known operation with validated parameters;
// the agent never executes free text.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// jobTimeout bounds a container command; the stack commands pull images and
// start services, so they get longer.
const (
	jobTimeout         = 90 * time.Second
	stackActionTimeout = 10 * time.Minute
	stackWriteTimeout  = 2 * time.Minute
	resourceTimeout    = 5 * time.Minute
)

// rememberedJobs is how many finished jobs the executor remembers to answer a
// repeated job id without running it again.
const rememberedJobs = 256

// errNoStacks is the failure of a stack command on an executor without stacks.
var errNoStacks = errors.New("stacks are not available on this agent")

// errNoResources is the failure of a resource command on an executor without them.
var errNoResources = errors.New("resources are not available on this agent")

// Actions are the container operations the executor can run.
type Actions interface {
	Do(ctx context.Context, container string, action agentv1.ContainerActionType) error
	// Logs returns the last lines of the output of a container after since (zero
	// means no limit), with timestamps when asked.
	Logs(ctx context.Context, container string, tail int, since time.Time, timestamps bool) (string, error)
}

// StackManager runs the managed Compose stacks of the server.
type StackManager interface {
	Write(ctx context.Context, name, compose, env string) (string, error)
	Do(ctx context.Context, name string, action agentv1.StackActionType) (string, error)
	Read(ctx context.Context, name string) (string, error)
	Adopt(ctx context.Context, name string) (string, error)
}

// ResourceManager lists and removes Docker images, volumes and networks.
type ResourceManager interface {
	ListResources(ctx context.Context, kind agentv1.ResourceKind) (string, error)
	RemoveResources(ctx context.Context, items []*agentv1.ResourceRef) (string, error)
}

// Executor runs commands. Every command carries a job id; running the same id
// twice does not repeat the effect, the executor sends the first outcome again.
type Executor struct {
	actions Actions
	stacks  StackManager
	// resources is nil until WithResources sets it.
	resources ResourceManager
	// updates is nil until WithUpdates sets it.
	updates Updater
	// apt is nil until WithApt sets it.
	apt AptStatusReader
	// aptOps is nil until WithAptOperations sets it.
	aptOps AptOperations
	// services is nil until WithServices sets it.
	services ServiceManager
	// journal is nil until WithJournal sets it.
	journal JournalReader

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

// WithStacks lets the executor run stack commands.
func (e *Executor) WithStacks(manager StackManager) *Executor {
	e.stacks = manager
	return e
}

// WithResources lets the executor list and remove Docker resources.
func (e *Executor) WithResources(manager ResourceManager) *Executor {
	e.resources = manager
	return e
}

// WithAptOperations lets the executor run apt upgrades and host reboots.
func (e *Executor) WithAptOperations(manager AptOperations) *Executor {
	e.aptOps = manager
	return e
}

// WithServices lets the executor run systemd service actions.
func (e *Executor) WithServices(manager ServiceManager) *Executor {
	e.services = manager
	return e
}

// WithJournal lets the executor read the systemd journal.
func (e *Executor) WithJournal(reader JournalReader) *Executor {
	e.journal = reader
	return e
}

// Handle starts the command carried by msg, if it carries one, and reports
// whether it did. The job runs in its own goroutine; emit is called from it, so
// it must be safe for concurrent use.
func (e *Executor) Handle(ctx context.Context, msg *agentv1.CoreMsg, emit func(*agentv1.AgentMsg)) bool {
	action, logs := msg.GetContainerAction(), msg.GetReadLogs()
	write, stackAction, read, adopt := msg.GetStackWrite(), msg.GetStackAction(), msg.GetStackRead(), msg.GetStackAdopt()
	list, remove := msg.GetListResources(), msg.GetRemoveResources()
	updateCommand := msg.GetAgentUpdate()
	aptStatus := msg.GetAptStatus()
	aptUpgrade, hostReboot := msg.GetAptUpgrade(), msg.GetHostReboot()
	serviceAction := msg.GetServiceAction()
	readJournal := msg.GetReadJournal()
	if action == nil && logs == nil && write == nil && stackAction == nil && read == nil && adopt == nil && list == nil && remove == nil && updateCommand == nil && aptStatus == nil && aptUpgrade == nil && hostReboot == nil && serviceAction == nil && readJournal == nil {
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
		go e.execute(ctx, jobID, "container action "+action.GetAction().String(), jobTimeout, emit, func(ctx context.Context) (string, error) {
			if !ValidContainerRef(action.GetContainer()) {
				return "", errors.New("invalid container reference")
			}
			return "", e.actions.Do(ctx, action.GetContainer(), action.GetAction())
		})
	case logs != nil:
		go e.execute(ctx, jobID, "read logs", jobTimeout, emit, func(ctx context.Context) (string, error) {
			if !ValidContainerRef(logs.GetContainer()) {
				return "", errors.New("invalid container reference")
			}
			since, err := parseSince(logs.GetSince())
			if err != nil {
				return "", err
			}
			return e.actions.Logs(ctx, logs.GetContainer(), int(logs.GetTail()), since, logs.GetTimestamps())
		})
	case write != nil:
		go e.execute(ctx, jobID, "stack write", stackWriteTimeout, emit, func(ctx context.Context) (string, error) {
			if e.stacks == nil {
				return "", errNoStacks
			}
			return e.stacks.Write(ctx, write.GetName(), write.GetComposeYaml(), write.GetEnv())
		})
	case stackAction != nil:
		go e.execute(ctx, jobID, "stack action "+stackAction.GetAction().String(), stackActionTimeout, emit, func(ctx context.Context) (string, error) {
			if e.stacks == nil {
				return "", errNoStacks
			}
			return e.stacks.Do(ctx, stackAction.GetName(), stackAction.GetAction())
		})
	case list != nil:
		go e.execute(ctx, jobID, "list resources", jobTimeout, emit, func(ctx context.Context) (string, error) {
			if e.resources == nil {
				return "", errNoResources
			}
			return e.resources.ListResources(ctx, list.GetKind())
		})
	case remove != nil:
		go e.execute(ctx, jobID, "remove resources", resourceTimeout, emit, func(ctx context.Context) (string, error) {
			if e.resources == nil {
				return "", errNoResources
			}
			return e.resources.RemoveResources(ctx, remove.GetItems())
		})
	case adopt != nil:
		go e.execute(ctx, jobID, "stack adopt", jobTimeout, emit, func(ctx context.Context) (string, error) {
			if e.stacks == nil {
				return "", errNoStacks
			}
			return e.stacks.Adopt(ctx, adopt.GetName())
		})
	case updateCommand != nil:
		go e.execute(ctx, jobID, "agent update", updateTimeout, emit, func(ctx context.Context) (string, error) {
			if e.updates == nil {
				return "", errNoUpdates
			}
			return e.updates.Update(ctx, updateCommand.GetVersion(), updateCommand.GetSha256())
		})
	case aptStatus != nil:
		timeout := aptStatusTimeout
		if aptStatus.GetRefresh() {
			timeout = aptStatusRefreshTimeout
		}
		go e.execute(ctx, jobID, "apt status", timeout, emit, func(ctx context.Context) (string, error) {
			if e.apt == nil {
				return "", errNoApt
			}
			output, err := e.apt.StatusJSON(ctx, aptStatus.GetRefresh())
			if err != nil {
				return "", err
			}
			if len(output) > maxAptStatusBytes {
				return "", errors.New("the apt status is too large")
			}
			return output, nil
		})
	case aptUpgrade != nil:
		go e.execute(ctx, jobID, "apt upgrade", aptUpgradeTimeout, emit, func(ctx context.Context) (string, error) {
			if !ValidJobID(jobID) {
				return "", fmt.Errorf("invalid job id %q", jobID)
			}
			if e.aptOps == nil {
				return "", errNoAptOperations
			}
			return e.aptOps.Upgrade(ctx, jobID, aptUpgrade.GetMode(), emit)
		})
	case hostReboot != nil:
		go e.execute(ctx, jobID, "host reboot", hostRebootTimeout, emit, func(ctx context.Context) (string, error) {
			if !ValidJobID(jobID) {
				return "", fmt.Errorf("invalid job id %q", jobID)
			}
			if e.aptOps == nil {
				return "", errNoAptOperations
			}
			return "", e.aptOps.Reboot(ctx, jobID, hostReboot.GetHostname())
		})
	case serviceAction != nil:
		go e.execute(ctx, jobID, "service action "+serviceAction.GetAction().String(), jobTimeout, emit, func(ctx context.Context) (string, error) {
			if e.services == nil {
				return "", errNoServices
			}
			return e.services.Do(ctx, serviceAction.GetUnit(), serviceAction.GetAction())
		})
	case readJournal != nil:
		go e.execute(ctx, jobID, "read journal", journalTimeout, emit, func(ctx context.Context) (string, error) {
			if e.journal == nil {
				return "", errNoJournal
			}
			return e.journal.Read(ctx, readJournal)
		})
	default:
		go e.execute(ctx, jobID, "stack read", jobTimeout, emit, func(ctx context.Context) (string, error) {
			if e.stacks == nil {
				return "", errNoStacks
			}
			return e.stacks.Read(ctx, read.GetName())
		})
	}
	return true
}

// execute runs one piece of work as a job: it reports the start, runs it with a
// timeout and reports and remembers the outcome.
func (e *Executor) execute(ctx context.Context, jobID, what string, timeout time.Duration, emit func(*agentv1.AgentMsg), work func(context.Context) (string, error)) {
	emit(jobMsg(event(jobID, agentv1.JobState_JOB_STATE_RUNNING, "")))

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output, err := work(ctx)

	// A protobuf string must be valid UTF-8 or the event cannot be sent, and
	// command output is cut at byte limits that can split a character.
	final := event(jobID, agentv1.JobState_JOB_STATE_SUCCEEDED, "")
	final.Output = strings.ToValidUTF8(output, "�")
	if err != nil {
		slog.Warn("job failed", "job_id", jobID, "job", what, "error", err)
		final = event(jobID, agentv1.JobState_JOB_STATE_FAILED, strings.ToValidUTF8(err.Error(), "�"))
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
