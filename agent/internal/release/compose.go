package release

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	tagRe        = regexp.MustCompile(`^[a-f0-9]{12}$`)
	serviceRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	containerRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	imageTagRe   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

// setServiceImage changes only the image of service in a Compose file. The
// rest of the file, including comments and formatting, is kept as it was.
func setServiceImage(compose, service, image string) (string, error) {
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
		return "", errors.New("compose.yaml has no services section")
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
		return "", fmt.Errorf("the compose service %q does not exist", service)
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
			return strings.Join(lines, newline), nil
		}
	}

	if strings.Contains(strings.TrimSpace(lines[serviceLine]), "{}") {
		lines[serviceLine] = strings.Repeat(" ", serviceIndent) + service + ":"
	}
	insert := strings.Repeat(" ", propertyIndent) + "image: " + image
	lines = append(lines[:serviceLine+1], append([]string{insert}, lines[serviceLine+1:]...)...)
	return strings.Join(lines, newline), nil
}

func indentOf(line string) int {
	count := 0
	for count < len(line) && (line[count] == ' ' || line[count] == '\t') {
		count++
	}
	return count
}
