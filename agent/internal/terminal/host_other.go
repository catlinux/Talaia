//go:build !linux

package terminal

import (
	"context"
	"errors"
)

type unsupportedHostBackend struct{}

// NewHostBackend returns a backend that refuses host sessions on this platform.
func NewHostBackend() HostBackend { return unsupportedHostBackend{} }

func (unsupportedHostBackend) Start(context.Context, []string, uint32, uint32) (Process, error) {
	return nil, errors.New("host terminal is only supported on Linux")
}
