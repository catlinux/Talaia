package checks

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/catlinux/Talaia/core/internal/netguard"
)

type config struct {
	Method         string   `json:"method"`
	ExpectedCodes  []string `json:"expected_codes"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

func parseConfig(raw json.RawMessage) (config, error) {
	c := config{Method: "GET", ExpectedCodes: []string{"200-399"}, TimeoutSeconds: 10}
	if len(raw) == 0 {
		return c, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return c, fmt.Errorf("invalid config: %w", err)
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("invalid config: %w", err)
	}
	if c.Method == "" {
		c.Method = "GET"
	}
	if len(c.ExpectedCodes) == 0 {
		c.ExpectedCodes = []string{"200-399"}
	}
	if _, explicit := fields["timeout_seconds"]; !explicit {
		c.TimeoutSeconds = 10
	}
	return c, nil
}

func ValidateCheck(kind, target string, raw json.RawMessage) error {
	switch kind {
	case "http":
		if _, err := netguard.ValidateURL(target); err != nil {
			return fmt.Errorf("invalid target: %w", err)
		}
	case "tcp", "tls":
		host, port, err := net.SplitHostPort(target)
		if err != nil || host == "" {
			return fmt.Errorf("invalid target")
		}
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("invalid target")
		}
	default:
		return fmt.Errorf("unknown check kind")
	}
	c, err := parseConfig(raw)
	if err != nil {
		return err
	}
	if kind == "http" {
		if c.Method != "GET" && c.Method != "HEAD" {
			return fmt.Errorf("invalid method")
		}
		for _, code := range c.ExpectedCodes {
			parts := strings.Split(code, "-")
			if len(parts) > 2 {
				return fmt.Errorf("invalid expected code")
			}
			lo, e := strconv.Atoi(parts[0])
			if e != nil || lo < 100 || lo > 599 {
				return fmt.Errorf("invalid expected code")
			}
			if len(parts) == 2 {
				hi, e := strconv.Atoi(parts[1])
				if e != nil || hi < lo || hi > 599 {
					return fmt.Errorf("invalid expected code")
				}
			}
		}
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 10 {
		return fmt.Errorf("invalid timeout")
	}
	return nil
}
