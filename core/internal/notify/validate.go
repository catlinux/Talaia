package notify

import (
	"encoding/json"
	"fmt"
	"github.com/catlinux/Talaia/core/internal/netguard"
	"net/mail"
	"regexp"
	"strings"
)

var telegramToken = regexp.MustCompile(`^[0-9]{1,20}:[A-Za-z0-9_-]{20,64}$`)

func ParseSecret(typ string, b []byte) (any, error) {
	var v map[string]string
	if e := json.Unmarshal(b, &v); e != nil {
		return nil, fmt.Errorf("invalid secret")
	}
	required := map[string][]string{"telegram": {"token"}, "ntfy": {"topic"}, "email": {"username", "password"}, "discord": {"url"}, "webhook": {"url"}}
	fields, ok := required[typ]
	if !ok {
		return nil, fmt.Errorf("unsupported channel type")
	}
	for _, f := range fields {
		if strings.TrimSpace(v[f]) == "" {
			return nil, fmt.Errorf("secret field required")
		}
	}
	// The bot token goes into the request path of api.telegram.org: only the
	// documented token shape is accepted so it cannot alter that path.
	if typ == "telegram" && !telegramToken.MatchString(v["token"]) {
		return nil, fmt.Errorf("invalid telegram token")
	}
	return v, nil
}
func ValidateChannel(typ string, config, secret json.RawMessage) error {
	var c map[string]any
	if e := json.Unmarshal(config, &c); e != nil {
		return fmt.Errorf("invalid config")
	}
	if _, e := ParseSecret(typ, secret); e != nil {
		return e
	}
	str := func(k string) string { v, _ := c[k].(string); return v }
	switch typ {
	case "telegram":
		if str("chat_id") == "" {
			return fmt.Errorf("chat_id required")
		}
	case "ntfy":
		if str("server") != "" {
			if _, e := netguard.ValidateURL(str("server")); e != nil {
				return fmt.Errorf("invalid server URL")
			}
		}
		if n, ok := c["priority"].(float64); ok && (n < 1 || n > 5) {
			return fmt.Errorf("invalid priority")
		}
	case "email":
		host := str("host")
		port, ok := c["port"].(float64)
		if host == "" || !ok || port < 1 || port > 65535 {
			return fmt.Errorf("invalid email server")
		}
		if str("tls_mode") != "tls" && str("tls_mode") != "starttls" {
			return fmt.Errorf("invalid tls mode")
		}
		from, e := mail.ParseAddress(str("from"))
		if e != nil || from.Address == "" {
			return fmt.Errorf("invalid from address")
		}
		tos, ok := c["to"].([]any)
		if !ok || len(tos) == 0 {
			return fmt.Errorf("recipient required")
		}
		for _, v := range tos {
			a, e := mail.ParseAddress(fmt.Sprint(v))
			if e != nil || a.Address == "" {
				return fmt.Errorf("invalid recipient")
			}
		}
	case "discord", "webhook":
		// The URL of these channels is a secret (it authorises posting), so it
		// lives in the sealed secret, never in the public config.
		var sec map[string]string
		_ = json.Unmarshal(secret, &sec)
		raw := sec["url"]
		if typ == "discord" && !validDiscordURL(raw) {
			return fmt.Errorf("invalid discord URL")
		}
		if typ == "webhook" {
			if _, e := netguard.ValidateURL(raw); e != nil {
				return fmt.Errorf("invalid webhook URL")
			}
		}
	default:
		return fmt.Errorf("unsupported channel type")
	}
	return nil
}
