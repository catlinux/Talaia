package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Rate limit of the enrollment endpoint: 10 requests per minute and client IP.
const (
	rateLimitRequests = 10
	rateLimitWindow   = time.Minute
)

// RateLimiter is a small in-memory limiter: at most limit requests per client
// and window.
type RateLimiter struct {
	mu          sync.Mutex
	clients     map[string]*clientWindow
	limit       int
	window      time.Duration
	now         func() time.Time
	lastCleanup time.Time
}

type clientWindow struct {
	count   int
	resetAt time.Time
}

// NewRateLimiter returns the limiter used by the enrollment endpoint.
func NewRateLimiter() *RateLimiter {
	return NewRateLimiterWithClock(rateLimitRequests, rateLimitWindow, time.Now)
}

// NewRateLimiterWithClock returns a limiter with the given limit, window and
// clock. Tests use it.
func NewRateLimiterWithClock(limit int, window time.Duration, now func() time.Time) *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*clientWindow),
		limit:   limit,
		window:  window,
		now:     now,
	}
}

// Allow reports whether the client can make one more request, counting it when
// it can.
func (l *RateLimiter) Allow(client string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.cleanup(now)

	current, ok := l.clients[client]
	if !ok || !now.Before(current.resetAt) {
		l.clients[client] = &clientWindow{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if current.count >= l.limit {
		return false
	}

	current.count++
	return true
}

// cleanup drops the windows that have expired. It runs at most once per window,
// so the map does not keep one entry per client forever.
func (l *RateLimiter) cleanup(now time.Time) {
	if !l.lastCleanup.IsZero() && now.Sub(l.lastCleanup) < l.window {
		return
	}
	l.lastCleanup = now

	for client, window := range l.clients {
		if !now.Before(window.resetAt) {
			delete(l.clients, client)
		}
	}
}

// clientIP returns the IP of the connection. Only RemoteAddr is used: proxy
// headers such as X-Forwarded-For are not trusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
