// Command talaiactl is the Talaia command-line client.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/catlinux/Talaia/cli/i18n"
)

// version is the version placeholder, overridden at build time.
var version = "dev"

// app carries the process dependencies so tests can drive the CLI in memory.
type app struct {
	version      string
	stdin        io.Reader
	stdout       io.Writer
	stderr       io.Writer
	getenv       func(string) string
	configPath   string
	pollInterval time.Duration
	reader       *bufio.Reader
}

func newApp() *app {
	return &app{
		version:      version,
		stdin:        os.Stdin,
		stdout:       os.Stdout,
		stderr:       os.Stderr,
		getenv:       os.Getenv,
		pollInterval: 2 * time.Second,
	}
}

func main() {
	os.Exit(newApp().run(os.Args[1:]))
}

// options are the global flags accepted before or after the command.
type options struct {
	lang    string
	url     string
	apiKey  string
	caFile  string
	json    bool
	wait    bool
	help    bool
	timeout time.Duration
}

// run parses the global flags, creates the localizer and dispatches the command.
func (a *app) run(args []string) int {
	flagArgs, positional := splitArgs(args)
	hint := flagValue(flagArgs, "lang")
	language := i18n.Resolve(hint, a.getenv)
	localizer, err := i18n.New(language)
	if err != nil {
		fmt.Fprintln(a.stderr, "talaiactl:", err)
		return 1
	}
	if hint != "" && !knownLanguage(hint) {
		fmt.Fprintln(a.stderr, localizer.T("error.unknownLanguage", map[string]any{"Language": hint}))
	}

	opts, err := a.parseFlags(flagArgs)
	if err != nil {
		return a.usageError(localizer)
	}
	if opts.help {
		a.printUsage(localizer)
		return 0
	}
	if len(positional) == 0 {
		a.printUsage(localizer)
		return 0
	}
	command, rest := positional[0], positional[1:]
	switch command {
	case "version":
		if len(rest) != 0 {
			return a.usageError(localizer)
		}
		fmt.Fprintln(a.stdout, localizer.T("version.line", map[string]any{"Version": a.version}))
		return 0
	case "help":
		a.printUsage(localizer)
		return 0
	case "login":
		return a.login(localizer, opts, rest)
	case "servers":
		return a.serversCommand(localizer, opts, rest)
	case "containers":
		return a.containersCommand(localizer, opts, rest)
	case "stacks":
		return a.stacksCommand(localizer, opts, rest)
	case "jobs":
		return a.jobsCommand(localizer, opts, rest)
	default:
		return a.usageErrorWith(localizer, localizer.T("error.unknownCommand", map[string]any{
			"Command": command,
		}))
	}
}

// parseFlags parses the global flags after they have been separated from the
// positional command words.
func (a *app) parseFlags(args []string) (options, error) {
	var opts options
	flags := flag.NewFlagSet("talaiactl", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.lang, "lang", "", "output language (ca, es, en)")
	flags.StringVar(&opts.url, "url", "", "core base URL")
	flags.StringVar(&opts.apiKey, "api-key", "", "API key (discouraged: it appears in the process list)")
	flags.StringVar(&opts.caFile, "ca-file", "", "extra PEM CA bundle")
	flags.BoolVar(&opts.json, "json", false, "print raw JSON")
	flags.BoolVar(&opts.wait, "wait", false, "wait for the job")
	timeout := &durationValue{value: 10 * time.Minute}
	flags.Var(timeout, "timeout", "job wait timeout")
	flags.BoolVar(&opts.help, "help", false, "show help")
	flags.BoolVar(&opts.help, "h", false, "show help")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	opts.timeout = timeout.value
	if opts.timeout < 0 {
		return options{}, errors.New("negative timeout")
	}
	opts.caFile = firstValue(opts.caFile, a.getenv("TALAIA_CA_FILE"))
	return opts, nil
}

// durationValue accepts Go durations and a bare number of seconds.
type durationValue struct {
	value time.Duration
}

func (d *durationValue) String() string { return d.value.String() }

