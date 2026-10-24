package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookRequestAndSignature(t *testing.T) {
	secret := "fake-hmac"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		stamp := r.Header.Get("X-Talaia-Timestamp")
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(stamp + "."))
		_, _ = mac.Write(body)
		if got, want := r.Header.Get("X-Talaia-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
			t.Errorf("signature=%q want %q", got, want)
		}
		var payload struct {
			Version int    `json:"version"`
			Event   string `json:"event"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Version != 1 || payload.Event != "firing" {
			t.Errorf("payload=%s err=%v", body, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := Message{Event: "firing", Body: "alert"}
	if err := (Webhook{URL: srv.URL, HMACSecret: secret, Client: srv.Client(), Clock: func() time.Time { return time.Unix(123, 0) }}).Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

func TestNtfyHeadersAndPriority(t *testing.T) {
	for _, tc := range []struct {
		severity string
		want     string
	}{{"warning", "3"}, {"critical", "4"}} {
		t.Run(tc.severity, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/topic" || r.Header.Get("Title") != "title" || r.Header.Get("Priority") != tc.want || r.Header.Get("Tags") != tc.severity || r.Header.Get("Authorization") != "Bearer token" {
					t.Errorf("request path=%s headers=%v", r.URL.Path, r.Header)
				}
				w.WriteHeader(200)
			}))
			defer srv.Close()
			if err := (Ntfy{Server: srv.URL, Topic: "topic", Token: "token", Client: srv.Client()}).Send(context.Background(), Message{Severity: tc.severity, Title: "title", Body: "body"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTelegramRequestAndStatusErrorsAreSafe(t *testing.T) {
	token := "fake-bot-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botfake-bot-token/sendMessage" {
			t.Errorf("path=%s", r.URL.Path)
		}
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["chat_id"] != "chat" || b["text"] != "hello" {
			t.Errorf("body=%v", b)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	if err := (Telegram{Token: token, ChatID: "chat", BaseURL: srv.URL, Client: srv.Client()}).Send(context.Background(), Message{Body: "hello"}); err != nil {
		t.Fatal(err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = fmt.Fprint(w, "sensitive response")
	}))
	defer bad.Close()
	err := (Telegram{Token: token, BaseURL: bad.URL, Client: bad.Client()}).Send(context.Background(), Message{})
	if err == nil || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), bad.URL) {
		t.Fatalf("unsafe status error: %v", err)
	}
}

func TestDiscordURLValidation(t *testing.T) {
	for _, raw := range []string{"https://discord.com/api/webhooks/id/token", "https://discordapp.com/api/webhooks/id/token"} {
		if !validDiscordURL(raw) {
			t.Errorf("rejected %s", raw)
		}
	}
	for _, raw := range []string{"https://evil.example.com/api/webhooks/id/token", "http://discord.com/api/webhooks/id/token", "https://discord.com/other"} {
		if validDiscordURL(raw) {
			t.Errorf("accepted %s", raw)
		}
		if err := (Discord{URL: raw}).Send(context.Background(), Message{}); err == nil {
			t.Errorf("sender accepted %s", raw)
		}
	}
}

type redirectTransport struct {
	target string
	base   http.RoundTripper
}

func (r redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	u := *req.URL
	target, err := http.NewRequest(req.Method, r.target, nil)
	if err != nil {
		return nil, err
	}
	u.Scheme, u.Host = target.URL.Scheme, target.URL.Host
	copy.URL = &u
	return r.base.RoundTrip(copy)
}

func TestDiscordSuccessfulDelivery(t *testing.T) {
	var received bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.Method != http.MethodPost || r.URL.Path != "/api/webhooks/id/token" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request=%s %s headers=%v", r.Method, r.URL, r.Header)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["content"] != "discord body" {
			t.Errorf("payload=%v err=%v", payload, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	client := &http.Client{Transport: redirectTransport{target: srv.URL, base: srv.Client().Transport}}
	err := (Discord{URL: "https://discord.com/api/webhooks/id/token", Client: client}).Send(context.Background(), Message{Body: "discord body"})
	if err != nil {
		t.Fatal(err)
	}
	if !received {
		t.Fatal("fake Discord server received no request")
	}
}

func TestWebhookWithoutSecretOmitsSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Talaia-Signature"); got != "" {
			t.Errorf("unexpected signature header %q", got)
		}
		if r.Header.Get("X-Talaia-Timestamp") == "" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers=%v", r.Header)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	if err := (Webhook{URL: srv.URL, Client: srv.Client()}).Send(context.Background(), Message{Event: "test"}); err != nil {
		t.Fatal(err)
	}
}

func TestNtfyWithoutTokenOmitsAuthorization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("unexpected Authorization header %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "body" {
			t.Errorf("body=%q err=%v", body, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	if err := (Ntfy{Server: srv.URL, Topic: "topic", Client: srv.Client()}).Send(context.Background(), Message{Body: "body"}); err != nil {
		t.Fatal(err)
	}
}
