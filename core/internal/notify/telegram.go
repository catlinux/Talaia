package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Telegram struct {
	Token, ChatID string
	Client        *http.Client
	BaseURL       string
}

func (s Telegram) Send(ctx context.Context, m Message) error {
	u := s.BaseURL
	if u == "" {
		u = "https://api.telegram.org"
	}
	b, _ := json.Marshal(map[string]string{"chat_id": s.ChatID, "text": m.Body})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/bot%s/sendMessage", u, s.Token), bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	c := s.Client
	if c == nil {
		c = client(nil)
	}
	resp, err := c.Do(req)
	if err != nil {
		return safeHTTPError(err, 0)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return safeHTTPError(nil, resp.StatusCode)
	}
	return nil
}
