package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/catlinux/Talaia/core/internal/netguard"
	"net/http"
	"strings"
)

type Discord struct {
	URL    string
	Client *http.Client
}

func validDiscordURL(raw string) bool {
	u, e := netguard.ValidateURL(raw)
	return e == nil && u.Scheme == "https" && (u.Hostname() == "discord.com" || u.Hostname() == "discordapp.com") && strings.HasPrefix(u.EscapedPath(), "/api/webhooks/")
}
func (s Discord) Send(ctx context.Context, m Message) error {
	if !validDiscordURL(s.URL) {
		return fmt.Errorf("invalid discord URL")
	}
	b, _ := json.Marshal(map[string]string{"content": m.Body})
	req, e := http.NewRequestWithContext(ctx, "POST", s.URL, bytes.NewReader(b))
	if e != nil {
		return fmt.Errorf("request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	c := s.Client
	if c == nil {
		c = client(nil)
	}
	resp, e := c.Do(req)
	if e != nil {
		return safeHTTPError(e, 0)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return safeHTTPError(nil, resp.StatusCode)
	}
	return nil
}
