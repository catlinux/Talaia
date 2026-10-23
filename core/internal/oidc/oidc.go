// Package oidc wraps OpenID Connect discovery, the authorization URL and the
// code exchange used by the panel.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	// discoveryTTL is how long a discovered provider is reused before it is
	// fetched again.
	discoveryTTL = 10 * time.Minute
	// discoveryTimeout bounds every discovery and JWKS request.
	discoveryTimeout = 10 * time.Second
	// RedirectPath is the callback path the provider sends the browser to.
	RedirectPath = "/api/v1/auth/oidc/callback"
)

// Config is the configuration of one OIDC provider.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Label        string
}

// Claims are the useful claims of an ID token.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// Provider is a discovered OIDC provider. It is safe for concurrent use.
type Provider struct {
	config Config
	client *http.Client
	now    func() time.Time

	mu           sync.Mutex
	discovered   *gooidc.Provider
	discoveredAt time.Time
	oauth        *oauth2.Config
}

// NewProvider validates the configuration and performs discovery. Discovery
// is repeated when the cache is older than ten minutes or when Refresh is
// called.
func NewProvider(ctx context.Context, config Config) (*Provider, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	provider := &Provider{
		config: config,
		client: &http.Client{Timeout: discoveryTimeout},
		now:    time.Now,
	}
	if err := provider.Refresh(ctx); err != nil {
		return nil, err
	}
	return provider, nil
}

// New is an alias of NewProvider.
func New(ctx context.Context, config Config) (*Provider, error) {
	return NewProvider(ctx, config)
}

func validateConfig(config Config) error {
	if err := ValidateIssuer(config.Issuer); err != nil {
		return err
	}
	if strings.TrimSpace(config.ClientID) == "" {
		return errors.New("oidc client id is required")
	}
	if strings.TrimSpace(config.RedirectURL) == "" {
		return errors.New("oidc redirect url is required")
	}
	return nil
}

// ValidateIssuer accepts https issuers and, for development and tests, plain
// http on localhost or 127.0.0.1.
func ValidateIssuer(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("oidc issuer is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid oidc issuer: %w", err)
	}
	if parsed.Host == "" {
		return errors.New("oidc issuer has no host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("oidc issuer must not contain credentials, a query or a fragment")
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if scheme == "https" {
		return nil
	}
	if scheme == "http" && (host == "localhost" || host == "127.0.0.1") {
		return nil
	}
	return errors.New("oidc issuer must use https, except http on localhost or 127.0.0.1")
}

// Refresh fetches the discovery document now. Without a context it uses the
// background context.
func (p *Provider) Refresh(contexts ...context.Context) error {
	var ctx context.Context
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	ctx = gooidc.ClientContext(ctx, p.client)

	discovered, err := gooidc.NewProvider(ctx, p.config.Issuer)
	if err != nil {
		return fmt.Errorf("discover oidc issuer: %w", err)
	}
	oauthConfig := &oauth2.Config{
		ClientID:     p.config.ClientID,
		ClientSecret: p.config.ClientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  p.config.RedirectURL,
		Scopes:       []string{gooidc.ScopeOpenID, "email", "profile"},
	}

	p.mu.Lock()
	p.discovered = discovered
	p.discoveredAt = p.now()
	p.oauth = oauthConfig
	p.mu.Unlock()
	return nil
}

// AuthURL builds the authorization URL with state, nonce and an S256 PKCE
// challenge. Empty values are rejected by returning an empty URL.
func (p *Provider) AuthURL(state, nonce, codeVerifier string) string {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(nonce) == "" || codeVerifier == "" {
		return ""
	}
	p.mu.Lock()
	oauthConfig := p.oauth
	p.mu.Unlock()
	if oauthConfig == nil {
		return ""
	}
	return oauthConfig.AuthCodeURL(
		state,
		gooidc.Nonce(nonce),
		oauth2.S256ChallengeOption(codeVerifier),
	)
}

// Exchange exchanges an authorization code and verifies the ID token. The
// issuer, audience, signature, expiry and nonce are checked against the
// provider. The caller still has to require an email address and
// EmailVerified when that is part of its policy.
func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, expectedNonce string) (Claims, error) {
	if strings.TrimSpace(code) == "" || codeVerifier == "" || strings.TrimSpace(expectedNonce) == "" {
		return Claims{}, errors.New("code, pkce verifier and nonce are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.client)
	ctx = gooidc.ClientContext(ctx, p.client)
	provider, oauthConfig, err := p.providerFor(ctx)
	if err != nil {
		return Claims{}, err
	}
	token, err := oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return Claims{}, fmt.Errorf("exchange oidc code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Claims{}, errors.New("the oidc token response has no id_token")
	}

	verifier := provider.VerifierContext(ctx, &gooidc.Config{ClientID: p.config.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Claims{}, fmt.Errorf("verify oidc id token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(expectedNonce)) != 1 {
		return Claims{}, errors.New("the oidc id token has the wrong nonce")
	}
	if strings.TrimSpace(idToken.Subject) == "" {
		return Claims{}, errors.New("the oidc id token has no subject")
	}

	var tokenClaims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&tokenClaims); err != nil {
		return Claims{}, fmt.Errorf("read oidc claims: %w", err)
	}
	return Claims{
		Subject:       idToken.Subject,
		Email:         tokenClaims.Email,
		EmailVerified: tokenClaims.EmailVerified,
		Name:          tokenClaims.Name,
	}, nil
}

func (p *Provider) providerFor(ctx context.Context) (*gooidc.Provider, *oauth2.Config, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.discovered != nil && p.now().Before(p.discoveredAt.Add(discoveryTTL)) {
		return p.discovered, p.oauth, nil
	}
	// The caller asked for a fresh discovery: Refresh also takes the lock, so
	// do the fetch inline while holding it.
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	ctx = gooidc.ClientContext(ctx, p.client)
	discovered, err := gooidc.NewProvider(ctx, p.config.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("discover oidc issuer: %w", err)
	}
	p.discovered = discovered
	p.discoveredAt = p.now()
	p.oauth = &oauth2.Config{
		ClientID:     p.config.ClientID,
		ClientSecret: p.config.ClientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  p.config.RedirectURL,
		Scopes:       []string{gooidc.ScopeOpenID, "email", "profile"},
	}
	return p.discovered, p.oauth, nil
}

// NewState returns a random OAuth state parameter.
func NewState() (string, error) {
	return randomValue(32)
}

// NewNonce returns a random OpenID Connect nonce.
func NewNonce() (string, error) {
	return randomValue(32)
}

// NewPKCEVerifier returns a random PKCE code verifier, valid for S256.
func NewPKCEVerifier() (string, error) {
	return randomValue(32)
}

// NewCodeVerifier is an alias of NewPKCEVerifier.
func NewCodeVerifier() (string, error) {
	return NewPKCEVerifier()
}

func randomValue(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate oidc random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
