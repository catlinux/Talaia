// Package jobs runs the typed commands the core sends to the agent and reports
// how they go. A command is one well-known operation with validated parameters;
// the agent never executes free text.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	appbuild "github.com/catlinux/Talaia/agent/internal/build"
	"github.com/catlinux/Talaia/agent/internal/proxy"
	"github.com/catlinux/Talaia/agent/internal/source"
	"github.com/catlinux/Talaia/agent/internal/stacks"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// jobTimeout bounds a container command; the stack commands pull images and
// start services, so they get longer.
const (
	jobTimeout          = 90 * time.Second
	stackActionTimeout  = 10 * time.Minute
	stackWriteTimeout   = 2 * time.Minute
	resourceTimeout     = 5 * time.Minute
	sourceTimeout       = 5 * time.Minute
	releaseTimeout      = 10 * time.Minute
	proxyInstallTimeout = 10 * time.Minute
	proxyApplyTimeout   = time.Minute
	appCleanupTimeout   = 5 * time.Minute
)

// rememberedJobs is how many finished jobs the executor remembers to answer a
// repeated job id without running it again.
const rememberedJobs = 256

// errNoStacks is the failure of a stack command on an executor without stacks.
var errNoStacks = errors.New("stacks are not available on this agent")

// errNoResources is the failure of a resource command on an executor without them.
var errNoResources = errors.New("resources are not available on this agent")

// errNoSource is the failure of an app source command without the git fetcher.
var errNoSource = errors.New("the git source is not available on this agent")

// errNoBuild is the failure of an app build command without the builder.
var errNoBuild = errors.New("the image builder is not available on this agent")

// errNoRelease is the failure of an app release command without the releaser.
var errNoRelease = errors.New("the app releaser is not available on this agent")

// errNoPorts is the failure of a port command without the port picker.
var errNoPorts = errors.New("the app port picker is not available on this agent")

// errNoCleanup is the failure of a cleanup command without the cleaner.
var errNoCleanup = errors.New("the app cleanup is not available on this agent")

// errNoProxy is the failure of a proxy command without the proxy manager.
var errNoProxy = errors.New("the proxy manager is not available on this agent")

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

// SourceFetcher checks out the source of an app into its build workspace.
type SourceFetcher interface {
	Fetch(ctx context.Context, app *agentv1.AppSource) (source.Result, error)
}

// AppBuilder builds the image of an app from its workspace.
type AppBuilder interface {
	Build(ctx context.Context, app *agentv1.AppBuild) (string, error)
}

// AppReleaser starts an app image in its stack.
type AppReleaser interface {
	Release(ctx context.Context, app *agentv1.AppRelease) (string, error)
}

// AppCleaner removes the images and build workspace of an app.
type AppCleaner interface {
	Cleanup(ctx context.Context, name string) (string, error)
}

// PortPicker chooses a free TCP port on 127.0.0.1 for an app.
type PortPicker interface {
	Pick(ctx context.Context) (proxy.Port, error)
}

// ProxyManager installs the managed proxy and loads its configuration.
type ProxyManager interface {
	Install(ctx context.Context, action agentv1.ProxyInstallAction) (string, error)
	Apply(ctx context.Context, routes []*agentv1.ProxyRoute) (string, error)
}

