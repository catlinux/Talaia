// Package deploy runs the app deployment pipeline of the core: one deployment
// at a time per app, with source fetch, image build, stack update and release.
package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	// DefaultIdleInterval is how long the worker waits when the queue is empty.
	DefaultIdleInterval = 5 * time.Second

	sourceTimeout   = 6 * time.Minute
	buildTimeout    = 31 * time.Minute
	stackTimeout    = 3 * time.Minute
	releaseTimeout  = 11 * time.Minute
	portPickTimeout = time.Minute
)

// ErrStackNotFound tells the pipeline that the app stack does not exist yet.
var ErrStackNotFound = errors.New("the stack does not exist")

// SourceResult is what the agent reports after checking out a commit.
type SourceResult struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Note    string `json:"note,omitempty"`
}

// Agent is the side of the deployment pipeline that runs commands on the app
// server. Tests replace it with a fake.
type Agent interface {
	Source(ctx context.Context, serverID string, msg *agentv1.AppSource) (SourceResult, error)
	Build(ctx context.Context, serverID string, msg *agentv1.AppBuild) (string, error)
	Release(ctx context.Context, serverID string, msg *agentv1.AppRelease) (string, error)
	ReadStack(ctx context.Context, serverID, name string) (compose, env string, err error)
	WriteStack(ctx context.Context, serverID string, msg *agentv1.StackWrite) (string, error)
	UpStack(ctx context.Context, serverID, name string) (string, error)
	PickPort(ctx context.Context, serverID, name string) (int, error)
}

// SecretOpener decrypts the sealed deploy key and build variables.
type SecretOpener interface {
	Open(sealed []byte) ([]byte, error)
}

// ProxyModeSource reports how apps are published on a server.
type ProxyModeSource interface {
	ProxyMode(ctx context.Context, serverID string) agentv1.ProxyMode
}

// AppStore is the part of the store the deployment worker needs.
type AppStore interface {
	GetApp(ctx context.Context, id string) (store.App, error)
	ListAppEnv(ctx context.Context, appID string) ([]store.AppEnv, error)
	ListDeployments(ctx context.Context, appID string, limit int) ([]store.Deployment, error)
	CreateDeployment(ctx context.Context, deployment store.Deployment) (store.Deployment, error)
	UpdateDeployment(ctx context.Context, deployment store.Deployment) error
	SupersedeQueuedDeployments(ctx context.Context, appID string) (int64, error)
	ClaimNextDeployment(ctx context.Context) (store.Deployment, error)
	SetAppHostPort(ctx context.Context, appID string, port int) error
}

// Options are the collaborators of a Manager.
type Options struct {
	Store        AppStore
	Agent        Agent
	Box          SecretOpener
	Modes        ProxyModeSource
	Logger       *slog.Logger
	IdleInterval time.Duration
}

// Manager owns the deployment queue and its worker.
type Manager struct {
	store        AppStore
	agent        Agent
	box          SecretOpener
	modes        ProxyModeSource
	logger       *slog.Logger
	idleInterval time.Duration
	wake         chan struct{}
	mu           sync.Mutex
}

// New returns a Manager with the defaults filled in.
func New(options Options) *Manager {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	idle := options.IdleInterval
	if idle <= 0 {
		idle = DefaultIdleInterval
	}
	return &Manager{
		store:        options.Store,
		agent:        options.Agent,
		box:          options.Box,
		modes:        options.Modes,
		logger:       logger,
		idleInterval: idle,
		wake:         make(chan struct{}, 1),
	}
}

// Enqueue queues a deployment. A newer push marks the deployment that is
// waiting for its app as superseded, never the one already building.
func (m *Manager) Enqueue(ctx context.Context, appID, trigger, commit string) error {
	if strings.TrimSpace(trigger) == "" {
		trigger = "manual"
	}
	if !validTrigger(trigger) {
		return fmt.Errorf("invalid deployment trigger %q", trigger)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.store.GetApp(ctx, appID); err != nil {
		return err
	}
	if _, err := m.store.SupersedeQueuedDeployments(ctx, appID); err != nil {
		return err
	}
	if _, err := m.store.CreateDeployment(ctx, store.Deployment{
		AppID:     appID,
		Trigger:   trigger,
		CommitSHA: commit,
		State:     store.DeploymentQueued,
	}); err != nil {
		return err
	}
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return nil
}

// Run processes queued deployments until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	if cleaner, ok := m.store.(interface {
		FailInterruptedDeployments(context.Context) (int64, error)
	}); ok {
		if count, err := cleaner.FailInterruptedDeployments(ctx); err != nil {
			m.logger.Error("close interrupted deployments", "error", err)
		} else if count > 0 {
			m.logger.Warn("closed interrupted deployments", "count", count)
		}
	}

	for {
		deployment, err := m.store.ClaimNextDeployment(ctx)
		switch {
		case err == nil:
			m.process(ctx, deployment)
		case errors.Is(err, store.ErrNotFound):
			if !m.wait(ctx) {
				return nil
			}
		default:
			if ctx.Err() != nil {
				return nil
			}
			m.logger.Error("claim deployment", "error", err)
			if !m.wait(ctx) {
				return nil
			}
		}
	}
}

