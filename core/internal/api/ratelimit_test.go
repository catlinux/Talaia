package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsTheLimitAndResets(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	limiter := NewRateLimiterWithClock(3, time.Minute, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if !limiter.Allow("192.0.2.10") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if limiter.Allow("192.0.2.10") {
		t.Fatal("the fourth request must be limited")
	}

	// Another client has its own window.
	if !limiter.Allow("192.0.2.11") {
		t.Fatal("another client must not be affected")
	}

	// The window resets.
	now = now.Add(time.Minute)
	if !limiter.Allow("192.0.2.10") {
		t.Fatal("the request must be allowed after the window")
	}
}

func TestRateLimiterDropsExpiredWindows(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	limiter := NewRateLimiterWithClock(10, time.Minute, func() time.Time { return now })

	for _, client := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"} {
		if !limiter.Allow(client) {
			t.Fatalf("client %s should be allowed", client)
		}
	}

	now = now.Add(2 * time.Minute)
	if !limiter.Allow("192.0.2.4") {
		t.Fatal("the request should be allowed")
	}

	if len(limiter.clients) != 1 {
		t.Fatalf("windows = %d, want the expired ones to be dropped", len(limiter.clients))
	}
}

func TestClientIPTakesRemoteAddrOnly(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		want       string
	}{
		{name: "host and port", remoteAddr: "192.0.2.10:4567", want: "192.0.2.10"},
		{name: "without port", remoteAddr: "192.0.2.10", want: "192.0.2.10"},
		{name: "forwarded header is ignored", remoteAddr: "192.0.2.10:4567", forwarded: "203.0.113.7", want: "192.0.2.10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/enroll", nil)
			request.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				request.Header.Set("X-Forwarded-For", tt.forwarded)
			}

			if got := clientIP(request); got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}