// Executor runs commands. Every command carries a job id; running the same id
// twice does not repeat the effect, the executor sends the first outcome again.
type Executor struct {
	actions Actions
	stacks  StackManager
	// sources is nil until WithSource sets it.
	sources SourceFetcher
	// builds is nil until WithBuild sets it.
	builds AppBuilder
	// releases is nil until WithRelease sets it.
	releases AppReleaser
	// cleanups is nil until WithCleanup sets it.
	cleanups AppCleaner
	// ports is nil until WithPorts sets it.
	ports PortPicker
	// proxy is nil until WithProxy sets it.
	proxy ProxyManager
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

// WithSource lets the executor fetch the git source of an app.
func (e *Executor) WithSource(fetcher SourceFetcher) *Executor {
	e.sources = fetcher
	return e
}

// WithBuild lets the executor build the image of an app.
func (e *Executor) WithBuild(builder AppBuilder) *Executor {
	e.builds = builder
	return e
}

// WithRelease lets the executor start an app image.
func (e *Executor) WithRelease(releaser AppReleaser) *Executor {
	e.releases = releaser
	return e
}

// WithCleanup lets the executor remove the images and build workspace of an app.
func (e *Executor) WithCleanup(cleaner AppCleaner) *Executor {
	e.cleanups = cleaner
	return e
}

// WithPorts lets the executor pick host ports for apps.
func (e *Executor) WithPorts(picker PortPicker) *Executor {
	e.ports = picker
	return e
}

// WithProxy lets the executor install and configure the managed proxy.
func (e *Executor) WithProxy(manager ProxyManager) *Executor {
	e.proxy = manager
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
	appSource, appBuild := msg.GetAppSource(), msg.GetAppBuild()
	appRelease := msg.GetAppRelease()
	appPortPick := msg.GetAppPortPick()
	appCleanup := msg.GetAppCleanup()
	proxyInstall, proxyApply := msg.GetProxyInstall(), msg.GetProxyApply()
	list, remove := msg.GetListResources(), msg.GetRemoveResources()
	updateCommand := msg.GetAgentUpdate()
	aptStatus := msg.GetAptStatus()
	aptUpgrade, hostReboot := msg.GetAptUpgrade(), msg.GetHostReboot()
	serviceAction := msg.GetServiceAction()
	readJournal := msg.GetReadJournal()
	if action == nil && logs == nil && write == nil && stackAction == nil && read == nil && adopt == nil && appSource == nil && appBuild == nil && appRelease == nil && appPortPick == nil && appCleanup == nil && proxyInstall == nil && proxyApply == nil && list == nil && remove == nil && updateCommand == nil && aptStatus == nil && aptUpgrade == nil && hostReboot == nil && serviceAction == nil && readJournal == nil {
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
	case appSource != nil:
		go e.execute(ctx, jobID, "app source", sourceTimeout, emit, func(ctx context.Context) (string, error) {
			if e.sources == nil {
				return "", errNoSource
			}
			result, err := e.sources.Fetch(ctx, appSource)
			if err != nil {
				return "", err
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				return "", fmt.Errorf("encode the source result: %w", err)
			}
			return string(encoded), nil
		})
	case appBuild != nil:
		go e.execute(ctx, jobID, "app build "+appBuild.GetName(), appbuild.Timeout, emit, func(ctx context.Context) (string, error) {
			if e.builds == nil {
				return "", errNoBuild
			}
			return e.builds.Build(ctx, appBuild)
		})
	case appRelease != nil:
		go e.execute(ctx, jobID, "app release "+appRelease.GetName(), releaseTimeout, emit, func(ctx context.Context) (string, error) {
			if e.releases == nil {
				return "", errNoRelease
			}
			return e.releases.Release(ctx, appRelease)
		})
	case appPortPick != nil:
		go e.execute(ctx, jobID, "app port pick "+appPortPick.GetName(), jobTimeout, emit, func(ctx context.Context) (string, error) {
			if e.ports == nil {
				return "", errNoPorts
			}
			if !stacks.ValidName(appPortPick.GetName()) {
				return "", errors.New("invalid stack name")
			}
			port, err := e.ports.Pick(ctx)
			if err != nil {
				return "", err
			}
			if port.Host != proxy.ExternalHost || int(port.Number) < proxy.PortRangeStart || int(port.Number) > proxy.PortRangeEnd {
				return "", errors.New("the port picker returned a port outside the app range")
			}
			return strconv.Itoa(int(port.Number)), nil
		})
	case appCleanup != nil:
		go e.execute(ctx, jobID, "app cleanup "+appCleanup.GetName(), appCleanupTimeout, emit, func(ctx context.Context) (string, error) {
			if e.cleanups == nil {
				return "", errNoCleanup
			}
			if !stacks.ValidName(appCleanup.GetName()) {
				return "", errors.New("invalid stack name")
			}
			return e.cleanups.Cleanup(ctx, appCleanup.GetName())
		})
	case proxyInstall != nil:
		go e.execute(ctx, jobID, "proxy install "+proxyInstall.GetAction().String(), proxyInstallTimeout, emit, func(ctx context.Context) (string, error) {
			if e.proxy == nil {
				return "", errNoProxy
			}
			return e.proxy.Install(ctx, proxyInstall.GetAction())
		})
	case proxyApply != nil:
		go e.execute(ctx, jobID, "proxy apply", proxyApplyTimeout, emit, func(ctx context.Context) (string, error) {
			if e.proxy == nil {
				return "", errNoProxy
			}
			return e.proxy.Apply(ctx, proxyApply.GetRoutes())
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
