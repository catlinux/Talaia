//go:build linux

package build

import (
	"fmt"
	"syscall"
)

// freeBytes reports the space available to unprivileged processes on the
// filesystem of path.
func freeBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
