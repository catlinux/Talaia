package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/catlinux/Talaia/core/internal/netguard"
	"net/http"
	"strconv"
	"time"
)

type Webhook struct {
	URL, HMACSecret string
	Client          *http.Client
	Clock           func() time.Time
}

func (s Webhook) Send(ctx context.Context, m Message) error {
	if _, e := netguard.ValidateURL(s.URL); e != nil {
		return fmt.Errorf("invalid webhook URL")
	}
	now := time.Now()
	if s.Clock != nil {
		now = s.Clock()
	}
	payload := struct {
		Version int       `json:"version"`
		Event   string    `json:"event"`
		Alert   Message   `json:"alert"`
		SentAt  time.Time `json:"sent_at"`
	}{1, m.Event, m, now}
	b, e := json.Marshal(payload)
	if e != nil {
		return fmt.Errorf("encode webhook")
	}
	stamp := strconv.FormatInt(now.Unix(), 10)
	req, e := http.NewRequestWithContext(ctx, "POST", s.URL, bytes.NewReader(b))
	if e != nil {
		return fmt.Errorf("request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Talaia-Timestamp", stamp)
	if s.HMACSecret != "" {
		mac := hmac.New(sha256.New, []byte(s.HMACSecret))
		mac.Write([]byte(stamp + "."))
		mac.Write(b)
		req.Header.Set("X-Talaia-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	c := s.Client
	if c == nil {
		c = client(nil)
	}
	resp, e := c.Do(req)
	if e != nil {
		return safeHTTPError(e, 0)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return safeHTTPError(nil, resp.StatusCode)
	}
	return nil
}
