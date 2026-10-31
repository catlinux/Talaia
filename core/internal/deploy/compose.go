package deploy

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/catlinux/Talaia/core/internal/store"
)

const (
	// DefaultCPUs and DefaultMemory are the limits of a new app.
	DefaultCPUs   = "1.0"
	DefaultMemory = "512m"
)

var (
	stackNameRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	tagRe         = regexp.MustCompile(`^[a-f0-9]{12}$`)
	commitRe      = regexp.MustCompile(`^[a-f0-9]{40}$`)
	serviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	imageRefRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:/@-]{0,255}$`)
)

func generateCompose(app store.App, service, imageRef string, managed bool) (string, error) {
	if !serviceNameRe.MatchString(service) {
		return "", errors.New("invalid app service name")
	}
	if !imageRefRe.MatchString(imageRef) {
		return "", errors.New("invalid app image")
	}
	if managed {
		return fmt.Sprintf(`services:
  %s:
    image: %s
    restart: unless-stopped
    cpus: "%s"
    mem_limit: %s
    labels:
      dev.talaia.app: %s
    networks:
      - talaia-proxy
networks:
  talaia-proxy:
    external: true
`, service, imageRef, DefaultCPUs, DefaultMemory, app.ID), nil
	}
	if app.Port <= 0 || app.Port > 65535 {
		return "", errors.New("the app needs a port for 127.0.0.1")
	}
	return fmt.Sprintf(`services:
  %s:
    image: %s
    restart: unless-stopped
    cpus: "%s"
    mem_limit: %s
    ports:
      - "127.0.0.1:%d:%d"
    labels:
      dev.talaia.app: %s
`, service, imageRef, DefaultCPUs, DefaultMemory, app.Port, app.Port, app.ID), nil
}

// setServiceImage changes only the image of service in an existing Compose
// file. It reports whether the service was found.
func setServiceImage(compose, service, image string) (string, bool) {
	newline := "\n"
	if strings.Contains(compose, "\r\n") {
		newline = "\r\n"
		compose = strings.ReplaceAll(compose, "\r\n", "\n")
	}
	lines := strings.Split(compose, "\n")

	servicesLine := -1
	for index, line := range lines {
		if indentOf(line) == 0 && strings.TrimSpace(line) == "services:" {
			servicesLine = index
			break
		}
	}
	if servicesLine < 0 {
		return compose, false
	}

	serviceLine := -1
	serviceIndent := 0
	for index := servicesLine + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := indentOf(lines[index])
		if indent == 0 {
			break
		}
		if strings.Trim(strings.TrimSuffix(trimmed, ":"), `"'`) == service {
			serviceLine = index
			serviceIndent = indent
			break
		}
	}
	if serviceLine < 0 {
		return compose, false
	}

	end := len(lines)
	for index := serviceLine + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if indentOf(lines[index]) <= serviceIndent {
			end = index
			break
		}
	}

	propertyIndent := serviceIndent + 2
	propertyIndentSet := false
	for index := serviceLine + 1; index < end; index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := indentOf(lines[index])
		if !propertyIndentSet {
			propertyIndent = indent
			propertyIndentSet = true
		}
		if indent == propertyIndent && strings.HasPrefix(trimmed, "image:") {
			lines[index] = strings.Repeat(" ", indent) + "image: " + image
			return strings.Join(lines, newline), true
		}
	}

	if strings.Contains(strings.TrimSpace(lines[serviceLine]), "{}") {
		lines[serviceLine] = strings.Repeat(" ", serviceIndent) + service + ":"
	}
	insert := strings.Repeat(" ", propertyIndent) + "image: " + image
	lines = append(lines[:serviceLine+1], append([]string{insert}, lines[serviceLine+1:]...)...)
	return strings.Join(lines, newline), true
}

func indentOf(line string) int {
	count := 0
	for count < len(line) && (line[count] == ' ' || line[count] == '\t') {
		count++
	}
	return count
}
