package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/jobs"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	jobKindSource  = "deploy_source"
	jobKindBuild   = "deploy_build"
	jobKindRelease = "deploy_release"
	jobKindRead    = "deploy_stack_read"
	jobKindWrite   = "deploy_stack_write"
	jobKindUp      = "deploy_stack_up"
	jobKindRemove  = "deploy_stack_remove"
	jobKindCleanup = "deploy_app_cleanup"
	jobKindProxy   = "deploy_proxy_apply"
	jobKindPort    = "deploy_port_pick"
	jobKindProbe   = jobs.GitProbeKind

	defaultPollInterval = time.Second
)

// Dispatcher queues a typed command for the agent of a server.
type Dispatcher interface {
	Dispatch(serverID string, msg *agentv1.CoreMsg) error
}

// JobStore is the part of the store used to follow an agent command.
type JobStore interface {
	CreateJob(ctx context.Context, serverID, kind string, params map[string]any, createdBy, createdByID string) (store.Job, error)
	GetJob(ctx context.Context, id string) (store.Job, error)
	UpdateJobState(ctx context.Context, id, serverID, state, message, output string) (bool, error)
}

// HubAgentOptions are the collaborators of the real agent adapter.
type HubAgentOptions struct {
	Dispatcher   Dispatcher
	Jobs         JobStore
	PollInterval time.Duration
}

// HubAgent sends the deployment commands through the agent hub and follows
// their jobs in the job table.
type HubAgent struct {
	dispatcher   Dispatcher
	jobs         JobStore
	pollInterval time.Duration
}

// NewHubAgent returns an Agent for the connected agents.
func NewHubAgent(options HubAgentOptions) *HubAgent {
	interval := options.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	return &HubAgent{dispatcher: options.Dispatcher, jobs: options.Jobs, pollInterval: interval}
}

// Source fetches a commit into the workspace of the app.
func (a *HubAgent) Source(ctx context.Context, serverID string, msg *agentv1.AppSource) (SourceResult, error) {
	job, err := a.runJob(ctx, serverID, jobKindSource, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_AppSource{AppSource: msg}}
	})
	if err != nil {
		return SourceResult{}, err
	}
	return jsonSourceResult(job.Output)
}

// Build builds the image of the app.
func (a *HubAgent) Build(ctx context.Context, serverID string, msg *agentv1.AppBuild) (string, error) {
	job, err := a.runJob(ctx, serverID, jobKindBuild, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_AppBuild{AppBuild: msg}}
	})
	if err != nil {
		return job.Output, err
	}
	return job.Output, nil
}

// Release starts the app image.
func (a *HubAgent) Release(ctx context.Context, serverID string, msg *agentv1.AppRelease) (string, error) {
	job, err := a.runJob(ctx, serverID, jobKindRelease, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_AppRelease{AppRelease: msg}}
	})
	if err != nil {
		return job.Output, err
	}
	return job.Output, nil
}

// ReadStack returns the files of the app stack.
func (a *HubAgent) ReadStack(ctx context.Context, serverID, name string) (string, string, error) {
	job, err := a.runJob(ctx, serverID, jobKindRead, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_StackRead{StackRead: &agentv1.StackRead{Name: name}}}
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "does not exist") {
			return "", "", ErrStackNotFound
		}
		return "", "", err
	}
	var contents struct {
		Compose string `json:"compose"`
		Env     string `json:"env"`
	}
	if err := json.Unmarshal([]byte(job.Output), &contents); err != nil {
		return "", "", fmt.Errorf("decode the stack files: %w", err)
	}
	return contents.Compose, contents.Env, nil
}

// WriteStack writes the files of the app stack.
func (a *HubAgent) WriteStack(ctx context.Context, serverID string, msg *agentv1.StackWrite) (string, error) {
	job, err := a.runJob(ctx, serverID, jobKindWrite, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_StackWrite{StackWrite: msg}}
	})
	if err != nil {
		return job.Output, err
	}
	return job.Output, nil
}

