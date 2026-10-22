package jobs

import (
	"context"
	"errors"
	"time"
)

const (
	// aptStatusTimeout bounds a cached AptStatus query.
	aptStatusTimeout = 2 * time.Minute
	// aptStatusRefreshTimeout bounds AptStatus with apt-get update.
	aptStatusRefreshTimeout = 10 * time.Minute
	// maxAptStatusBytes is the largest JSON the job sends as its output.
	maxAptStatusBytes = 256 << 10
)

// errNoApt is the failure of an apt status command on an agent without apt.
var errNoApt = errors.New("apt is not available on this agent")

// AptStatusReader answers the AptStatus job with the JSON of the full list.
type AptStatusReader interface {
	StatusJSON(ctx context.Context, refresh bool) (string, error)
}

// WithApt lets the executor run apt status commands.
func (e *Executor) WithApt(reader AptStatusReader) *Executor {
	e.apt = reader
	return e
}
