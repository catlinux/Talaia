package source

import (
	"strings"
	"testing"
)

func TestRepoURLValidation(t *testing.T) {
	valid := []struct {
		url  string
		kind repoKind
	}{
		{"https://github.com/owner/repo", repoHTTPS},
		{"https://github.com/owner/repo.git", repoHTTPS},
		{"https://GitHub.com/owner/repo.git", repoHTTPS},
		{"https://github.com:443/owner/repo", repoHTTPS},
		{"ssh://git@github.com/owner/repo", repoSSH},
		{"ssh://git@github.com/owner/repo.git", repoSSH},
		{"ssh://git@GitHub.com:22/owner/repo", repoSSH},
		{"git@github.com:owner/repo", repoSSH},
		{"git@github.com:owner/repo.git", repoSSH},
		{"git@GitHub.com:owner/repo.git", repoSSH},
	}
	for _, test := range valid {
		kind, err := validateRepoURL(test.url)
		if err != nil {
			t.Errorf("%q was rejected: %v", test.url, err)
			continue
		}
		if kind != test.kind {
			t.Errorf("%q kind = %v, want %v", test.url, kind, test.kind)
		}
	}

	invalid := []string{
		"",
		"http://github.com/owner/repo",
		"git://github.com/owner/repo",
		"file:///tmp/repo",
		"ext::sh -c echo",
		"/tmp/repo",
		"C:\\repo",
		"github.com/owner/repo",
		"ssh://github.com/owner/repo",
		"ssh://user@github.com/owner/repo",
		"ssh://git:pass@github.com/owner/repo",
		"ssh://git@github.com:2222/owner/repo",
		"git@gitlab.com:owner/repo",
		"git@github.com.evil:owner/repo",
		"https://gitlab.com/owner/repo",
		"https://github.com.evil.com/owner/repo",
		"https://evilgithub.com/owner/repo",
		"https://github.com@evil.com/owner/repo",
		"https://user:pass@github.com/owner/repo",
		"https://github.com:8443/owner/repo",
		"https://github.com/owner/repo?ref=main",
		"https://github.com/owner/repo#main",
		"https://github.com/owner%2Frepo",
		"https://github.com/owner/re%70o",
		"https://github.com/owner/../repo",
		"https://github.com/./repo",
		"https://github.com/owner/repo/",
		"https://github.com/owner",
		"https://github.com/owner/repo/extra",
		"https://github.com//owner/repo",
		"https://github.com/owner/repo name",
		"https://github.com/owner/repo\t",
		"https://github.com/owner/repo\\name",
		"-https://github.com/owner/repo",
		"git@github.com:/owner/repo",
		"git@github.com:../repo",
		"git@github.com:owner/repo/extra",
	}
	for _, raw := range invalid {
		if _, err := validateRepoURL(raw); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}
}

func TestBranchValidation(t *testing.T) {
	for _, branch := range []string{"main", "feature/login", "release-1.2", "a_b", strings.Repeat("a", 200)} {
		if !validBranch(branch) {
			t.Errorf("%q should be a valid branch", branch)
		}
	}
	for _, branch := range []string{
		"", "-main", "/main", "main/", "main.", "main.lock", "a..b", "a b", "a:b", "a\\b",
		strings.Repeat("a", 201), "feature//login", "feature/./login", "feature/.hidden",
	} {
		if validBranch(branch) {
			t.Errorf("%q should be an invalid branch", branch)
		}
	}
}

func TestCommitValidation(t *testing.T) {
	if !validCommit("") || !validCommit(strings.Repeat("a", 40)) {
		t.Fatal("a valid commit was rejected")
	}
	for _, commit := range []string{
		strings.Repeat("a", 39),
		strings.Repeat("a", 41),
		strings.Repeat("A", 40),
		strings.Repeat("g", 40),
		"-" + strings.Repeat("a", 39),
	} {
		if validCommit(commit) {
			t.Errorf("%q should be an invalid commit", commit)
		}
	}
}
