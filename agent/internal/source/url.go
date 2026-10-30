package source

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

type repoKind int

const (
	repoHTTPS repoKind = iota
	repoSSH
)

var (
	repoPartRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	branchRe   = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	commitRe   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// validateRepoURL accepts only GitHub repositories in the two forms the agent
// supports. It returns whether the repository is reached over HTTPS or SSH.
func validateRepoURL(raw string) (repoKind, error) {
	if raw == "" {
		return 0, errors.New("repository URL is required")
	}
	if strings.HasPrefix(raw, "-") {
		return 0, errors.New("repository URL cannot start with a dash")
	}
	for _, r := range raw {
		if r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return 0, errors.New("repository URL contains whitespace, control characters or a backslash")
		}
	}
	if strings.ContainsAny(raw, "?#") {
		return 0, errors.New("repository URL cannot have a query or fragment")
	}
	if strings.Contains(raw, "%") {
		return 0, errors.New("repository URL cannot be percent-encoded")
	}

	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "https://"):
		return validateURL(raw, "https", "443")
	case strings.HasPrefix(lower, "ssh://"):
		return validateURL(raw, "ssh", "22")
	case strings.HasPrefix(raw, "git@"):
		return validateSCP(raw)
	default:
		return 0, errors.New("repository URL must be an HTTPS or SSH URL of github.com")
	}
}

func validateURL(raw, scheme, defaultPort string) (repoKind, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" {
		return 0, errors.New("invalid repository URL")
	}
	if !strings.EqualFold(parsed.Scheme, scheme) {
		return 0, errors.New("invalid repository URL scheme")
	}
	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return 0, errors.New("repository host must be github.com")
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return 0, errors.New("invalid repository port")
	}
	if port := parsed.Port(); port != "" && port != defaultPort {
		return 0, errors.New("repository port must be the default one")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return 0, errors.New("repository URL cannot have a query or fragment")
	}
	if scheme == "https" {
		if parsed.User != nil {
			return 0, errors.New("HTTPS repository URL cannot have a user")
		}
		return repoHTTPS, validateRepoPath(parsed.Path)
	}

	if !validGitUser(parsed.User) {
		return 0, errors.New("SSH repository URL must use the git user")
	}
	return repoSSH, validateRepoPath(parsed.Path)
}

func validateSCP(raw string) (repoKind, error) {
	rest := strings.TrimPrefix(raw, "git@")
	host, path, ok := strings.Cut(rest, ":")
	if !ok || !strings.EqualFold(host, "github.com") {
		return 0, errors.New("repository host must be github.com")
	}
	if validateRepoPath("/"+path) != nil {
		return 0, errors.New("invalid repository path")
	}
	return repoSSH, nil
}

func validGitUser(user *url.Userinfo) bool {
	if user == nil || user.Username() != "git" {
		return false
	}
	_, hasPassword := user.Password()
	return !hasPassword
}

func validateRepoPath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return errors.New("repository path must start with a slash")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("repository path must have an owner and a name")
	}
	for _, part := range parts {
		if part == "." || part == ".." || !repoPartRe.MatchString(part) {
			return errors.New("invalid repository owner or name")
		}
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	if repo == "" || repo == "." || repo == ".." || !repoPartRe.MatchString(repo) {
		return errors.New("invalid repository name")
	}
	return nil
}

func validBranch(branch string) bool {
	if branch == "" || len(branch) > 200 || branch[0] == '-' || branch[0] == '/' {
		return false
	}
	if strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") ||
		strings.HasSuffix(branch, ".lock") || strings.Contains(branch, "..") ||
		!branchRe.MatchString(branch) {
		return false
	}
	for _, part := range strings.Split(branch, "/") {
		if part == "" || strings.HasPrefix(part, ".") ||
			strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func validCommit(commit string) bool {
	return commit == "" || commitRe.MatchString(commit)
}