func (d *durationValue) Set(raw string) error {
	raw = strings.TrimSpace(raw)
	if parsed, err := time.ParseDuration(raw); err == nil {
		d.value = parsed
		return nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return err
	}
	d.value = time.Duration(seconds) * time.Second
	return nil
}

// splitArgs moves global flags out of the positional command words, so flags
// may appear before or after the command without confusing the flag package.
func splitArgs(args []string) ([]string, []string) {
	valueFlags := map[string]bool{
		"lang": true, "url": true, "api-key": true, "ca-file": true, "timeout": true,
	}
	var flags, positional []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			positional = append(positional, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if equal := strings.IndexByte(name, '='); equal >= 0 {
			name = name[:equal]
		}
		if valueFlags[name] && !strings.Contains(arg, "=") && index+1 < len(args) {
			index++
			flags = append(flags, args[index])
		}
	}
	return flags, positional
}

// flagValue reads one string flag from the separated flags.
func flagValue(args []string, name string) string {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		trimmed := strings.TrimLeft(arg, "-")
		if equal := strings.IndexByte(trimmed, '='); equal >= 0 {
			if trimmed[:equal] == name {
				return trimmed[equal+1:]
			}
			continue
		}
		if trimmed == name && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

// knownLanguage reports whether the tag names one of the supported languages.
func knownLanguage(tag string) bool {
	primary := strings.ToLower(strings.TrimSpace(tag))
	if index := strings.IndexAny(primary, "_.-@"); index >= 0 {
		primary = primary[:index]
	}
	return slices.Contains(i18n.Supported(), primary)
}

func (a *app) printUsage(localizer *i18n.Localizer) {
	lines := []string{
		localizer.T("usage.line", map[string]any{"App": localizer.T("app.name", nil)}),
		"",
		localizer.T("usage.commands", nil),
		localizer.T("usage.login", nil),
		localizer.T("usage.serversList", nil),
		localizer.T("usage.containersList", nil),
		localizer.T("usage.containersAction", nil),
		localizer.T("usage.stacksList", nil),
		localizer.T("usage.stacksAction", nil),
		localizer.T("usage.jobsWait", nil),
		localizer.T("usage.version", nil),
		localizer.T("usage.help", nil),
		"",
		localizer.T("usage.flags", nil),
		localizer.T("usage.apiKeyWarning", nil),
	}
	for _, line := range lines {
		fmt.Fprintln(a.stdout, line)
	}
}

func (a *app) usageError(localizer *i18n.Localizer) int {
	return a.usageErrorWith(localizer, localizer.T("error.usage", nil))
}

func (a *app) usageErrorWith(localizer *i18n.Localizer, message string) int {
	fmt.Fprintln(a.stderr, localizer.T("app.name", nil)+": "+message)
	a.printUsage(localizer)
	return 2
}

// fail prints a translated error and returns the normal error exit code.
func (a *app) fail(localizer *i18n.Localizer, err error) int {
	fmt.Fprintln(a.stderr, localizer.T("app.name", nil)+": "+a.errorText(localizer, err))
	return 1
}

// errorText maps typed failures to translated messages. It never formats the
// API key or a raw response body.
func (a *app) errorText(localizer *i18n.Localizer, err error) string {
	var (
		apiErr     *apiError
		networkErr *networkError
		invalidErr *invalidResponseError
		caErr      *caFileError
		permission *configPermissionError
		accessErr  *configAccessError
		writeErr   *configWriteError
		notFound   *serverNotFoundError
		ambiguous  *serverAmbiguousError
	)
	switch {
	case errors.As(err, &permission):
		return localizer.T("error.configPermissions", map[string]any{"Path": permission.path})
	case errors.As(err, &accessErr):
		return localizer.T("error.configRead", map[string]any{"Path": accessErr.path})
	case errors.As(err, &writeErr):
		return localizer.T("error.configWrite", map[string]any{"Path": writeErr.path})
	case errors.As(err, &caErr):
		return localizer.T("error.caFile", nil)
	case errors.As(err, &notFound):
		return localizer.T("error.serverNotFound", map[string]any{"Server": notFound.reference})
	case errors.As(err, &ambiguous):
		return localizer.T("error.serverAmbiguous", map[string]any{
			"Server": ambiguous.reference, "Count": ambiguous.count,
		})
	case errors.As(err, &networkErr):
		return localizer.T("error.network", nil)
	case errors.As(err, &invalidErr):
		return localizer.T("error.invalidResponse", nil)
	case errors.As(err, &apiErr):
		return apiErrorMessage(localizer, apiErr)
	case errors.Is(err, context.DeadlineExceeded):
		return localizer.T("error.timeout", nil)
	case errors.Is(err, errInvalidURL):
		return localizer.T("error.invalidURL", nil)
	case errors.Is(err, errMissingURL):
		return localizer.T("error.missingURL", nil)
	case errors.Is(err, errMissingAPIKey):
		return localizer.T("error.missingAPIKey", nil)
	default:
		return localizer.T("error.generic", nil)
	}
}

// apiErrorMessage maps the stable core error codes used by talaiactl.
func apiErrorMessage(localizer *i18n.Localizer, err *apiError) string {
	switch err.code {
	case "forbidden":
		return localizer.T("error.api.forbidden", nil)
	case "not_found":
		return localizer.T("error.api.notFound", nil)
	case "invalid_api_key":
		return localizer.T("error.api.invalidAPIKey", nil)
	case "rate_limited":
		return localizer.T("error.api.rateLimited", nil)
	case "session_required":
		return localizer.T("error.api.sessionRequired", nil)
	case "mfa_required":
		return localizer.T("error.api.mfaRequired", nil)
	case "agent_offline":
		return localizer.T("error.api.agentOffline", nil)
	case "confirmation_required":
		return localizer.T("error.api.confirmationRequired", nil)
	case "invalid_request":
		return localizer.T("error.api.invalidRequest", nil)
	default:
		return localizer.T("error.api.generic", map[string]any{"Code": err.code})
	}
}

// configFile resolves the per-user configuration path once per run.
func (a *app) configFile() (string, error) {
	if a.configPath != "" {
		return a.configPath, nil
	}
	return defaultConfigPath()
}

// readConfig loads the file and adds the path to failures.
func (a *app) readConfig(localizer *i18n.Localizer, path string) (config, int) {
	cfg, err := loadConfig(path)
	if err != nil {
		var permission *configPermissionError
		if errors.As(err, &permission) {
			return config{}, a.fail(localizer, permission)
		}
		return config{}, a.fail(localizer, &configAccessError{path: path, err: err})
	}
	return cfg, -1
}

// configAccessError is a failed config read.
type configAccessError struct {
	path string
	err  error
}

func (e *configAccessError) Error() string { return e.err.Error() }

// configWriteError is a failed config write.
type configWriteError struct {
	path string
	err  error
}

func (e *configWriteError) Error() string { return e.err.Error() }

var (
	errMissingURL    = errors.New("missing URL")
	errMissingAPIKey = errors.New("missing API key")
	errInvalidURL    = errors.New("invalid URL")
)

// prepare resolves the runtime settings and builds the API client. The int is
// the already-printed exit code, or -1 to continue.
func (a *app) prepare(localizer *i18n.Localizer, opts options) (*apiClient, int) {
	path, err := a.configFile()
	if err != nil {
		return nil, a.fail(localizer, err)
	}
	cfg, code := a.readConfig(localizer, path)
	if code >= 0 {
		return nil, code
	}
	rawURL := firstValue(opts.url, a.getenv("TALAIA_URL"), cfg.URL)
	key := firstValue(opts.apiKey, a.getenv("TALAIA_API_KEY"), cfg.APIKey)
	if rawURL == "" {
		return nil, a.fail(localizer, errMissingURL)
	}
	if key == "" {
		return nil, a.fail(localizer, errMissingAPIKey)
	}
	client, err := newAPIClient(rawURL, key, opts.caFile, a.version)
	if err != nil {
		return nil, a.fail(localizer, err)
	}
	return client, -1
}
