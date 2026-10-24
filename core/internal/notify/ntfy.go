package notify

import (
	"context"
	"fmt"
	"github.com/catlinux/Talaia/core/internal/netguard"
	"net/http"
	"net/url"
	"strings"
)

type Ntfy struct {
	Server, Topic, Token string
	Priority             int
	Client               *http.Client
}

func (s Ntfy) Send(ctx context.Context, m Message) error {
	server := s.Server
	if server == "" {
		server = "https://ntfy.sh"
	}
	u, e := netguard.ValidateURL(strings.TrimRight(server, "/") + "/" + url.PathEscape(s.Topic))
	if e != nil {
		return fmt.Errorf("invalid server URL")
	}
	priority := s.Priority
	if priority < 1 || priority > 5 {
		priority = 3
		if m.Severity == "critical" {
			priority = 4
		}
	}
	req, e := http.NewRequestWithContext(ctx, "POST", u.String(), strings.NewReader(m.Body))
	if e != nil {
		return fmt.Errorf("request invalid")
	}
	req.Header.Set("Title", m.Title)
	req.Header.Set("Priority", fmt.Sprint(priority))
	req.Header.Set("Tags", m.Severity)
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
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
