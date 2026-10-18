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
	// DefaultCoreURL is the core address used by default.
	DefaultCoreURL = "http://127.0.0.1:8080"
	// DefaultLogLevel is the log level used by default.
	DefaultLogLevel = "info"
)

// Config is the runtime configuration of talaia-agent.
type Config struct {
	CoreURL  string
	LogLevel string
}

// fileConfig mirrors the TOML document. Pointer fields tell an absent key apart
// from an empty value, so defaults are only replaced when the key is set.
type fileConfig struct {
	CoreURL  *string `toml:"core_url"`
	LogLevel *string `toml:"log_level"`
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
		CoreURL:  DefaultCoreURL,
		LogLevel: DefaultLogLevel,
	}

	path := defaultPath
	explicit := false
	if value, ok := lookup("TALAIA_AGENT_CONFIG"); ok && strings.TrimSpace(value) != "" {
		path = value
		explicit = true
	}

	if err := applyFile(&cfg, path, explicit); err != nil {
		return Config{}, err
	}

	if value, ok := lookup("TALAIA_CORE_URL"); ok {
		cfg.CoreURL = value
	}
	if value, ok := lookup("TALAIA_LOG_LEVEL"); ok {
		cfg.LogLevel = value
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
	if file.CoreURL != nil {
		cfg.CoreURL = *file.CoreURL
	}
	if file.LogLevel != nil {
		cfg.LogLevel = *file.LogLevel
	}
	return nil
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.CoreURL) == "" {
		return errors.New("core_url is required")
	}
	parsed, err := url.Parse(c.CoreURL)
	if err != nil {
		return fmt.Errorf("core_url is not a valid URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("core_url must be an absolute http or https URL, got %q", c.CoreURL)
	}

	level := strings.ToLower(strings.TrimSpace(c.LogLevel))
	if _, ok := logLevels[level]; !ok {
		return fmt.Errorf("invalid log_level %q (expected debug, info, warn or error)", c.LogLevel)
	}
	c.LogLevel = level
	return nil
}
