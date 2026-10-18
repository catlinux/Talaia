// Package config loads the talaia-core configuration from a TOML file and
// environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
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
)

// Config is the runtime configuration of talaia-core.
type Config struct {
	Listen      string
	DatabaseURL string
	LogLevel    string
}

// fileConfig mirrors the TOML document. Pointer fields tell an absent key
// apart from an empty value, so defaults are only replaced when the key is set.
type fileConfig struct {
	Listen      *string `toml:"listen"`
	DatabaseURL *string `toml:"database_url"`
	LogLevel    *string `toml:"log_level"`
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
	if file.Listen != nil {
		cfg.Listen = *file.Listen
	}
	if file.DatabaseURL != nil {
		cfg.DatabaseURL = *file.DatabaseURL
	}
	if file.LogLevel != nil {
		cfg.LogLevel = *file.LogLevel
	}
	return nil
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return errors.New("database_url is required (set it in the config file or TALAIA_DATABASE_URL)")
	}
	if strings.TrimSpace(c.Listen) == "" {
		return errors.New("listen address is required")
	}

	level := strings.ToLower(strings.TrimSpace(c.LogLevel))
	if _, ok := logLevels[level]; !ok {
		return fmt.Errorf("invalid log_level %q (expected debug, info, warn or error)", c.LogLevel)
	}
	c.LogLevel = level
	return nil
}
