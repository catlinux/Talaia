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

func generateCompose(app store.App, service, imageRef string, managed bool, hostPort int) (string, error) {
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
    env_file: [.env]
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
	if hostPort < 20000 || hostPort > 29999 {
		return "", errors.New("the app needs a host port in 20000-29999")
	}
	return fmt.Sprintf(`services:
  %s:
    image: %s
    restart: unless-stopped
    cpus: "%s"
    mem_limit: %s
    env_file: [.env]
    ports:
      - "127.0.0.1:%d:%d"
    labels:
      dev.talaia.app: %s
`, service, imageRef, DefaultCPUs, DefaultMemory, hostPort, app.Port, app.ID), nil
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

type serviceBlock struct {
	lines          []string
	newline        string
	start          int
	end            int
	propertyIndent int
}

// locateService returns the lines and bounds of one service in a Compose file.
// It works on the text instead of parsing YAML so the rest of the file, its
// comments and its formatting stay untouched.
func locateService(compose, service string) (serviceBlock, bool) {
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
		return serviceBlock{}, false
	}

	start := -1
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
			start = index
			serviceIndent = indent
			break
		}
	}
	if start < 0 {
		return serviceBlock{}, false
	}

	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
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
	for index := start + 1; index < end; index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		propertyIndent = indentOf(lines[index])
		break
	}

	return serviceBlock{
		lines:          lines,
		newline:        newline,
		start:          start,
		end:            end,
		propertyIndent: propertyIndent,
	}, true
}

// addServiceEnvFile makes the service read .env and reports whether the service
// was found. An existing env_file property is left as it is.
func addServiceEnvFile(compose, service string) (string, bool) {
	block, ok := locateService(compose, service)
	if !ok {
		return compose, false
	}
	if serviceHasEnvFileInBlock(block) {
		return strings.Join(block.lines, block.newline), true
	}
	insert := strings.Repeat(" ", block.propertyIndent) + "env_file: [.env]"
	lines := make([]string, 0, len(block.lines)+1)
	lines = append(lines, block.lines[:block.start+1]...)
	lines = append(lines, insert)
	lines = append(lines, block.lines[block.start+1:]...)
	return strings.Join(lines, block.newline), true
}

// serviceHasEnvFile reports whether the service already declares env_file.
func serviceHasEnvFile(compose, service string) bool {
	block, ok := locateService(compose, service)
	return ok && serviceHasEnvFileInBlock(block)
}

func serviceHasEnvFileInBlock(block serviceBlock) bool {
	for index := block.start + 1; index < block.end; index++ {
		trimmed := strings.TrimSpace(block.lines[index])
		if strings.HasPrefix(trimmed, "env_file:") {
			return true
		}
	}
	return false
}
