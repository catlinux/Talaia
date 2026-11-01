package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// probeTimeout bounds one branch probe so an offline server or an agent that
// does not know the command cannot stall the whole poller.
const probeTimeout = 45 * time.Second

// Probe returns the commit the app branch points at.
func (m *Manager) Probe(ctx context.Context, app store.App) (string, error) {
	if m.agent == nil {
		return "", errors.New("the deployment agent is not configured")
	}
	key, err := m.deployKey(app)
	if err != nil {
		return "", err
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	commit, err := m.agent.Probe(probeCtx, app.ServerID, &agentv1.GitProbe{
		RepoUrl:   app.RepoURL,
		Branch:    app.Branch,
		DeployKey: key,
	})
	if err != nil {
		return "", fmt.Errorf("probe the app branch: %w", redactProbeError(err, key))
	}
	return commit, nil
}

// redactedProbeError hides a deploy key an agent error may echo, while
// keeping the original error available to errors.Is and errors.As.
type redactedProbeError struct {
	err    error
	secret string
}

func (e redactedProbeError) Error() string {
	return strings.ReplaceAll(e.err.Error(), e.secret, "[redacted]")
}

func (e redactedProbeError) Unwrap() error {
	return e.err
}

func redactProbeError(err error, secret string) error {
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return redactedProbeError{err: err, secret: secret}
}
