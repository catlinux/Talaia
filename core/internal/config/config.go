// Package config loads the talaia-core configuration from a TOML file and
// environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	// DefaultPath is the configuration file used when TALAIA_CONFIG is not set.
	DefaultPath = "/etc/talaia/core.toml"
	// DefaultListen is the address the HTTP server listens on by default.
	DefaultListen = ":8080"
	// DefaultLogLevel is the log level used by default.
	DefaultLogLevel = "info"
	// DefaultDataDir holds the CA, the certificates and the state of the core.
	DefaultDataDir = "/var/lib/talaia"
	// DefaultWebDir holds the compiled web panel.
	DefaultWebDir = "/usr/share/talaia/web"
)

// Config is the runtime configuration of talaia-core.
type Config struct {
	Listen      string
	DatabaseURL string
	LogLevel    string
	DataDir     string
	// WebDir is the directory of the compiled panel.
	WebDir string
	// DistDir holds install.sh and the agent packages the core serves to new
	// servers. Without it the core answers 404 to those downloads.
	DistDir string
	// PublicPort is the port browsers and agents use to reach the core, when it
	// differs from the listen port (a container that publishes another one).
	PublicPort string
	// PublicHostnames are the names and addresses the server certificate of the
	// core must be valid for.
	PublicHostnames []string
	// AllowedOrigins are the browser origins the panel may be served from, as
	// scheme://host[:port]. They are derived from PublicHostnames when unset.
	AllowedOrigins []string
	// InsecureDev disables TLS. It must never be used in production.
	InsecureDev bool
}

// fileConfig mirrors the TOML document. Pointer fields tell an absent key
// apart from an empty value, so defaults are only replaced when the key is set.
type fileConfig struct {
	Listen          *string   `toml:"listen"`
	DatabaseURL     *string   `toml:"database_url"`
	LogLevel        *string   `toml:"log_level"`
	DataDir         *string   `toml:"data_dir"`
	WebDir          *string   `toml:"web_dir"`
	DistDir         *string   `toml:"dist_dir"`
	PublicPort      *string   `toml:"public_port"`
	PublicHostnames *[]string `toml:"public_hostnames"`
	AllowedOrigins  *[]string `toml:"allowed_origins"`
	InsecureDev     *bool     `toml:"insecure_dev"`
}

var logLevels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

// Load reads the configuration applying the precedence defaults < file <
// environment.
func Load() (Config, error) {
	return load(os.LookupEnv, DefaultPath)
}

// Level returns the parsed log level. It is only meaningful after a
// successful Load.
func (c Config) Level() slog.Level {
	return logLevels[c.LogLevel]
}

// load is Load with the environment lookup and the default path injected so
// tests do not depend on the host configuration.
func load(lookup func(string) (string, bool), defaultPath string) (Config, error) {
	cfg := Config{
		Listen:   DefaultListen,
		LogLevel: DefaultLogLevel,
		DataDir:  DefaultDataDir,
		WebDir:   DefaultWebDir,
	}

	path := defaultPath
	explicit := false
	if value, ok := lookup("TALAIA_CONFIG"); ok && strings.TrimSpace(value) != "" {
		path = value
		explicit = true
	}

	if err := applyFile(&cfg, path, explicit); err != nil {
		return Config{}, err
	}

	if value, ok := lookup("TALAIA_LISTEN"); ok {
		cfg.Listen = value
	}
	if value, ok := lookup("TALAIA_DATABASE_URL"); ok {
		cfg.DatabaseURL = value
	}
	if value, ok := lookup("TALAIA_LOG_LEVEL"); ok {
		cfg.LogLevel = value
	}
	if value, ok := lookup("TALAIA_DATA_DIR"); ok {
		cfg.DataDir = value
	}
	if value, ok := lookup("TALAIA_WEB_DIR"); ok {
		cfg.WebDir = value
	}
	if value, ok := lookup("TALAIA_DIST_DIR"); ok {
		cfg.DistDir = value
	}
	if value, ok := lookup("TALAIA_PUBLIC_PORT"); ok {
		cfg.PublicPort = strings.TrimSpace(value)
	}
	if value, ok := lookup("TALAIA_PUBLIC_HOSTNAMES"); ok {
		cfg.PublicHostnames = splitList(value)
	}
	if value, ok := lookup("TALAIA_ALLOWED_ORIGINS"); ok {
		cfg.AllowedOrigins = splitList(value)
	}
	if value, ok := lookup("TALAIA_INSECURE_DEV"); ok {
		insecure, err := parseInsecureDev(value)
		if err != nil {
			return Config{}, err
		}
		cfg.InsecureDev = insecure
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ValidateCertificateHosts checks what the TLS server certificate of the core
// needs. It is not part of Load on purpose: until task E2 adds TLS, the serve
// path and the enrollment commands must keep working without public_hostnames,
// and E2 calls this before it starts serving TLS.
func (c Config) ValidateCertificateHosts() error {
	if c.InsecureDev {
		return nil
	}
	if len(c.PublicHostnames) == 0 {
		return errors.New("public_hostnames is required unless insecure_dev is true")
	}
	return nil
}

func applyFile(cfg *Config, path string, explicit bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if explicit {
				return fmt.Errorf("config file %s does not exist", path)
			}
			return nil
		}
		return fmt.Errorf("read config file %s: %w", path, err)
	}

	var file fileConfig
	if err := toml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse config file %s: %w", path, err)
	}
	if file.Listen != nil {
		cfg.Listen = *file.Listen
	}
	if file.DatabaseURL != nil {
		cfg.DatabaseURL = *file.DatabaseURL
	}
	if file.LogLevel != nil {
		cfg.LogLevel = *file.LogLevel
	}
	if file.DataDir != nil {
		cfg.DataDir = *file.DataDir
	}
	if file.WebDir != nil {
		cfg.WebDir = *file.WebDir
	}
	if file.DistDir != nil {
		cfg.DistDir = *file.DistDir
	}
	if file.PublicPort != nil {
		cfg.PublicPort = strings.TrimSpace(*file.PublicPort)
	}
	if file.PublicHostnames != nil {
		cfg.PublicHostnames = *file.PublicHostnames
	}
	if file.AllowedOrigins != nil {
		cfg.AllowedOrigins = *file.AllowedOrigins
	}
	if file.InsecureDev != nil {
		cfg.InsecureDev = *file.InsecureDev
	}
	return nil
}

