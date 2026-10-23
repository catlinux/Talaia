package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// oidcTestIDP is a small in-process OpenID provider for the package tests.
type oidcTestIDP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey
	other  *rsa.PrivateKey
	keyID  string

	mu               sync.Mutex
	discoveryCount   atomic.Int64
	expectedCode     string
	expectedVerifier string
	issuer           string
	audience         string
	subject          string
	omitSubject      bool
	nonce            string
	email            string
	emailVerified    bool
	name             string
	expiry           time.Time
	tokenKey         *rsa.PrivateKey
	lastTokenForm    url.Values
}

func newOIDCTestIDP(t *testing.T) *oidcTestIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate other signing key: %v", err)
	}
	idp := &oidcTestIDP{
		t: t, key: key, other: other, keyID: "test-key",
		expectedCode: "test-code", subject: "subject-1", email: "alice@example.com",
		emailVerified: true, name: "Alice", expiry: time.Now().Add(time.Hour),
	}
	idp.tokenKey = key
	idp.server = httptest.NewServer(http.HandlerFunc(idp.handle))
	t.Cleanup(idp.server.Close)
	idp.issuer = idp.server.URL
	idp.audience = "talaia-test-client"
	return idp
}

func (f *oidcTestIDP) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		f.discoveryCount.Add(1)
		writeOIDCTestJSON(w, map[string]any{
			"issuer":                                f.server.URL,
			"authorization_endpoint":                f.server.URL + "/authorize",
			"token_endpoint":                        f.server.URL + "/token",
			"jwks_uri":                              f.server.URL + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"scopes_supported":                      []string{"openid", "email", "profile"},
		})
	case "/keys":
		writeOIDCTestJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: &f.key.PublicKey, KeyID: f.keyID, Algorithm: string(jose.RS256), Use: "sig",
		}}})
	case "/token":
		f.token(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *oidcTestIDP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.lastTokenForm = r.PostForm
	code, verifier := f.expectedCode, f.expectedVerifier
	expectedNonce := f.nonce
	f.mu.Unlock()

	if r.PostForm.Get("code") != code ||
		(verifier != "" && r.PostForm.Get("code_verifier") != verifier) {
		writeOIDCTestJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error": "invalid_grant",
		})
		return
	}

	f.mu.Lock()
	claims := oidcTestClaims{
		Issuer: f.issuer, Audience: f.audience, Subject: f.subject,
		Nonce: expectedNonce, Email: f.email, EmailVerified: f.emailVerified,
		Name: f.name, Expiry: f.expiry.Unix(), IssuedAt: time.Now().Unix(),
	}
	omitSubject, tokenKey := f.omitSubject, f.tokenKey
	f.mu.Unlock()
	if omitSubject {
		claims.Subject = ""
	}

	rawClaims, err := json.Marshal(claims)
	if err != nil {
		http.Error(w, "encode claims", http.StatusInternalServerError)
		return
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: tokenKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", f.keyID),
	)
	if err != nil {
		http.Error(w, "create signer", http.StatusInternalServerError)
		return
	}
	signed, err := signer.Sign(rawClaims)
	if err != nil {
		http.Error(w, "sign token", http.StatusInternalServerError)
		return
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		http.Error(w, "serialize token", http.StatusInternalServerError)
		return
	}
	writeOIDCTestJSON(w, map[string]any{
		"access_token": "access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     raw,
	})
}

type oidcTestClaims struct {
	Issuer        string `json:"iss"`
	Audience      string `json:"aud"`
	Subject       string `json:"sub,omitempty"`
	Nonce         string `json:"nonce"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Expiry        int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
}

func writeOIDCTestJSON(w http.ResponseWriter, value any) {
	writeOIDCTestJSONStatus(w, http.StatusOK, value)
}

func writeOIDCTestJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		_, _ = fmt.Fprintln(w, "encode response")
	}
}

func (f *oidcTestIDP) form() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := url.Values{}
	for key, values := range f.lastTokenForm {
		copy[key] = append([]string(nil), values...)
	}
	return copy
}