func (m *Manager) wait(ctx context.Context) bool {
	timer := time.NewTimer(m.idleInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-m.wake:
		return true
	case <-timer.C:
		return true
	}
}

func (m *Manager) process(ctx context.Context, deployment store.Deployment) {
	log := &deploymentLog{text: deployment.Log}
	app, err := m.store.GetApp(ctx, deployment.AppID)
	if err != nil {
		m.finish(ctx, deployment, log, err)
		return
	}
	if err := m.deploy(ctx, app, &deployment, log); err != nil {
		m.logger.Warn("deployment failed", "app_id", app.ID, "deployment_id", deployment.ID, "error", err)
		m.finish(ctx, deployment, log, err)
		return
	}
	m.finish(ctx, deployment, log, nil)
}

func (m *Manager) finish(ctx context.Context, deployment store.Deployment, log *deploymentLog, cause error) {
	deployment.FinishedAt = time.Now()
	deployment.Log = log.String()
	if cause != nil {
		deployment.State = store.DeploymentFailed
		deployment.Error = limitText(cause.Error(), 2000)
	} else {
		deployment.State = store.DeploymentSucceeded
		deployment.Error = ""
	}
	if err := m.store.UpdateDeployment(ctx, deployment); err != nil {
		m.logger.Error("save deployment result", "deployment_id", deployment.ID, "error", err)
	}
}

