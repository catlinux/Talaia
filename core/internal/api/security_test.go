package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	tests := []struct {
		name string
		tls  bool
	}{
		{name: "over TLS", tls: true},
		{name: "plaintext"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := Security(SecurityConfig{TLS: tt.tls})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

			header := recorder.Header()
			if got := header.Get("Content-Security-Policy"); got != contentSecurityPolicy {
				t.Errorf("Content-Security-Policy = %q, want the panel policy", got)
			}
			if got := header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := header.Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want no-referrer", got)
			}

			hsts := header.Get("Strict-Transport-Security")
			if tt.tls && hsts != "max-age=31536000" {
				t.Errorf("Strict-Transport-Security = %q, want max-age=31536000", hsts)
			}
			if !tt.tls && hsts != "" {
				t.Errorf("Strict-Transport-Security = %q, want none without TLS", hsts)
			}
		})
	}
}

func TestSecurityKeepsAPIFailuresOutOfTheCache(t *testing.T) {
	handler := Security(SecurityConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		path string
		want string
	}{
		{path: "/api/v1/auth/session", want: "no-store"},
		{path: "/", want: ""},
	}

	for _, tt := range tests {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if got := recorder.Header().Get("Cache-Control"); got != tt.want {
			t.Errorf("%s: Cache-Control = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestSecurityOriginCheck(t *testing.T) {
	handler := Security(SecurityConfig{Origins: []string{"https://panel.example.com"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		origin      string
		fetchSite   string
		want        int
	}{
		{
			name: "allowed origin", method: http.MethodPost, path: "/api/v1/auth/login",
			contentType: "application/json", origin: "https://panel.example.com", want: http.StatusOK,
		},
		{
			name: "origin with another case", method: http.MethodPost, path: "/api/v1/auth/login",
			contentType: "application/json", origin: "https://PANEL.example.com", want: http.StatusOK,
		},
		{
			name: "foreign origin", method: http.MethodPost, path: "/api/v1/auth/login",
			contentType: "application/json", origin: "https://evil.example.com", want: http.StatusForbidden,
		},
		{
			name: "no origin and no fetch metadata", method: http.MethodPost, path: "/api/v1/auth/login",
			contentType: "application/json", want: http.StatusForbidden,
		},
		{
			name: "same-origin fetch metadata", method: http.MethodPost, path: "/api/v1/auth/login",
			contentType: "application/json", fetchSite: "same-origin", want: http.StatusOK,
		},
		{
			name: "cross-site fetch metadata", method: http.MethodPost, path: "/api/v1/auth/login",
			contentType: "application/json", fetchSite: "cross-site", want: http.StatusForbidden,
		},
		{
			name: "form content type", method: http.MethodPost, path: "/api/v1/auth/login",
			contentType: "application/x-www-form-urlencoded", origin: "https://panel.example.com", want: http.StatusForbidden,
		},
		{
			name: "no content type", method: http.MethodPost, path: "/api/v1/auth/logout",
			origin: "https://panel.example.com", want: http.StatusForbidden,
		},
		{
			name: "read without origin", method: http.MethodGet, path: "/api/v1/auth/session", want: http.StatusOK,
		},
		{
			name: "enrollment is not a browser request", method: http.MethodPost, path: "/enroll", want: http.StatusOK,
		},
		{
			name: "renewal is not a browser request", method: http.MethodPost, path: "/renew", want: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.contentType != "" {
				request.Header.Set("Content-Type", tt.contentType)
			}
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			if tt.fetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", tt.fetchSite)
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %q)", recorder.Code, tt.want, recorder.Body.String())
			}
			if tt.want == http.StatusForbidden && recorder.Body.String() != "{\"error\":{\"code\":\"forbidden_origin\"}}\n" {
				t.Errorf("body = %q, want the forbidden_origin code", recorder.Body.String())
			}
		})
	}
}
