// Package install builds the command that installs the agent on a server.
package install

import "fmt"

// Command returns the one-line command a server runs to install its agent: it
// fetches install.sh from the core and passes it the address, the one-time
// token and the CA fingerprint the agent will trust. It returns "" when the
// core has no public address to reach it at.
func Command(publicURL, token, fingerprint string) string {
	if publicURL == "" {
		return ""
	}
	return fmt.Sprintf("curl -fsSk %s/install.sh | sudo bash -s -- --url %s --token %s --ca-fingerprint %s",
		publicURL, publicURL, token, fingerprint)
}