func (m *Manager) deploy(ctx context.Context, app store.App, deployment *store.Deployment, log *deploymentLog) error {
	service := app.Service
	if service == "" {
		service = "app"
	}
	if !serviceNameRe.MatchString(service) {
		return errors.New("invalid app service name")
	}
	if app.Port < 0 || app.Port > 65535 {
		return errors.New("invalid app port")
	}
	if !stackNameRe.MatchString(app.StackName) {
		return errors.New("invalid app stack name")
	}
	managed := false
	if m.modes != nil {
		managed = m.modes.ProxyMode(ctx, app.ServerID) == agentv1.ProxyMode_PROXY_MODE_MANAGED
	}

	imageRef := deployment.ImageRef
	tag := ""
	rollback := deployment.Trigger == "rollback"

	switch app.Source {
	case store.AppSourceGit:
		if rollback {
			ref, err := imageForRollback(app, deployment)
			if err != nil {
				return err
			}
			imageRef, tag = ref.image, ref.tag
			log.add("rolling back to " + imageRef)
		} else {
			if app.Build != store.AppBuildDockerfile {
				return errors.New("only Dockerfile builds are supported")
			}
			key, err := m.deployKey(app)
			if err != nil {
				return err
			}
			log.add("fetching " + app.Branch + " from " + app.RepoURL)
			sourceCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
			source, err := m.agent.Source(sourceCtx, app.ServerID, &agentv1.AppSource{
				Name:      app.StackName,
				RepoUrl:   app.RepoURL,
				Branch:    app.Branch,
				Commit:    deployment.CommitSHA,
				DeployKey: key,
			})
			cancel()
			if err != nil {
				return fmt.Errorf("fetch the source: %w", err)
			}
			if !validCommit(source.Commit) {
				return errors.New("the agent returned an invalid commit")
			}
			if source.Note != "" {
				log.add("source: " + source.Note)
			}
			if deployment.CommitSHA != "" && deployment.CommitSHA != source.Commit {
				log.add("the branch has moved; deploying its current tip")
			}
			deployment.CommitSHA = source.Commit
			deployment.CommitSubject = source.Subject
			tag = source.Commit[:12]
			imageRef = "talaia/" + app.StackName + ":" + tag
			log.add("source " + tag + " " + source.Subject)
			if err := m.save(ctx, *deployment, log); err != nil {
				return err
			}

			args, err := m.buildArgs(ctx, app)
			if err != nil {
				return err
			}
			log.add("building " + imageRef)
			buildCtx, cancel := context.WithTimeout(ctx, buildTimeout)
			output, err := m.agent.Build(buildCtx, app.ServerID, &agentv1.AppBuild{
				Name:           app.StackName,
				Tag:            tag,
				DockerfilePath: app.DockerfilePath,
				BuildContext:   app.BuildContext,
				BuildArgs:      args,
			})
			cancel()
			log.add(output)
			if err != nil {
				return fmt.Errorf("build the image: %w", err)
			}
			deployment.ImageRef = imageRef
			if err := m.save(ctx, *deployment, log); err != nil {
				return err
			}
		}

	case store.AppSourceImage:
		if deployment.ImageRef != "" {
			imageRef = deployment.ImageRef
		}
		if imageRef == "" {
			imageRef = app.Image
		}
		if strings.TrimSpace(imageRef) == "" {
			return errors.New("the app has no image")
		}
		log.add("deploying image " + imageRef)

	case store.AppSourceTemplate:
		return errors.New("template deployments are not available in this phase")

	default:
		return fmt.Errorf("unsupported app source %q", app.Source)
	}

	if !imageRefRe.MatchString(imageRef) {
		return errors.New("invalid app image")
	}

	hostPort := 0
	if !managed {
		if app.Port <= 0 || app.Port > 65535 {
			return errors.New("the app needs a port for 127.0.0.1")
		}
		hostPort = app.HostPort
		if hostPort == 0 {
			pickCtx, cancel := context.WithTimeout(ctx, portPickTimeout)
			picked, err := m.agent.PickPort(pickCtx, app.ServerID, app.StackName)
			cancel()
			if err != nil {
				return fmt.Errorf("pick the app host port: %w", err)
			}
			if picked < 20000 || picked > 29999 {
				return fmt.Errorf("the agent returned an invalid host port %d", picked)
			}
			if err := m.store.SetAppHostPort(ctx, app.ID, picked); err != nil {
				return fmt.Errorf("store the app host port: %w", err)
			}
			hostPort = picked
			app.HostPort = picked
			log.add(fmt.Sprintf("host port %d chosen by the agent", picked))
		} else if hostPort < 20000 || hostPort > 29999 {
			return fmt.Errorf("the stored app host port %d is outside 20000-29999", hostPort)
		}
	}

	deployment.State = store.DeploymentReleasing
	deployment.ImageRef = imageRef
	if err := m.save(ctx, *deployment, log); err != nil {
		return err
	}

	if err := m.prepareStack(ctx, app, service, imageRef, managed, hostPort, log); err != nil {
		return err
	}

	switch app.Source {
	case store.AppSourceGit:
		keepTags, err := m.keepTags(ctx, app)
		if err != nil {
			return err
		}
		releaseCtx, cancel := context.WithTimeout(ctx, releaseTimeout)
		output, err := m.agent.Release(releaseCtx, app.ServerID, &agentv1.AppRelease{
			Name:         app.StackName,
			Tag:          tag,
			Service:      service,
			ZeroDowntime: app.ZeroDowntime,
			Port:         uint32(app.Port),
			KeepTags:     keepTags,
		})
		cancel()
		log.add(output)
		if err != nil {
			return fmt.Errorf("release the app: %w", err)
		}
	case store.AppSourceImage:
		upCtx, cancel := context.WithTimeout(ctx, releaseTimeout)
		output, err := m.agent.UpStack(upCtx, app.ServerID, app.StackName)
		cancel()
		log.add(output)
		if err != nil {
			return fmt.Errorf("start the app: %w", err)
		}
	}
	log.add("deployment succeeded")
	return nil
}

type rollbackImage struct {
	image string
	tag   string
}

func imageForRollback(app store.App, deployment *store.Deployment) (rollbackImage, error) {
	ref := deployment.ImageRef
	if ref == "" {
		if len(deployment.CommitSHA) < 12 || !tagRe.MatchString(deployment.CommitSHA[:12]) {
			return rollbackImage{}, errors.New("the rollback deployment has no image")
		}
		ref = "talaia/" + app.StackName + ":" + deployment.CommitSHA[:12]
	}
	prefix := "talaia/" + app.StackName + ":"
	if !strings.HasPrefix(ref, prefix) {
		return rollbackImage{}, fmt.Errorf("invalid rollback image %q", ref)
	}
	tag := strings.TrimPrefix(ref, prefix)
	if !tagRe.MatchString(tag) {
		return rollbackImage{}, fmt.Errorf("invalid rollback tag %q", tag)
	}
	return rollbackImage{image: ref, tag: tag}, nil
}

