package jobs

import (
	"context"
	"errors"
	"time"
)

// updateTimeout bounds a complete agent update: the download, the verification
// and the start of the detached installer.
const updateTimeout = 10 * time.Minute

// errNoUpdates is the failure of an update command on an executor without an
// updater.
var errNoUpdates = errors.New("updates are not available on this agent")

// Updater downloads and starts the installation of an agent package.
type Updater interface {
	Update(ctx context.Context, version, sha256Hex string) (string, error)
}

// WithUpdates lets the executor run agent update commands.
func (e *Executor) WithUpdates(updater Updater) *Executor {
	e.updates = updater
	return e
}
