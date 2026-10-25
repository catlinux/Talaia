package netguard

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateURL(t *testing.T) {
	for _, raw := range []string{"file:///tmp/x", "ftp://example.com", "gopher://example.com", "http://user:pass@example.com", "http://2130706433/", "http://0x7f.0.0.1/", "http://017700000001/", "http:///path", "http://127.1/", "http://0x7f000001/"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ValidateURL(raw); err == nil {
				t.Fatalf("ValidateURL(%q) succeeded", raw)
			}
		})
	}
	for _, raw := range []string{"http://[::1]/", "http://localhost", "http://127.0.0.1/", "https://example.com", "https://bad.cafe", "http://dead.beef.example/"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ValidateURL(raw); err != nil {
				t.Fatalf("ValidateURL(%q): %v", raw, err)
			}
		})
	}
}

func TestHTTPClientGuardsResolvedAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer server.Close()
	allowed := false
	client := NewHTTPClient(HTTPClientOptions{AllowPrivate: func() bool { return allowed }})
	if _, err := client.Get(server.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("blocked request error=%v", err)
	}
	allowed = true
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	allowed = false
	if _, err := client.Get(server.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("toggled request error=%v", err)
	}
}

func TestHTTPClientBlocksLocalhostResolution(t *testing.T) {
	client := NewHTTPClient(HTTPClientOptions{})
	_, err := client.Get("http://localhost/")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("localhost request error=%v", err)
	}
}

func TestRedirectValidation(t *testing.T) {
	client := NewHTTPClient(HTTPClientOptions{AllowPrivate: func() bool { return true }})
	base := httptest.NewRequest("GET", "http://example.com", nil)
	for _, target := range []string{"ftp://example.com/", "http://example.com/"} {
		req := httptest.NewRequest("GET", target, nil)
		if err := client.CheckRedirect(req, []*http.Request{base}); target[0:3] == "ftp" && err == nil {
			t.Fatal("ftp redirect accepted")
		}
	}
	via := []*http.Request{base, base, base, base}
	if err := client.CheckRedirect(base, via); err == nil {
		t.Fatal("fourth redirect accepted")
	}
}

func TestReadLimited(t *testing.T) {
	got, err := ReadLimited(strings.NewReader("abcdefghij"), 4)
	if err != nil || string(got) != "abcd" {
		t.Fatalf("ReadLimited() = %q, %v", got, err)
	}
	got, err = ReadLimited(strings.NewReader(strings.Repeat("x", maxBodyBytes+1)), maxBodyBytes+1)
	if err != nil || len(got) != maxBodyBytes {
		t.Fatalf("hard cap len=%d err=%v", len(got), err)
	}
}

func TestProxyEnvironmentIgnored(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("https_proxy", proxy.URL)
	client := NewHTTPClient(HTTPClientOptions{AllowPrivate: func() bool { return true }})
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestDialTCPRejectsUnsupportedNetwork(t *testing.T) {
	if _, err := DialTCP(context.Background(), "udp", "127.0.0.1:80", nil, 0); err == nil {
		t.Fatal("udp accepted")
	}
}
