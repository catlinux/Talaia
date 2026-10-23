package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

type oidcLoginTestIDP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey
	keyID  string

	mu             sync.Mutex
	clientID       string
	code           string
	subject        string
	email          string
	emailVerified  bool
	name           string
	expiry         time.Time
	nonce          string
	challenge      string
	lastVerifier   string
	tokenCalls     int
	discoveryCalls int
}

func newOIDCLoginTestIDP(t *testing.T) *oidcLoginTestIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate oidc signing key: %v", err)
	}
	idp := &oidcLoginTestIDP{
		t: t, key: key, keyID: "login-test-key",
		clientID: "talaia-oidc-client", code: "test-code",
		subject: "subject-1", email: "alice@example.com", emailVerified: true,
		name: "Alice", expiry: time.Now().Add(time.Hour),
	}
	idp.server = httptest.NewServer(http.HandlerFunc(idp.handle))
	t.Cleanup(idp.server.Close)
	return idp
}

func (f *oidcLoginTestIDP) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		f.mu.Lock()
		f.discoveryCalls++
		f.mu.Unlock()
		writeLoginTestJSON(w, map[string]any{
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
		writeLoginTestJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: &f.key.PublicKey, KeyID: f.keyID, Algorithm: string(jose.RS256), Use: "sig",
		}}})
	case "/token":
		f.token(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *oidcLoginTestIDP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.tokenCalls++
	f.lastVerifier = r.PostForm.Get("code_verifier")
	code := f.code
	challenge := f.challenge
	nonce := f.nonce
	subject := f.subject
	email := f.email
	verified := f.emailVerified
	name := f.name
	expiry := f.expiry
	f.mu.Unlock()

	verifier := r.PostForm.Get("code_verifier")
	if r.PostForm.Get("code") != code || verifier == "" ||
		(challenge != "" && oauth2.S256ChallengeFromVerifier(verifier) != challenge) {
		writeLoginTestJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}

	claims := struct {
		Issuer        string `json:"iss"`
		Audience      string `json:"aud"`
		Subject       string `json:"sub"`
		Nonce         string `json:"nonce"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Expiry        int64  `json:"exp"`
		IssuedAt      int64  `json:"iat"`
	}{
		Issuer: f.server.URL, Audience: f.clientID, Subject: subject, Nonce: nonce,
		Email: email, EmailVerified: verified, Name: name,
		Expiry: expiry.Unix(), IssuedAt: time.Now().Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		http.Error(w, "encode claims", http.StatusInternalServerError)
		return
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", f.keyID),
	)
	if err != nil {
		http.Error(w, "create signer", http.StatusInternalServerError)
		return
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		http.Error(w, "sign token", http.StatusInternalServerError)
		return
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		http.Error(w, "serialize token", http.StatusInternalServerError)
		return
	}
	writeLoginTestJSON(w, map[string]any{
		"access_token": "test-access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": raw,
	})
}

func writeLoginTestJSON(w http.ResponseWriter, value any) {
	writeLoginTestJSONStatus(w, http.StatusOK, value)
}

func writeLoginTestJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		_, _ = fmt.Fprintln(w, "encode response")
	}
}

func (f *oidcLoginTestIDP) setFlow(nonce, challenge string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nonce = nonce
	f.challenge = challenge
}

func (f *oidcLoginTestIDP) setUser(email, subject string, verified bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.email = email
	f.subject = subject
	f.emailVerified = verified
}

func (f *oidcLoginTestIDP) verifier() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastVerifier
}

func (f *oidcLoginTestIDP) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenCalls
}

func oidcNoRedirectClient(t *testing.T, panel *testPanel) *http.Client {
	t.Helper()
	jar, err := newTestJar()
	if err != nil {
		t.Fatalf("create oidc cookie jar: %v", err)
	}
	return &http.Client{
		Transport: panel.server.Client().Transport,
		Jar:       jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// newTestJar keeps this test independent of the cookie jar type used elsewhere.
func newTestJar() (http.CookieJar, error) {
	return cookiejar.New(nil)
}

func oidcStartFlow(t *testing.T, panel *testPanel, idp *oidcLoginTestIDP, client *http.Client) (state, nonce string, response *http.Response) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, panel.server.URL+"/api/v1/auth/oidc/start", nil)
	if err != nil {
		t.Fatalf("build oidc start: %v", err)
	}
	response, err = client.Do(request)
	if err != nil {
		t.Fatalf("oidc start: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("oidc start status = %d, want 302 (%s)", response.StatusCode, body)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse oidc location: %v", err)
	}
	query := location.Query()
	state, nonce = query.Get("state"), query.Get("nonce")
	if state == "" || nonce == "" || query.Get("code_challenge") == "" ||
		query.Get("code_challenge_method") != "S256" {
		t.Fatalf("oidc authorization parameters = %v", query)
	}
	idp.setFlow(nonce, query.Get("code_challenge"))
	return state, nonce, response
}

func oidcCallbackFlow(t *testing.T, panel *testPanel, client *http.Client, state, code string) *http.Response {
	t.Helper()
	values := url.Values{"state": {state}, "code": {code}}
	request, err := http.NewRequest(http.MethodGet,
		panel.server.URL+"/api/v1/auth/oidc/callback?"+values.Encode(), nil)
	if err != nil {
		t.Fatalf("build oidc callback: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("oidc callback: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func findResponseCookie(response *http.Response, name string) *http.Cookie {
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func oidcFailureLocation(t *testing.T, response *http.Response) {
	t.Helper()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("oidc failure status = %d, want 302", response.StatusCode)
	}
	if got := response.Header.Get("Location"); got != oidcFailureURL {
		t.Fatalf("oidc failure location = %q, want %q", got, oidcFailureURL)
	}
	if cookie := findResponseCookie(response, SessionCookieName); cookie != nil && cookie.Value != "" {
		t.Fatal("a failure set a session cookie")
	}
}

func createOIDCLoginUser(t *testing.T, panel *testPanel, name, email string) store.User {
	t.Helper()
	user, err := panel.store.CreateUserFull(context.Background(), name, auth.HashPassword(testPassword), name, email)
	if err != nil {
		t.Fatalf("create oidc user %s: %v", name, err)
	}
	return user
}

func saveOIDCLoginConfig(t *testing.T, panel *testPanel, idp *oidcLoginTestIDP) {
	t.Helper()
	if _, err := panel.store.SaveOIDCConfig(context.Background(), store.OIDCConfig{
		Enabled: true, Issuer: idp.server.URL, ClientID: idp.clientID, ButtonLabel: "Company SSO",
		UpdatedBy: "test",
	}); err != nil {
		t.Fatalf("save oidc config: %v", err)
	}
}

func TestOIDCLoginHappyPathAndSubjectBinding(t *testing.T) {
	panel := newTestPanel(t)
	idp := newOIDCLoginTestIDP(t)
	saveOIDCLoginConfig(t, panel, idp)
	user := createOIDCLoginUser(t, panel, "oidc-alice", "alice@example.com")
	idp.setUser("alice@example.com", "subject-1", true)

	configResponse := panel.do(t, panel.client, http.MethodGet, "/api/v1/auth/oidc/config", nil)
	if configResponse.StatusCode != http.StatusOK {
		t.Fatalf("oidc config status = %d, want 200", configResponse.StatusCode)
	}
	var rawConfig map[string]any
	decodeBody(t, configResponse, &rawConfig)
	if len(rawConfig) != 2 || rawConfig["enabled"] != true || rawConfig["label"] != "Company SSO" {
		t.Fatalf("oidc config body = %v", rawConfig)
	}
	if _, ok := rawConfig["issuer"]; ok {
		t.Fatal("oidc config exposes the issuer")
	}

	client := oidcNoRedirectClient(t, panel)
	state, _, start := oidcStartFlow(t, panel, idp, client)
	if cookie := findResponseCookie(start, OIDCCookieName); cookie == nil {
		t.Fatal("oidc start did not set the state cookie")
	} else {
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode ||
			cookie.Path != "/" || cookie.MaxAge != 600 {
			t.Fatalf("oidc state cookie = %+v", cookie)
		}
	}

	callback := oidcCallbackFlow(t, panel, client, state, idp.code)
	if callback.StatusCode != http.StatusFound || callback.Header.Get("Location") != "/" {
		t.Fatalf("oidc callback = %d %q", callback.StatusCode, callback.Header.Get("Location"))
	}
	if cookie := findResponseCookie(callback, SessionCookieName); cookie == nil || cookie.Value == "" {
		t.Fatal("oidc callback did not set a session cookie")
	}
	if cookie := findResponseCookie(callback, OIDCCookieName); cookie == nil || cookie.MaxAge != -1 {
		t.Fatalf("oidc state cookie was not cleared: %+v", cookie)
	}
	if idp.verifier() == "" {
		t.Fatal("the token endpoint did not receive a PKCE verifier")
	}

	stored, err := panel.store.GetUserByUsername(context.Background(), "oidc-alice")
	if err != nil {
		t.Fatalf("get oidc user: %v", err)
	}
	if stored.ID != user.ID || stored.OIDCSubject != "subject-1" {
		t.Fatalf("stored oidc user = %+v", stored)
	}
	if stage := panel.stage(t, client); stage != store.StageMFAPending {
		t.Fatalf("oidc session stage = %q, want %q", stage, store.StageMFAPending)
	}
	if response := panel.do(t, client, http.MethodGet, "/api/v1/servers", nil); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pending oidc session server status = %d, want 401", response.StatusCode)
	}

	// A later login uses the bound subject even when the email changed.
	if err := panel.store.UpdateUser(context.Background(), user.ID, "Alice", "changed@example.com"); err != nil {
		t.Fatalf("change oidc user email: %v", err)
	}
	idp.setUser("changed@example.com", "subject-1", true)
	secondClient := oidcNoRedirectClient(t, panel)
	secondState, _, _ := oidcStartFlow(t, panel, idp, secondClient)
	second := oidcCallbackFlow(t, panel, secondClient, secondState, idp.code)
	if second.StatusCode != http.StatusFound || second.Header.Get("Location") != "/" {
		t.Fatalf("second oidc callback = %d %q", second.StatusCode, second.Header.Get("Location"))
	}
	if stage := panel.stage(t, secondClient); stage != store.StageMFAPending {
		t.Fatalf("second oidc session stage = %q", stage)
	}

	entries, err := panel.store.ListAudit(context.Background(), 200)
	if err != nil {
		t.Fatalf("list oidc audit: %v", err)
	}
	successes, denied := 0, 0
	for _, entry := range entries {
		switch entry.Action {
		case "oidc_login":
			successes++
			details, _ := json.Marshal(entry.Details)
			if strings.Contains(string(details), idp.code) || strings.Contains(string(details), "test-access-token") {
				t.Fatalf("oidc login audit contains a code or token: %s", details)
			}
		case "oidc_login_denied":
			denied++
		}
	}
	if successes != 2 {
		t.Fatalf("oidc_login audit entries = %d, want 2", successes)
	}
	if denied != 0 {
		t.Fatalf("unexpected denied entries = %d", denied)
	}
}

func TestOIDCLoginFailures(t *testing.T) {
	panel := newTestPanel(t)
	idp := newOIDCLoginTestIDP(t)
	saveOIDCLoginConfig(t, panel, idp)
	ctx := context.Background()

	createOIDCLoginUser(t, panel, "oidc-unverified", "unverified@example.com")
	createOIDCLoginUser(t, panel, "oidc-known", "known@example.com")
	disabled := createOIDCLoginUser(t, panel, "oidc-disabled", "disabled@example.com")
	if err := panel.store.DisableUser(ctx, disabled.ID); err != nil {
		t.Fatalf("disable oidc user: %v", err)
	}
	bound := createOIDCLoginUser(t, panel, "oidc-bound", "bound@example.com")
	if err := panel.store.SetOIDCSubject(ctx, bound.ID, "other-subject"); err != nil {
		t.Fatalf("bind oidc subject: %v", err)
	}

	runFailure := func(t *testing.T, client *http.Client, state, code string, idpUser func()) *http.Response {
		t.Helper()
		if idpUser != nil {
			idpUser()
		}
		response := oidcCallbackFlow(t, panel, client, state, code)
		oidcFailureLocation(t, response)
		return response
	}

	t.Run("email unverified", func(t *testing.T) {
		idp.setUser("unverified@example.com", "unverified-subject", false)
		client := oidcNoRedirectClient(t, panel)
		state, _, _ := oidcStartFlow(t, panel, idp, client)
		runFailure(t, client, state, idp.code, nil)
		stored, _ := panel.store.GetUserByUsername(ctx, "oidc-unverified")
		if stored.OIDCSubject != "" {
			t.Fatal("an unverified email bound an oidc subject")
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		idp.setUser("missing@example.com", "missing-subject", true)
		client := oidcNoRedirectClient(t, panel)
		state, _, _ := oidcStartFlow(t, panel, idp, client)
		runFailure(t, client, state, idp.code, nil)
	})

	t.Run("disabled user", func(t *testing.T) {
		idp.setUser("disabled@example.com", "disabled-subject", true)
		client := oidcNoRedirectClient(t, panel)
		state, _, _ := oidcStartFlow(t, panel, idp, client)
		runFailure(t, client, state, idp.code, nil)
	})

	t.Run("subject mismatch", func(t *testing.T) {
		idp.setUser("bound@example.com", "new-subject", true)
		client := oidcNoRedirectClient(t, panel)
		state, _, _ := oidcStartFlow(t, panel, idp, client)
		runFailure(t, client, state, idp.code, nil)
		stored, _ := panel.store.GetUserByUsername(ctx, "oidc-bound")
		if stored.OIDCSubject != "other-subject" {
			t.Fatal("a mismatched subject was rebound")
		}
	})

	t.Run("wrong state", func(t *testing.T) {
		idp.setUser("known@example.com", "known-subject", true)
		client := oidcNoRedirectClient(t, panel)
		state, _, _ := oidcStartFlow(t, panel, idp, client)
		runFailure(t, client, state+"x", idp.code, nil)
	})

	t.Run("missing cookie", func(t *testing.T) {
		idp.setUser("known@example.com", "known-subject", true)
		startClient := oidcNoRedirectClient(t, panel)
		state, _, _ := oidcStartFlow(t, panel, idp, startClient)
		callbackClient := oidcNoRedirectClient(t, panel)
		runFailure(t, callbackClient, state, idp.code, nil)
	})

	t.Run("tampered cookie", func(t *testing.T) {
		idp.setUser("known@example.com", "known-subject", true)
		startClient := oidcNoRedirectClient(t, panel)
		state, _, start := oidcStartFlow(t, panel, idp, startClient)
		cookie := findResponseCookie(start, OIDCCookieName)
		if cookie == nil || len(cookie.Value) < 2 {
			t.Fatal("missing oidc state cookie")
		}
		// Change a middle character: the tail of a base64 value can encode only
		// unused padding bits, so changing it might decode to the same bytes.
		middle := len(cookie.Value) / 2
		replacement := "A"
		if cookie.Value[middle:middle+1] == "A" {
			replacement = "B"
		}
		tampered := cookie.Value[:middle] + replacement + cookie.Value[middle+1:]
		client := oidcNoRedirectClient(t, panel)
		values := url.Values{"state": {state}, "code": {idp.code}}
		request, _ := http.NewRequest(http.MethodGet,
			panel.server.URL+"/api/v1/auth/oidc/callback?"+values.Encode(), nil)
		request.AddCookie(&http.Cookie{Name: OIDCCookieName, Value: tampered})
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("tampered callback: %v", err)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		oidcFailureLocation(t, response)
	})

	t.Run("replayed callback", func(t *testing.T) {
		idp.setUser("known@example.com", "known-subject", true)
		client := oidcNoRedirectClient(t, panel)
		state, _, _ := oidcStartFlow(t, panel, idp, client)
		first := oidcCallbackFlow(t, panel, client, state, idp.code)
		if first.StatusCode != http.StatusFound || first.Header.Get("Location") != "/" {
			t.Fatalf("first oidc login = %d %q", first.StatusCode, first.Header.Get("Location"))
		}
		replay := oidcCallbackFlow(t, panel, client, state, idp.code)
		if replay.StatusCode != http.StatusFound || replay.Header.Get("Location") != oidcFailureURL {
			t.Fatalf("replayed oidc callback = %d %q", replay.StatusCode, replay.Header.Get("Location"))
		}
	})

	t.Run("state cookie expired", func(t *testing.T) {
		idp.setUser("known@example.com", "known-subject", true)
		client := oidcNoRedirectClient(t, panel)
		state, _, _ := oidcStartFlow(t, panel, idp, client)
		panel.clock.Advance(oidcStateTTL + time.Second)
		runFailure(t, client, state, idp.code, nil)
	})

	entries, err := panel.store.ListAudit(ctx, 500)
	if err != nil {
		t.Fatalf("list oidc failure audit: %v", err)
	}
	reasons := map[string]int{}
	for _, entry := range entries {
		if entry.Action != "oidc_login_denied" {
			continue
		}
		reason, _ := entry.Details["reason"].(string)
		reasons[reason]++
		details, _ := json.Marshal(entry.Details)
		if strings.Contains(string(details), idp.code) || strings.Contains(string(details), "test-access-token") {
			t.Fatalf("oidc denial audit contains a code or token: %s", details)
		}
	}
	for _, reason := range []string{"email_unverified", "user_not_found", "disabled", "subject_mismatch", "state_mismatch"} {
		if reasons[reason] == 0 {
			t.Errorf("audit reasons = %v, want %q", reasons, reason)
		}
	}
}

func TestOIDCLoginDisabled(t *testing.T) {
	panel := newTestPanel(t)
	client := oidcNoRedirectClient(t, panel)
	response, err := client.Get(panel.server.URL + "/api/v1/auth/oidc/start")
	if err != nil {
		t.Fatalf("oidc start while disabled: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled oidc start status = %d, want 404", response.StatusCode)
	}
	if cookie := findResponseCookie(response, SessionCookieName); cookie != nil && cookie.Value != "" {
		t.Fatal("disabled oidc start set a session cookie")
	}
}
