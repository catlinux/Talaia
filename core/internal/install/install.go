// Package install builds the command that installs the agent on a server.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// ScriptSHA256 returns the SHA-256 of install.sh in dir, or "" when it cannot be
// read. The command embeds it so the server can check the script it downloads.
func ScriptSHA256(dir string) string {
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "install.sh"))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Command returns the one-line command a server runs to install its agent. It
// downloads install.sh from the core, checks it against scriptSHA256 and only
// then runs it as root, passing the address, the one-time token and the CA
// fingerprint the agent will trust.
//
// The script is fetched without verifying the TLS certificate, because the
// server does not trust the core CA yet. The hash travels in the command, which
// the administrator copies from the panel or the console, so a machine in the
// middle cannot swap the script. It returns "" when the core has no public
// address or no script to hash.
func Command(publicURL, token, fingerprint, scriptSHA256 string) string {
	if publicURL == "" || scriptSHA256 == "" {
		return ""
	}
	return fmt.Sprintf(`f=$(mktemp) && curl -fsSk %s/install.sh -o "$f" && echo "%s  $f" | sha256sum -c - && sudo bash "$f" --url %s --token %s --ca-fingerprint %s; rm -f "$f"`,
		publicURL, scriptSHA256, publicURL, token, fingerprint)
}
