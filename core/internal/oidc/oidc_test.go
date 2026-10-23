package oidc

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func oidcTestProvider(t *testing.T, idp *oidcTestIDP) *Provider {
	t.Helper()
	provider, err := NewProvider(context.Background(), Config{
		Issuer:       idp.server.URL,
		ClientID:     idp.audience,
		ClientSecret: "test-secret",
		RedirectURL:  "https://panel.example.com" + RedirectPath,
		Label:        "SSO",
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	return provider
}

func TestProviderAuthURL(t *testing.T) {
	idp := newOIDCTestIDP(t)
	provider := oidcTestProvider(t, idp)
	verifier, err := NewPKCEVerifier()
	if err != nil {
		t.Fatalf("generate verifier: %v", err)
	}

	raw := provider.AuthURL("state-1", "nonce-1", verifier)
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	if parsed.Scheme != "http" || parsed.Host != strings.TrimPrefix(idp.server.URL, "http://") {
		t.Fatalf("auth url = %q", raw)
	}
	query := parsed.Query()
	if query.Get("state") != "state-1" || query.Get("nonce") != "nonce-1" {
		t.Fatalf("state or nonce missing from %q", raw)
	}
	if query.Get("response_type") != "code" || query.Get("client_id") != idp.audience {
		t.Fatalf("authorization parameters = %v", query)
	}
	if query.Get("redirect_uri") != "https://panel.example.com"+RedirectPath {
		t.Fatalf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	for _, scope := range []string{"openid", "email", "profile"} {
		if !strings.Contains(" "+query.Get("scope")+" ", " "+scope+" ") {
			t.Fatalf("scope %q missing from %q", scope, query.Get("scope"))
		}
	}
	if query.Get("code_challenge_method") != "S256" ||
		query.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(verifier) {
		t.Fatalf("pkce parameters = %v", query)
	}
	if raw := provider.AuthURL("", "nonce", verifier); raw != "" {
		t.Fatalf("AuthURL without state = %q, want empty", raw)
	}
}

func TestProviderExchange(t *testing.T) {
	idp := newOIDCTestIDP(t)
	verifier, err := NewPKCEVerifier()
	if err != nil {
		t.Fatalf("generate verifier: %v", err)
	}
	idp.expectedVerifier = verifier
	idp.nonce = "expected-nonce"
	provider := oidcTestProvider(t, idp)

	claims, err := provider.Exchange(context.Background(), idp.expectedCode, verifier, idp.nonce)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if claims.Subject != "subject-1" || claims.Email != "alice@example.com" ||
		!claims.EmailVerified || claims.Name != "Alice" {
		t.Fatalf("claims = %+v", claims)
	}
	if got := idp.form().Get("code_verifier"); got != verifier {
		t.Fatalf("code_verifier = %q, want %q", got, verifier)
	}
	if got := idp.discoveryCount.Load(); got != 1 {
		t.Fatalf("discovery count = %d, want 1", got)
	}

	// The discovery cache means a second exchange does not fetch discovery again.
	if _, err := provider.Exchange(context.Background(), idp.expectedCode, verifier, idp.nonce); err != nil {
		t.Fatalf("second exchange: %v", err)
	}
	if got := idp.discoveryCount.Load(); got != 1 {
		t.Fatalf("discovery count after cached exchange = %d, want 1", got)
	}
	if err := provider.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh discovery: %v", err)
	}
	if got := idp.discoveryCount.Load(); got != 2 {
		t.Fatalf("discovery count after refresh = %d, want 2", got)
	}
}

func TestProviderRejectsInvalidIDTokens(t *testing.T) {
	idp := newOIDCTestIDP(t)
	verifier, err := NewPKCEVerifier()
	if err != nil {
		t.Fatalf("generate verifier: %v", err)
	}
	idp.expectedVerifier = verifier
	idp.nonce = "expected-nonce"
	provider := oidcTestProvider(t, idp)

	reset := func() {
		idp.mu.Lock()
		idp.issuer = idp.server.URL
		idp.audience = "talaia-test-client"
		idp.subject = "subject-1"
		idp.omitSubject = false
		idp.nonce = "expected-nonce"
		idp.expiry = time.Now().Add(time.Hour)
		idp.tokenKey = idp.key
		idp.mu.Unlock()
	}

	tests := []struct {
		name   string
		mutate func()
	}{
		{
			name: "wrong nonce",
			mutate: func() {
				idp.mu.Lock()
				idp.nonce = "other-nonce"
				idp.mu.Unlock()
			},
		},
		{
			name: "wrong audience",
			mutate: func() {
				idp.mu.Lock()
				idp.audience = "another-client"
				idp.mu.Unlock()
			},
		},
		{
			name: "wrong issuer",
			mutate: func() {
				idp.mu.Lock()
				idp.issuer = "https://another.example.com"
				idp.mu.Unlock()
			},
		},
		{
			name: "expired token",
			mutate: func() {
				idp.mu.Lock()
				idp.expiry = time.Now().Add(-time.Minute)
				idp.mu.Unlock()
			},
		},
		{
			name: "bad signature",
			mutate: func() {
				idp.mu.Lock()
				idp.tokenKey = idp.other
				idp.mu.Unlock()
			},
		},
		{
			name: "missing subject",
			mutate: func() {
				idp.mu.Lock()
				idp.omitSubject = true
				idp.mu.Unlock()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reset()
			test.mutate()
			if _, err := provider.Exchange(context.Background(), idp.expectedCode, verifier, "expected-nonce"); err == nil {
				t.Fatal("invalid id token was accepted")
			}
		})
	}
	reset()

	if _, err := provider.Exchange(context.Background(), "", verifier, "expected-nonce"); err == nil {
		t.Fatal("exchange without code was accepted")
	}
	if _, err := provider.Exchange(context.Background(), idp.expectedCode, "", "expected-nonce"); err == nil {
		t.Fatal("exchange without verifier was accepted")
	}
	if _, err := provider.Exchange(context.Background(), idp.expectedCode, verifier, ""); err == nil {
		t.Fatal("exchange without nonce was accepted")
	}
}

func TestValidateIssuer(t *testing.T) {
	tests := []struct {
		issuer string
		ok     bool
	}{
		{"https://accounts.example.com", true},
		{"https://accounts.example.com/path", true},
		{"http://localhost:9000", true},
		{"http://127.0.0.1:9000", true},
		{"http://localhost.evil.example", false},
		{"http://127.0.0.1.evil.example", false},
		{"http://accounts.example.com", false},
		{"ftp://accounts.example.com", false},
		{"https://user:pass@accounts.example.com", false},
		{"https://accounts.example.com?x=1", false},
		{"", false},
	}
	for _, test := range tests {
		err := ValidateIssuer(test.issuer)
		if (err == nil) != test.ok {
			t.Errorf("ValidateIssuer(%q) error = %v, want ok=%v", test.issuer, err, test.ok)
		}
	}

	if _, err := NewProvider(context.Background(), Config{
		Issuer: "http://example.com", ClientID: "client",
		RedirectURL: "https://panel.example.com" + RedirectPath,
	}); err == nil {
		t.Fatal("NewProvider accepted a non-https issuer")
	}
}

func TestRandomHelpers(t *testing.T) {
	state, err := NewState()
	if err != nil || state == "" {
		t.Fatalf("state = %q, %v", state, err)
	}
	nonce, err := NewNonce()
	if err != nil || nonce == "" {
		t.Fatalf("nonce = %q, %v", nonce, err)
	}
	verifier, err := NewPKCEVerifier()
	if err != nil || len(verifier) < 43 || len(verifier) > 128 {
		t.Fatalf("verifier = %q, %v", verifier, err)
	}
}
