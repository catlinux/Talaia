// Package config loads the talaia-agent configuration from a TOML file and
// environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	// DefaultPath is the configuration file used when TALAIA_AGENT_CONFIG is
	// not set.
	DefaultPath = "/etc/talaia/agent.toml"
	// DefaultCoreURL is the core address used in insecure development mode.
	DefaultCoreURL = "http://127.0.0.1:8080"
	// DefaultLogLevel is the log level used by default.
	DefaultLogLevel = "info"
	// DefaultCertDir holds the identity of the agent.
	DefaultCertDir = "/etc/talaia/certs"
	// DefaultEnrollTokenFile holds the one-time enrollment token.
	DefaultEnrollTokenFile = "/etc/talaia/enroll.token"
)

// Config is the runtime configuration of talaia-agent.
type Config struct {
	CoreURL         string
	LogLevel        string
	CAFingerprint   string
	InsecureDev     bool
	CertDir         string
	EnrollTokenFile string
	// HostTerminal enables root shells on the server itself.
	HostTerminal bool
}

// fileConfig mirrors the TOML document. Pointer fields tell an absent key apart
// from an empty value, so defaults are only replaced when the key is set.
type fileConfig struct {
	CoreURL         *string `toml:"core_url"`
	LogLevel        *string `toml:"log_level"`
	CAFingerprint   *string `toml:"ca_fingerprint"`
	InsecureDev     *bool   `toml:"insecure_dev"`
	CertDir         *string `toml:"cert_dir"`
	EnrollTokenFile *string `toml:"enroll_token_file"`
	Terminal        struct {
		Host *bool `toml:"host"`
	} `toml:"terminal"`
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

// Level returns the parsed log level. It is only meaningful after a successful
// Load.
func (c Config) Level() slog.Level {
	return logLevels[c.LogLevel]
}

// load is Load with the environment lookup and the default path injected so
// tests do not depend on the host configuration.
func load(lookup func(string) (string, bool), defaultPath string) (Config, error) {
	cfg := Config{
		CoreURL:         DefaultCoreURL,
		LogLevel:        DefaultLogLevel,
		CertDir:         DefaultCertDir,
		EnrollTokenFile: DefaultEnrollTokenFile,
		HostTerminal:    true,
	}
	coreURLSet := false

	path := defaultPath
	explicit := false
	if value, ok := lookup("TALAIA_AGENT_CONFIG"); ok && strings.TrimSpace(value) != "" {
		path = value
		explicit = true
	}

	if err := applyFile(&cfg, path, explicit, &coreURLSet); err != nil {
		return Config{}, err
	}

	if value, ok := lookup("TALAIA_CORE_URL"); ok {
		cfg.CoreURL = value
		coreURLSet = true
	}
	if value, ok := lookup("TALAIA_LOG_LEVEL"); ok {
		cfg.LogLevel = value
	}
	if value, ok := lookup("TALAIA_CA_FINGERPRINT"); ok {
		cfg.CAFingerprint = value
	}
	if value, ok := lookup("TALAIA_AGENT_INSECURE_DEV"); ok {
		insecure, err := parseInsecureDev(value)
		if err != nil {
			return Config{}, err
		}
		cfg.InsecureDev = insecure
	}
	if value, ok := lookup("TALAIA_AGENT_CERT_DIR"); ok {
		cfg.CertDir = value
	}
	if value, ok := lookup("TALAIA_AGENT_ENROLL_TOKEN_FILE"); ok {
		cfg.EnrollTokenFile = value
	}
	if value, ok := lookup("TALAIA_AGENT_TERMINAL_HOST"); ok {
		host, err := parseBool("terminal host", value)
		if err != nil {
			return Config{}, err
		}
		cfg.HostTerminal = host
	}

	if err := cfg.validate(coreURLSet); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyFile(cfg *Config, path string, explicit bool, coreURLSet *bool) error {
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
	if file.CoreURL != nil {
		cfg.CoreURL = *file.CoreURL
		*coreURLSet = true
	}
	if file.LogLevel != nil {
		cfg.LogLevel = *file.LogLevel
	}
	if file.CAFingerprint != nil {
		cfg.CAFingerprint = *file.CAFingerprint
	}
	if file.InsecureDev != nil {
		cfg.InsecureDev = *file.InsecureDev
	}
	if file.CertDir != nil {
		cfg.CertDir = *file.CertDir
	}
	if file.EnrollTokenFile != nil {
		cfg.EnrollTokenFile = *file.EnrollTokenFile
	}
	if file.Terminal.Host != nil {
		cfg.HostTerminal = *file.Terminal.Host
	}
	return nil
}

// parseInsecureDev accepts 1/true and 0/false.
func parseInsecureDev(value string) (bool, error) {
	return parseBool("insecure_dev", value)
}

func parseBool(name, value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s value %q (expected 1 or true)", name, value)
	}
}

func (c *Config) validate(coreURLSet bool) error {
	// The loopback default is only a convenience for insecure development mode.
	if !coreURLSet && !c.InsecureDev {
		return errors.New("core_url is required (set it in the config file or TALAIA_CORE_URL)")
	}
	if strings.TrimSpace(c.CoreURL) == "" {
		return errors.New("core_url is required (set it in the config file or TALAIA_CORE_URL)")
	}

	parsed, err := url.Parse(c.CoreURL)
	if err != nil {
		return fmt.Errorf("core_url is not a valid URL: %w", err)
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("core_url must be an absolute http or https URL, got %q", c.CoreURL)
	}
	if !c.InsecureDev && parsed.Scheme != "https" {
		return fmt.Errorf("core_url must be https:// unless insecure_dev is true, got %q", c.CoreURL)
	}

	fingerprint := strings.ToLower(strings.TrimSpace(c.CAFingerprint))
	if fingerprint != "" && (len(fingerprint) != 64 || !isHex(fingerprint)) {
		return fmt.Errorf("ca_fingerprint must be 64 hexadecimal characters, got %q", c.CAFingerprint)
	}
	c.CAFingerprint = fingerprint

	if strings.TrimSpace(c.CertDir) == "" {
		return errors.New("cert_dir is required")
	}
	if strings.TrimSpace(c.EnrollTokenFile) == "" {
		return errors.New("enroll_token_file is required")
	}

	level := strings.ToLower(strings.TrimSpace(c.LogLevel))
	if _, ok := logLevels[level]; !ok {
		return fmt.Errorf("invalid log_level %q (expected debug, info, warn or error)", c.LogLevel)
	}
	c.LogLevel = level
	return nil
}

func isHex(value string) bool {
	for _, char := range value {
		switch {
		case char >= '0' && char <= '9':
		case char >= 'a' && char <= 'f':
		default:
			return false
		}
	}
	return true
}
