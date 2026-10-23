package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// config is the persistent part of the client settings.
type config struct {
	URL    string
	APIKey string
}

// configPermissionError reports a key file that other users could read.
type configPermissionError struct {
	path string
}

func (e *configPermissionError) Error() string {
	return "config file is readable by group or others: " + e.path
}

// defaultConfigPath returns the per-user configuration path of talaiactl.
func defaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "talaia", "config.toml"), nil
}

// loadConfig reads url and api_key from path. A missing file is an empty config.
func loadConfig(path string) (config, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return config{}, nil
	}
	if err != nil {
		return config{}, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o044 != 0 {
		return config{}, &configPermissionError{path: path}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}
	return parseConfig(data)
}

// parseConfig implements the two string keys talaiactl owns. Unknown keys and
// comments are ignored so a hand-edited file does not break the client.
func parseConfig(data []byte) (config, error) {
	var result config
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		index := strings.IndexByte(line, '=')
		if index < 0 {
			continue
		}
		key := strings.TrimSpace(line[:index])
		value, err := parseTOMLString(strings.TrimSpace(line[index+1:]))
		if err != nil {
			return config{}, fmt.Errorf("parse %s: %w", key, err)
		}
		switch key {
		case "url":
			result.URL = value
		case "api_key":
			result.APIKey = value
		}
	}
	if err := scanner.Err(); err != nil {
		return config{}, err
	}
	return result, nil
}

// parseTOMLString accepts a basic or literal TOML string, or an unquoted value
// for hand-written files. Everything after a complete value is a comment.
func parseTOMLString(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	switch value[0] {
	case '"':
		end := 1
		for end < len(value) {
			if value[end] == '\\' {
				end += 2
				continue
			}
			if value[end] == '"' {
				return strconv.Unquote(value[:end+1])
			}
			end++
		}
		return "", errors.New("unterminated basic string")
	case '\'':
		end := strings.IndexByte(value[1:], '\'')
		if end < 0 {
			return "", errors.New("unterminated literal string")
		}
		return value[1 : end+1], nil
	default:
		if index := strings.IndexByte(value, '#'); index >= 0 {
			value = value[:index]
		}
		return strings.TrimSpace(value), nil
	}
}

// saveConfig writes both keys with owner-only permissions.
func saveConfig(path string, cfg config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data := []byte(fmt.Sprintf("url = %s\napi_key = %s\n", strconv.Quote(cfg.URL), strconv.Quote(cfg.APIKey)))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// firstValue returns the first non-empty value.
func firstValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