// UpStack starts the stack.
func (a *HubAgent) UpStack(ctx context.Context, serverID, name string) (string, error) {
	job, err := a.runJob(ctx, serverID, jobKindUp, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_StackAction{
			StackAction: &agentv1.StackAction{Name: name, Action: agentv1.StackActionType_STACK_ACTION_TYPE_UP},
		}}
	})
	if err != nil {
		return job.Output, err
	}
	return job.Output, nil
}

// RemoveStack stops and removes the app stack, keeping its named volumes.
func (a *HubAgent) RemoveStack(ctx context.Context, serverID, name string) (string, error) {
	job, err := a.runJob(ctx, serverID, jobKindRemove, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_StackAction{
			StackAction: &agentv1.StackAction{Name: name, Action: agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE},
		}}
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "stack does not exist") {
			return job.Output, ErrStackNotFound
		}
		return job.Output, err
	}
	return job.Output, nil
}

// CleanupApp removes the images and build workspace of an app.
func (a *HubAgent) CleanupApp(ctx context.Context, serverID, name string) (string, error) {
	job, err := a.runJob(ctx, serverID, jobKindCleanup, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_AppCleanup{
			AppCleanup: &agentv1.AppCleanup{Name: name},
		}}
	})
	if err != nil {
		return job.Output, err
	}
	return job.Output, nil
}

// ApplyProxyRoutes replaces the managed proxy configuration of a server.
func (a *HubAgent) ApplyProxyRoutes(ctx context.Context, serverID string, routes []*agentv1.ProxyRoute) (string, error) {
	job, err := a.runJob(ctx, serverID, jobKindProxy, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_ProxyApply{
			ProxyApply: &agentv1.ProxyApply{Routes: routes},
		}}
	})
	if err != nil {
		return job.Output, err
	}
	return job.Output, nil
}

// PickPort asks the agent for a free host port on 127.0.0.1.
func (a *HubAgent) PickPort(ctx context.Context, serverID, name string) (int, error) {
	job, err := a.runJob(ctx, serverID, jobKindPort, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_AppPortPick{
			AppPortPick: &agentv1.AppPortPick{Name: name},
		}}
	})
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(job.Output))
	if err != nil {
		return 0, fmt.Errorf("decode the app host port: %w", err)
	}
	return port, nil
}

// Probe asks which commit the app branch points at.
func (a *HubAgent) Probe(ctx context.Context, serverID string, msg *agentv1.GitProbe) (string, error) {
	job, err := a.runJob(ctx, serverID, jobKindProbe, func(jobID string) *agentv1.CoreMsg {
		return &agentv1.CoreMsg{JobId: jobID, Cmd: &agentv1.CoreMsg_GitProbe{GitProbe: msg}}
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal([]byte(job.Output), &result); err != nil {
		return "", fmt.Errorf("decode the git probe result: %w", err)
	}
	commit := strings.TrimSpace(result.Commit)
	if commit == "" {
		return "", errors.New("the git probe returned an empty commit")
	}
	if !validCommit(commit) {
		return "", errors.New("the git probe returned an invalid commit")
	}
	return commit, nil
}

func (a *HubAgent) runJob(ctx context.Context, serverID, kind string, command func(jobID string) *agentv1.CoreMsg) (store.Job, error) {
	if a.dispatcher == nil || a.jobs == nil {
		return store.Job{}, errors.New("the deployment agent is not configured")
	}
	job, err := a.jobs.CreateJob(ctx, serverID, kind, map[string]any{}, "deploy", "")
	if err != nil {
		return store.Job{}, err
	}
	if err := a.dispatcher.Dispatch(serverID, command(job.ID)); err != nil {
		_, _ = a.jobs.UpdateJobState(ctx, job.ID, serverID, store.JobFailed, err.Error(), "")
		return store.Job{}, err
	}

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()
	for {
		current, err := a.jobs.GetJob(ctx, job.ID)
		if err != nil {
			return store.Job{}, err
		}
		switch current.State {
		case store.JobSucceeded:
			return current, nil
		case store.JobFailed:
			message := strings.TrimSpace(current.Message)
			if message == "" {
				message = "the agent failed the deployment step"
			}
			return current, errors.New(message)
		}

		select {
		case <-ctx.Done():
			return store.Job{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
