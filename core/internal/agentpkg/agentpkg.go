// Package agentpkg reads the agent Debian packages served by the core and
// compares agent versions.
package agentpkg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

	// cache keeps the hash of the package files. It is keyed by path and only
	// reused while the size and the modification time are unchanged.
	cache sync.Map
)

// cachedPackage is the hash and the stat that produced it.
type cachedPackage struct {
	size     int64
	modified time.Time
	sha256   string
}

// PackageName returns the name of the package served for arch.
func PackageName(arch string) (string, bool) {
	switch arch {
	case "amd64", "arm64":
		return "talaia-agent_" + arch + ".deb", true
	default:
		return "", false
	}
}

// NormalizeArch turns the names agents report into the two package names the
// core serves.
func NormalizeArch(arch string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "amd64", "x86_64", "x86-64":
		return "amd64", true
	case "arm64", "aarch64":
		return "arm64", true
	default:
		return "", false
	}
}

// Info returns the SHA-256 in hexadecimal of the package for arch. It hashes the
// file again only when its size or modification time changed. A missing package
// or an unknown architecture is an error.
func Info(distDir, arch string) (string, error) {
	name, ok := PackageName(arch)
	if !ok {
		return "", fmt.Errorf("unsupported agent architecture %q", arch)
	}
	if strings.TrimSpace(distDir) == "" {
		return "", fmt.Errorf("the agent package directory is empty")
	}

	path := filepath.Join(distDir, name)
	stat, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat agent package %s: %w", name, err)
	}
	if stat.IsDir() {
		return "", fmt.Errorf("agent package %s is a directory", name)
	}
	if value, ok := cache.Load(path); ok {
		entry, ok := value.(cachedPackage)
		if ok && entry.size == stat.Size() && entry.modified.Equal(stat.ModTime()) {
			return entry.sha256, nil
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open agent package %s: %w", name, err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("hash agent package %s: %w", name, err)
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	cache.Store(path, cachedPackage{size: stat.Size(), modified: stat.ModTime(), sha256: sum})
	return sum, nil
}

// MinSelfUpdate is the first agent version that understands the update order.
// Older agents ignore it, so they need one manual upgrade.
const MinSelfUpdate = "0.2.8"

// SupportsSelfUpdate reports whether an agent of this version can update itself
// from the panel. A version that is not x.y.z, such as "dev", is assumed to.
func SupportsSelfUpdate(running string) bool {
	if !ValidVersion(running) {
		return true
	}
	return !Newer(MinSelfUpdate, running)
}

// ValidVersion reports whether version is x.y.z with numeric components.
func ValidVersion(version string) bool {
	_, ok := parseVersion(version)
	return ok
}

// Newer reports whether offered is a newer x.y.z than running. An offered
// version that is not x.y.z is never newer. A running version that is not
// x.y.z, such as "dev" or an empty string, cannot be compared, so any valid
// offered version is accepted.
func Newer(offered, running string) bool {
	next, ok := parseVersion(offered)
	if !ok {
		return false
	}
	current, ok := parseVersion(running)
	if !ok {
		return true
	}
	for index := range next {
		if next[index] != current[index] {
			return next[index] > current[index]
		}
	}
	return false
}

// parseVersion parses x.y.z with no leading zeroes.
func parseVersion(version string) ([3]uint64, bool) {
	var parts [3]uint64
	if !versionPattern.MatchString(version) {
		return parts, false
	}
	for index, value := range strings.Split(version, ".") {
		number, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return [3]uint64{}, false
		}
		parts[index] = number
	}
	return parts, true
}