// splitList splits a comma separated environment value.
func splitList(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// parseInsecureDev accepts 1/true and 0/false.
func parseInsecureDev(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid insecure_dev value %q (expected 1 or true)", value)
	}
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return errors.New("database_url is required (set it in the config file or TALAIA_DATABASE_URL)")
	}
	if strings.TrimSpace(c.Listen) == "" {
		return errors.New("listen address is required")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return errors.New("data_dir is required")
	}
	if strings.TrimSpace(c.WebDir) == "" {
		return errors.New("web_dir is required")
	}

	// The allowed origins default to the panel's own address; a deployment
	// behind a proxy can override them.
	origins := c.AllowedOrigins
	if len(origins) == 0 {
		origins = defaultOrigins(c.PublicHostnames, c.publicPort(), c.InsecureDev)
	}
	normalized := make([]string, 0, len(origins))
	seen := make(map[string]bool, len(origins))
	for _, origin := range origins {
		value, err := normalizeOrigin(origin)
		if err != nil {
			return err
		}
		// Two spellings of the same origin (a trailing slash, the default port)
		// collapse into one.
		if seen[value] {
			continue
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		normalized = nil
	}
	c.AllowedOrigins = normalized
	// Without TLS anyone who reaches the port can impersonate an agent, so the
	// development mode only listens on the loopback interface.
	if c.InsecureDev && !isLoopbackListen(c.Listen) {
		return fmt.Errorf("insecure_dev requires a loopback listen address such as 127.0.0.1:8080, got %q", c.Listen)
	}

	level := strings.ToLower(strings.TrimSpace(c.LogLevel))
	if _, ok := logLevels[level]; !ok {
		return fmt.Errorf("invalid log_level %q (expected debug, info, warn or error)", c.LogLevel)
	}
	c.LogLevel = level
	return nil
}

// isLoopbackListen reports whether a listen address only binds the loopback
// interface. An empty host (":8080") binds every interface.
func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// defaultOrigins derives the allowed browser origins from the public hostnames
// and the port the core is reached at.
func defaultOrigins(hostnames []string, port string, insecure bool) []string {
	origins := make([]string, 0, len(hostnames)+2)

	for _, host := range hostnames {
		origins = append(origins, (&url.URL{Scheme: "https", Host: host}).String())
		if port != "" && port != "443" {
			origins = append(origins, (&url.URL{Scheme: "https", Host: net.JoinHostPort(host, port)}).String())
		}
	}
	if insecure {
		// The Vite development server serves the panel and proxies the API.
		origins = append(origins, "http://localhost:5173", "http://127.0.0.1:5173")
	}
	return origins
}

// publicPort returns the port the core is reached at: the configured public
// port, or the one it listens on.
func (c Config) publicPort() string {
	if c.PublicPort != "" {
		return c.PublicPort
	}
	return listenPort(c.Listen)
}

// PublicURL returns the https address agents and browsers reach the core at,
// built from the first public hostname, or "" when there is none.
func (c Config) PublicURL() string {
	if len(c.PublicHostnames) == 0 {
		return ""
	}
	host := c.PublicHostnames[0]
	if port := c.publicPort(); port != "" && port != "443" {
		host = net.JoinHostPort(host, port)
	}
	return (&url.URL{Scheme: "https", Host: host}).String()
}

// listenPort returns the port of a listen address, or "" when it has none.
func listenPort(listen string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return ""
	}
	return port
}

// normalizeOrigin checks that an allowed origin is scheme://host[:port] and
// returns the lower-case form browsers use, without the default port.
func normalizeOrigin(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid allowed origin %q: %w", trimmed, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("allowed origin %q must start with http:// or https://", trimmed)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("allowed origin %q has no host", trimmed)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("allowed origin %q must be scheme://host[:port] without a path", trimmed)
	}

	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Host)
	defaultPort := map[string]string{"http": ":80", "https": ":443"}[scheme]
	if defaultPort != "" {
		host = strings.TrimSuffix(host, defaultPort)
	}
	return scheme + "://" + host, nil
}
