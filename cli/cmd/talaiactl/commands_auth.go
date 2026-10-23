package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/catlinux/Talaia/cli/i18n"
)

// login verifies an API key, prints the user name and saves the configuration.
func (a *app) login(localizer *i18n.Localizer, opts options, args []string) int {
	if len(args) != 0 {
		return a.usageError(localizer)
	}
	path, err := a.configFile()
	if err != nil {
		return a.fail(localizer, err)
	}
	cfg, code := a.readConfig(localizer, path)
	if code >= 0 {
		return code
	}

	rawURL := firstValue(opts.url, a.getenv("TALAIA_URL"), cfg.URL)
	if rawURL == "" {
		fmt.Fprint(a.stdout, localizer.T("login.urlPrompt", nil)+" ")
		rawURL, err = a.readLine()
		if err != nil {
			return a.fail(localizer, err)
		}
	}
	if strings.TrimSpace(rawURL) == "" {
		return a.fail(localizer, errMissingURL)
	}
	normalized, err := normalizeURL(rawURL)
	if err != nil {
		return a.fail(localizer, err)
	}

	key := firstValue(opts.apiKey, a.getenv("TALAIA_API_KEY"), cfg.APIKey)
	if key == "" {
		fmt.Fprint(a.stdout, localizer.T("login.apiKeyPrompt", nil)+" ")
		key, err = a.readSecret()
		if err != nil {
			return a.fail(localizer, err)
		}
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return a.fail(localizer, errMissingAPIKey)
	}

	client, err := newAPIClient(normalized, key, opts.caFile, a.version)
	if err != nil {
		return a.fail(localizer, err)
	}
	me, err := client.getMe(context.Background())
	if err != nil {
		return a.fail(localizer, err)
	}
	if err := saveConfig(path, config{URL: normalized, APIKey: key}); err != nil {
		return a.fail(localizer, &configWriteError{path: path, err: err})
	}
	fmt.Fprintln(a.stdout, localizer.T("login.success", map[string]any{"User": me.User.Username}))
	return 0
}

// readLine reads one line from the shared stdin reader. It accepts a final
// unterminated line and strips CR from CRLF input.
func (a *app) readLine() (string, error) {
	if a.reader == nil {
		a.reader = bufio.NewReader(a.stdin)
	}
	line, err := a.reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readSecret reads a key without echo when stdin is a terminal.
func (a *app) readSecret() (string, error) {
	if file, ok := a.stdin.(*os.File); ok && isTerminal(file) {
		if restore, err := disableEcho(file); err == nil {
			defer restore()
			value, readErr := a.readLine()
			fmt.Fprintln(a.stdout)
			return value, readErr
		}
	}
	return a.readLine()
}
