package deploy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/catlinux/Talaia/core/internal/store"
)

const templatePasswordKey = "TEMPLATE_PASSWORD"

// renderEnvFile renders the variables of an app as a Compose .env file. The
// template password is internal to the template and is never exported. The
// returned count is the number of variables written.
func renderEnvFile(envs []store.AppEnv, opener SecretOpener) (string, int, error) {
	selected := make([]store.AppEnv, 0, len(envs))
	for _, env := range envs {
		if env.Key == templatePasswordKey {
			continue
		}
		selected = append(selected, env)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Key < selected[j].Key })

	lines := make([]string, 0, len(selected))
	for _, env := range selected {
		value := ""
		if len(env.ValueEnc) > 0 {
			if opener == nil {
				return "", 0, fmt.Errorf("the variable %q is sealed and no secret box is available", env.Key)
			}
			plain, err := opener.Open(env.ValueEnc)
			if err != nil {
				return "", 0, fmt.Errorf("open the variable %q: %w", env.Key, err)
			}
			value = string(plain)
		}
		if strings.IndexByte(value, 0) >= 0 {
			return "", 0, fmt.Errorf("the variable %q contains a NUL byte", env.Key)
		}
		lines = append(lines, env.Key+`="`+escapeEnvValue(value)+`"`)
	}
	if len(lines) == 0 {
		return "", 0, nil
	}
	return strings.Join(lines, "\n") + "\n", len(lines), nil
}

// envValueEscaper makes a value safe inside the double quotes of a Compose
// .env line. Compose reads \n, \" and \\ inside double quotes and uses $$ for a
// literal dollar.
var envValueEscaper = strings.NewReplacer(
	"\\", "\\\\",
	`"`, `\"`,
	"\n", "\\n",
	"\r", "",
	"$", "$$",
)

func escapeEnvValue(value string) string {
	return envValueEscaper.Replace(value)
}

// emptyEnvFile keeps a referenced .env in place when the app has no variables:
// the agent removes the file when the content is empty and Compose would then
// fail to find it.
func emptyEnvFile() string {
	return "\n"
}

// envFileContent keeps the .env file present: the agent removes a zero-byte
// file, and Compose would then miss it wherever the service references it.
func envFileContent(env string) string {
	if env == "" {
		return emptyEnvFile()
	}
	return env
}

func (m *Manager) renderAppEnv(ctx context.Context, app store.App) (string, int, error) {
	envs, err := m.store.ListAppEnv(ctx, app.ID)
	if err != nil {
		return "", 0, fmt.Errorf("list the app variables: %w", err)
	}
	env, count, err := renderEnvFile(envs, m.box)
	if err != nil {
		return "", 0, fmt.Errorf("render the app variables: %w", err)
	}
	return env, count, nil
}