func (m *Manager) prepareStack(ctx context.Context, app store.App, service, imageRef string, managed bool, hostPort int, log *deploymentLog) error {
	readCtx, cancel := context.WithTimeout(ctx, stackTimeout)
	compose, env, err := m.agent.ReadStack(readCtx, app.ServerID, app.StackName)
	cancel()
	if errors.Is(err, ErrStackNotFound) {
		generated, err := generateCompose(app, service, imageRef, managed, hostPort)
		if err != nil {
			return err
		}
		log.add("writing the initial stack")
		return m.writeStack(ctx, app, generated, "", log)
	}
	if err != nil {
		return fmt.Errorf("read the app stack: %w", err)
	}
	updated, found := setServiceImage(compose, service, imageRef)
	if !found {
		log.add("warning: the service " + service + " no longer exists in the hand-edited stack")
		return fmt.Errorf("the service %q no longer exists in the hand-edited stack", service)
	}
	log.add("the stack is hand-edited; only the image of " + service + " changes")
	return m.writeStack(ctx, app, updated, env, log)
}

func (m *Manager) writeStack(ctx context.Context, app store.App, compose, env string, log *deploymentLog) error {
	writeCtx, cancel := context.WithTimeout(ctx, stackTimeout)
	output, err := m.agent.WriteStack(writeCtx, app.ServerID, &agentv1.StackWrite{
		Name:        app.StackName,
		ComposeYaml: compose,
		Env:         env,
	})
	cancel()
	log.add(output)
	if err != nil {
		return fmt.Errorf("write the app stack: %w", err)
	}
	return nil
}

func (m *Manager) deployKey(app store.App) (string, error) {
	if len(app.DeployKeyEnc) == 0 {
		return "", nil
	}
	if m.box == nil {
		return "", errors.New("the deploy key is sealed and no secret box is available")
	}
	plain, err := m.box.Open(app.DeployKeyEnc)
	if err != nil {
		return "", fmt.Errorf("open the deploy key: %w", err)
	}
	return string(plain), nil
}

func (m *Manager) buildArgs(ctx context.Context, app store.App) ([]*agentv1.BuildArg, error) {
	envs, err := m.store.ListAppEnv(ctx, app.ID)
	if err != nil {
		return nil, fmt.Errorf("list the app variables: %w", err)
	}
	args := make([]*agentv1.BuildArg, 0)
	for _, env := range envs {
		if !env.BuildTime || env.Secret {
			continue
		}
		value := ""
		if len(env.ValueEnc) > 0 {
			if m.box == nil {
				return nil, fmt.Errorf("the variable %q is sealed and no secret box is available", env.Key)
			}
			plain, err := m.box.Open(env.ValueEnc)
			if err != nil {
				return nil, fmt.Errorf("open the variable %q: %w", env.Key, err)
			}
			value = string(plain)
		}
		args = append(args, &agentv1.BuildArg{Key: env.Key, Value: value})
	}
	return args, nil
}

func (m *Manager) keepTags(ctx context.Context, app store.App) ([]string, error) {
	deployments, err := m.store.ListDeployments(ctx, app.ID, 50)
	if err != nil {
		return nil, fmt.Errorf("list the previous deployments: %w", err)
	}
	prefix := "talaia/" + app.StackName + ":"
	seen := map[string]bool{}
	tags := make([]string, 0, 3)
	for _, item := range deployments {
		if item.State != store.DeploymentSucceeded || !strings.HasPrefix(item.ImageRef, prefix) {
			continue
		}
		tag := strings.TrimPrefix(item.ImageRef, prefix)
		if !tagRe.MatchString(tag) || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
		if len(tags) == 3 {
			break
		}
	}
	return tags, nil
}

func (m *Manager) save(ctx context.Context, deployment store.Deployment, log *deploymentLog) error {
	deployment.Log = log.String()
	if err := m.store.UpdateDeployment(ctx, deployment); err != nil {
		return fmt.Errorf("save the deployment: %w", err)
	}
	return nil
}

type deploymentLog struct {
	text string
}

func (l *deploymentLog) add(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if l.text != "" {
		l.text += "\n"
	}
	l.text += text
	l.text = capLog(l.text)
}

func (l *deploymentLog) String() string { return l.text }

func capLog(text string) string {
	if len(text) <= store.MaxDeploymentLog {
		return text
	}
	start := len(text) - store.MaxDeploymentLog
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	return text[start:]
}

func limitText(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	return strings.ToValidUTF8(text[:max], "")
}

func validTrigger(trigger string) bool {
	switch trigger {
	case "push", "poll", "manual", "rollback", "create":
		return true
	default:
		return false
	}
}

func validCommit(commit string) bool {
	return len(commit) == 40 && commitRe.MatchString(commit)
}

// jsonSourceResult is kept separate so the agent output parser is explicit.
func jsonSourceResult(output string) (SourceResult, error) {
	var result SourceResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return SourceResult{}, fmt.Errorf("decode the source result: %w", err)
	}
	return result, nil
}
