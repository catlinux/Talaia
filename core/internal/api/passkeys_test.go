package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/config"
	"github.com/catlinux/Talaia/core/internal/store"
)

const testPasskeyRPID = "panel.example.com"

// passkeyTestPanel is a small core with only the authentication API, a full
// session and passkey support configured.
type passkeyTestPanel struct {
	server *httptest.Server
	store  *store.Store
	clock  *testClock
	client *http.Client
	user   store.User
	rpID   string
}

func newPasskeyTestPanel(t *testing.T, rpID string) *passkeyTestPanel {
	t.Helper()

	st := testStore(t)
	clock := newTestClock()
	key, err := auth.LoadOrCreateKey(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatalf("secrets key: %v", err)
	}
	box, err := auth.NewSecretBox(key)
	if err != nil {
		t.Fatalf("secret box: %v", err)
	}
	router := NewRouter(RouterDeps{
		DB:       st,
		WebDir:   t.TempDir(),
		Security: SecurityConfig{TLS: true, Origins: []string{testOrigin}},
		Session:  SessionDeps{Store: st, Now: clock.Now},
		Auth: AuthDeps{
			Store:          st,
			Box:            box,
			Limiter:        NewRateLimiterWithClock(1000, time.Minute, clock.Now),
			Now:            clock.Now,
			LockDuration:   time.Minute,
			DeviceMFAHours: 12,
			PasskeyRPID:    rpID,
			PasskeyOrigins: []string{testOrigin},
		},
	})
	server := httptest.NewTLSServer(router)
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Transport: server.Client().Transport, Jar: jar}

	ctx := context.Background()
	id, err := st.CreateUser(ctx, "admin", auth.HashPassword(testPassword))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, err := st.GetUser(ctx, id)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	token := auth.NewSessionToken()
	if _, err := st.CreateSession(ctx, user.ID, auth.HashToken(token), store.StageFull,
		clock.Now().Add(fullSessionTTL), "192.0.2.10", "test"); err != nil {
		t.Fatalf("create full session: %v", err)
	}
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	jar.SetCookies(parsed, []*http.Cookie{sessionCookie(token, store.StageFull)})

	return &passkeyTestPanel{
		server: server,
		store:  st,
		clock:  clock,
		client: client,
		user:   user,
		rpID:   rpID,
	}
}

func (p *passkeyTestPanel) newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{Transport: p.server.Client().Transport, Jar: jar}
}

func (p *passkeyTestPanel) do(t *testing.T, client *http.Client, method, path string, body any) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s: %v", path, err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, p.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build %s: %v", path, err)
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Origin", testOrigin)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func (p *passkeyTestPanel) post(t *testing.T, client *http.Client, path string, body any) *http.Response {
	t.Helper()
	return p.do(t, client, http.MethodPost, path, body)
}

func (p *passkeyTestPanel) cookieValueNamed(t *testing.T, client *http.Client, name string) string {
	t.Helper()
	if client.Jar == nil {
		return ""
	}
	for _, cookie := range client.Jar.Cookies(mustParseURL(t, p.server.URL)) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func (p *passkeyTestPanel) stage(t *testing.T, client *http.Client) string {
	t.Helper()
	response := p.do(t, client, http.MethodGet, "/api/v1/auth/session", nil)
	if response.StatusCode != http.StatusOK {
		return ""
	}
	var body sessionResponse
	decodeBody(t, response, &body)
	return body.Stage
}

func (p *passkeyTestPanel) listPasskeys(t *testing.T, client *http.Client) (trustedPasskeysResponse, []byte) {
	t.Helper()
	response := p.do(t, client, http.MethodGet, "/api/v1/auth/passkeys", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list passkeys status = %d", response.StatusCode)
	}
	raw := readResponseBody(t, response)
	var body trustedPasskeysResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode passkeys: %v", err)
	}
	return body, raw
}

// trustedPasskeysResponse aliases the API shape to keep the test readable.
type trustedPasskeysResponse = passkeysResponse

func (p *passkeyTestPanel) beginLogin(t *testing.T, client *http.Client) (string, []byte) {
	t.Helper()
	response := p.post(t, client, "/api/v1/auth/passkey/login/begin", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login begin status = %d", response.StatusCode)
	}
	var body struct {
		CeremonyID string          `json:"ceremony_id"`
		Options    json.RawMessage `json:"options"`
	}
	decodeBody(t, response, &body)
	if body.CeremonyID == "" || len(body.Options) == 0 {
		t.Fatalf("login begin body = %+v", body)
	}
	return body.CeremonyID, body.Options
}

func (p *passkeyTestPanel) finishLogin(t *testing.T, client *http.Client, ceremonyID string, assertion []byte) *http.Response {
	t.Helper()
	return p.post(t, client, "/api/v1/auth/passkey/login/finish", map[string]any{
		"ceremony_id": ceremonyID,
		"credential":  json.RawMessage(assertion),
	})
}

func (p *passkeyTestPanel) registerPasskey(t *testing.T, client *http.Client, authenticator *softwareAuthenticator, name string) passkeyEntry {
	t.Helper()
	response := p.post(t, client, "/api/v1/auth/passkeys/register/begin", map[string]string{
		"password": testPassword,
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("register begin status = %d", response.StatusCode)
	}
	options := readResponseBody(t, response)
	credential := authenticator.register(t, options)
	response = p.post(t, client, "/api/v1/auth/passkeys/register/finish", map[string]any{
		"name":       name,
		"credential": json.RawMessage(credential),
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register finish status = %d: %s", response.StatusCode, readResponseBody(t, response))
	}
	raw := readResponseBody(t, response)
	var entry passkeyEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("decode passkey: %v", err)
	}
	return entry
}

// softwareAuthenticator is a minimal ES256 authenticator with no attestation.
// It is enough to exercise registration, assertions and counter checks without
// a browser.
type softwareAuthenticator struct {
	rpID         string
	origin       string
	userHandle   []byte
	credentialID []byte
	privateKey   *ecdsa.PrivateKey
	aaguid       []byte
	signCount    uint32
}

func newSoftwareAuthenticator(origin string) *softwareAuthenticator {
	return &softwareAuthenticator{origin: origin, aaguid: bytes.Repeat([]byte{0x42}, 16)}
}

func (a *softwareAuthenticator) register(t *testing.T, optionsRaw []byte) []byte {
	t.Helper()

	var options struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(optionsRaw, &options); err != nil {
		t.Fatalf("decode registration options: %v", err)
	}
	if options.PublicKey.RP.ID != "" {
		a.rpID = options.PublicKey.RP.ID
	}
	handle, err := base64.RawURLEncoding.DecodeString(options.PublicKey.User.ID)
	if err != nil {
		t.Fatalf("decode user handle: %v", err)
	}
	a.userHandle = handle

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	a.privateKey = key
	a.credentialID = make([]byte, 32)
	if _, err := rand.Read(a.credentialID); err != nil {
		t.Fatalf("credential id: %v", err)
	}
	a.signCount = 0

	publicKeyBytes, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("encode public key: %v", err)
	}
	cose, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{
			KeyType:   2,
			Algorithm: int64(webauthncose.AlgES256),
		},
		Curve:  1,
		XCoord: publicKeyBytes[1:33],
		YCoord: publicKeyBytes[33:],
	})
	if err != nil {
		t.Fatalf("encode cose key: %v", err)
	}
	authData := a.registrationAuthData(cose)
	attestation, err := webauthncbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	if err != nil {
		t.Fatalf("encode attestation: %v", err)
	}
	clientData, err := json.Marshal(map[string]any{
		"type":        "webauthn.create",
		"challenge":   options.PublicKey.Challenge,
		"origin":      a.origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("encode client data: %v", err)
	}
	response, err := json.Marshal(map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(a.credentialID),
		"rawId": base64.RawURLEncoding.EncodeToString(a.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"attestationObject": base64.RawURLEncoding.EncodeToString(attestation),
			"transports":        []string{"internal"},
		},
	})
	if err != nil {
		t.Fatalf("encode registration response: %v", err)
	}
	return response
}

func (a *softwareAuthenticator) login(t *testing.T, optionsRaw []byte) []byte {
	t.Helper()
	a.signCount++
	return a.assertion(t, optionsRaw, a.origin, a.rpID, true, a.signCount)
}

func (a *softwareAuthenticator) assertion(t *testing.T, optionsRaw []byte, origin, rpID string, uv bool, counter uint32) []byte {
	t.Helper()

	var options struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(optionsRaw, &options); err != nil {
		t.Fatalf("decode assertion options: %v", err)
	}
	if origin == "" {
		origin = a.origin
	}
	if rpID == "" {
		rpID = a.rpID
	}
	flags := byte(protocol.FlagUserPresent)
	if uv {
		flags |= byte(protocol.FlagUserVerified)
	}
	authData := a.assertionAuthData(rpID, flags, counter)
	clientData, err := json.Marshal(map[string]any{
		"type":        "webauthn.get",
		"challenge":   options.PublicKey.Challenge,
		"origin":      origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("encode assertion client data: %v", err)
	}
	clientDataHash := sha256.Sum256(clientData)
	signed := append(append([]byte{}, authData...), clientDataHash[:]...)
	digest := sha256.Sum256(signed)
	signature, err := ecdsa.SignASN1(rand.Reader, a.privateKey, digest[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	response, err := json.Marshal(map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(a.credentialID),
		"rawId": base64.RawURLEncoding.EncodeToString(a.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"signature":         base64.RawURLEncoding.EncodeToString(signature),
			"userHandle":        base64.RawURLEncoding.EncodeToString(a.userHandle),
		},
	})
	if err != nil {
		t.Fatalf("encode assertion: %v", err)
	}
	return response
}

func (a *softwareAuthenticator) registrationAuthData(coseKey []byte) []byte {
	flags := byte(protocol.FlagUserPresent | protocol.FlagUserVerified | protocol.FlagAttestedCredentialData)
	authData := a.assertionAuthData(a.rpID, flags, 0)
	authData = append(authData, a.aaguid...)
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(a.credentialID)))
	authData = append(authData, length[:]...)
	authData = append(authData, a.credentialID...)
	authData = append(authData, coseKey...)
	return authData
}

func (a *softwareAuthenticator) assertionAuthData(rpID string, flags byte, counter uint32) []byte {
	hash := sha256.Sum256([]byte(rpID))
	authData := make([]byte, 0, 37)
	authData = append(authData, hash[:]...)
	authData = append(authData, flags)
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], counter)
	authData = append(authData, count[:]...)
	return authData
}

func TestSoftwareAuthenticatorAndPasskeyConversion(t *testing.T) {
	wa := newPasskeyWebAuthn(AuthDeps{
		PasskeyRPID:    testPasskeyRPID,
		PasskeyOrigins: []string{testOrigin},
	})
	if wa == nil {
		t.Fatal("passkeys are not configured")
	}
	user := passkeyUser{account: store.User{ID: uuid.NewString(), Username: "admin"}}
	authenticator := newSoftwareAuthenticator(testOrigin)

	creation, session, err := wa.BeginRegistration(user,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification:   protocol.VerificationRequired,
			ResidentKey:        protocol.ResidentKeyRequirementPreferred,
			RequireResidentKey: protocol.ResidentKeyNotRequired(),
		}))
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	options, err := json.Marshal(creation)
	if err != nil {
		t.Fatal(err)
	}
	registration := authenticator.register(t, options)
	parsed, err := protocol.ParseCredentialCreationResponseBytes(registration)
	if err != nil {
		t.Fatalf("parse registration: %v", err)
	}
	credential, err := wa.CreateCredential(user, *session, parsed)
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	if len(credential.ID) == 0 || len(credential.PublicKey) == 0 || !credential.Flags.UserVerified {
		t.Fatalf("credential = %+v", credential)
	}

	stored := store.Passkey{
		CredentialID:   credential.ID,
		PublicKey:      credential.PublicKey,
		AAGUID:         credential.Authenticator.AAGUID,
		SignCount:      credential.Authenticator.SignCount,
		Transports:     []string{"internal"},
		BackupEligible: credential.Flags.BackupEligible,
		BackupState:    credential.Flags.BackupState,
	}
	loginUser := passkeyUser{
		account:     user.account,
		credentials: []webauthn.Credential{passkeyCredential(stored)},
	}
	assertion, loginSession, err := wa.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	loginOptions, err := json.Marshal(assertion)
	if err != nil {
		t.Fatal(err)
	}
	response := authenticator.login(t, loginOptions)
	parsedResponse, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		t.Fatalf("parse assertion: %v", err)
	}
	handler := func(_, userHandle []byte) (webauthn.User, error) {
		if string(userHandle) != user.account.ID {
			return nil, errors.New("unknown user")
		}
		return loginUser, nil
	}
	_, validated, err := wa.ValidatePasskeyLogin(handler, *loginSession, parsedResponse)
	if err != nil {
		t.Fatalf("validate login: %v", err)
	}
	if validated.Authenticator.SignCount != 1 || validated.Authenticator.CloneWarning {
		t.Errorf("validated credential = %+v", validated.Authenticator)
	}
}

func TestPasskeyRegistrationAndLogin(t *testing.T) {
	panel := newPasskeyTestPanel(t, testPasskeyRPID)
	authenticator := newSoftwareAuthenticator(testOrigin)

	entry := panel.registerPasskey(t, panel.client, authenticator, "laptop")
	if entry.ID == "" || entry.Name != "laptop" || entry.CreatedAt.IsZero() || entry.LastUsedAt != nil {
		t.Errorf("entry = %+v", entry)
	}
	passkeys, listRaw := panel.listPasskeys(t, panel.client)
	if !passkeys.Available || len(passkeys.Passkeys) != 1 || passkeys.Passkeys[0].ID != entry.ID {
		t.Errorf("passkeys = %+v", passkeys)
	}
	for _, secret := range []string{
		base64.RawURLEncoding.EncodeToString(authenticator.credentialID),
		base64.StdEncoding.EncodeToString(authenticator.credentialID),
		"publicKey",
	} {
		if bytes.Contains(listRaw, []byte(secret)) {
			t.Errorf("the passkey list leaks %q", secret)
		}
	}

	login := panel.newClient(t)
	ceremonyID, options := panel.beginLogin(t, login)
	assertion := authenticator.login(t, options)
	response := panel.finishLogin(t, login, ceremonyID, assertion)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("login finish status = %d", response.StatusCode)
	}
	if stage := panel.stage(t, login); stage != store.StageFull {
		t.Errorf("stage = %q, want full", stage)
	}
	if browser := panel.cookieValueNamed(t, login, BrowserCookieName); browser == "" {
		t.Error("the passkey login did not set the browser marker")
	}

	passkeys, _ = panel.listPasskeys(t, panel.client)
	if len(passkeys.Passkeys) != 1 || passkeys.Passkeys[0].LastUsedAt == nil {
		t.Errorf("passkeys after login = %+v", passkeys.Passkeys)
	}

	entries, err := panel.store.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	actions := make(map[string]bool)
	for _, entry := range entries {
		actions[entry.Action] = true
	}
	for _, action := range []string{"passkey_added", "login_passkey"} {
		if !actions[action] {
			t.Errorf("audit action %q missing", action)
		}
	}
	assertNoSecretsInAudit(t, entries,
		base64.RawURLEncoding.EncodeToString(authenticator.credentialID))
}

func TestPasskeyLoginFailuresAreUniform(t *testing.T) {
	panel := newPasskeyTestPanel(t, testPasskeyRPID)
	authenticator := newSoftwareAuthenticator(testOrigin)
	entry := panel.registerPasskey(t, panel.client, authenticator, "laptop")

	cases := []struct {
		name   string
		assert func(*testing.T, []byte) []byte
	}{
		{
			name: "wrong origin",
			assert: func(t *testing.T, options []byte) []byte {
				return authenticator.assertion(t, options, "https://evil.example.com", authenticator.rpID, true, authenticator.signCount+1)
			},
		},
		{
			name: "wrong relying party",
			assert: func(t *testing.T, options []byte) []byte {
				return authenticator.assertion(t, options, authenticator.origin, "evil.example.com", true, authenticator.signCount+1)
			},
		},
		{
			name: "missing user verification",
			assert: func(t *testing.T, options []byte) []byte {
				return authenticator.assertion(t, options, authenticator.origin, authenticator.rpID, false, authenticator.signCount+1)
			},
		},
		{
			name: "unknown credential",
			assert: func(t *testing.T, options []byte) []byte {
				original := authenticator.credentialID
				authenticator.credentialID = append([]byte{}, original...)
				authenticator.credentialID[0] ^= 0xff
				defer func() { authenticator.credentialID = original }()
				return authenticator.assertion(t, options, authenticator.origin, authenticator.rpID, true, authenticator.signCount+1)
			},
		},
		{
			name: "unknown user",
			assert: func(t *testing.T, options []byte) []byte {
				original := authenticator.userHandle
				authenticator.userHandle = []byte(uuid.NewString())
				defer func() { authenticator.userHandle = original }()
				return authenticator.assertion(t, options, authenticator.origin, authenticator.rpID, true, authenticator.signCount+1)
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			login := panel.newClient(t)
			ceremonyID, options := panel.beginLogin(t, login)
			response := panel.finishLogin(t, login, ceremonyID, test.assert(t, options))
			if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "invalid_credentials" {
				t.Fatalf("status = %d, want 401 invalid_credentials", response.StatusCode)
			}
			if stage := panel.stage(t, login); stage != "" {
				t.Errorf("failed login got a session: %q", stage)
			}
		})
	}

	unknown := panel.newClient(t)
	response := panel.finishLogin(t, unknown, uuid.NewString(), []byte(`{}`))
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "invalid_credentials" {
		t.Errorf("unknown ceremony = %d, want 401", response.StatusCode)
	}

	passkeys, _ := panel.listPasskeys(t, panel.client)
	if len(passkeys.Passkeys) != 1 || passkeys.Passkeys[0].ID != entry.ID {
		t.Errorf("a failed login removed the passkey: %+v", passkeys.Passkeys)
	}
}

func TestPasskeyCounterRegressionDisablesTheCredential(t *testing.T) {
	panel := newPasskeyTestPanel(t, testPasskeyRPID)
	authenticator := newSoftwareAuthenticator(testOrigin)
	panel.registerPasskey(t, panel.client, authenticator, "laptop")

	first := panel.newClient(t)
	ceremonyID, options := panel.beginLogin(t, first)
	if response := panel.finishLogin(t, first, ceremonyID, authenticator.login(t, options)); response.StatusCode != http.StatusNoContent {
		t.Fatalf("first login status = %d", response.StatusCode)
	}

	// The authenticator reports the same counter again: a cloned credential.
	replay := panel.newClient(t)
	ceremonyID, options = panel.beginLogin(t, replay)
	assertion := authenticator.assertion(t, options, authenticator.origin, authenticator.rpID, true, authenticator.signCount)
	response := panel.finishLogin(t, replay, ceremonyID, assertion)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "invalid_credentials" {
		t.Fatalf("clone status = %d, want 401", response.StatusCode)
	}

	passkeys, _ := panel.listPasskeys(t, panel.client)
	if len(passkeys.Passkeys) != 0 {
		t.Errorf("the cloned passkey is still listed: %+v", passkeys.Passkeys)
	}
	entries, err := panel.store.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range entries {
		if event.Action == "passkey_clone_suspected" {
			found = true
		}
	}
	if !found {
		t.Error("the clone suspicion was not audited")
	}

	// The credential is gone, so even a valid assertion fails.
	login := panel.newClient(t)
	ceremonyID, options = panel.beginLogin(t, login)
	response = panel.finishLogin(t, login, ceremonyID, authenticator.login(t, options))
	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("deleted passkey was accepted: %d", response.StatusCode)
	}
}

func TestPasskeyCeremonyIsSingleUseAndExpires(t *testing.T) {
	panel := newPasskeyTestPanel(t, testPasskeyRPID)
	authenticator := newSoftwareAuthenticator(testOrigin)
	panel.registerPasskey(t, panel.client, authenticator, "laptop")

	client := panel.newClient(t)
	ceremonyID, options := panel.beginLogin(t, client)
	assertion := authenticator.login(t, options)
	if response := panel.finishLogin(t, client, ceremonyID, assertion); response.StatusCode != http.StatusNoContent {
		t.Fatalf("first finish status = %d", response.StatusCode)
	}
	replay := panel.newClient(t)
	if response := panel.finishLogin(t, replay, ceremonyID, assertion); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("replayed ceremony status = %d, want 401", response.StatusCode)
	}

	expiredID, err := panel.store.CreateWebAuthnCeremony(context.Background(), "", "login",
		[]byte(`{}`), time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("create expired ceremony: %v", err)
	}
	if response := panel.finishLogin(t, replay, expiredID, []byte(`{}`)); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("expired ceremony status = %d, want 401", response.StatusCode)
	}
}

func TestPasskeyUserLockoutIsRespected(t *testing.T) {
	panel := newPasskeyTestPanel(t, testPasskeyRPID)
	authenticator := newSoftwareAuthenticator(testOrigin)
	panel.registerPasskey(t, panel.client, authenticator, "laptop")

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := panel.store.RecordFailure(ctx, panel.user.ID, 5, time.Minute); err != nil {
			t.Fatalf("record failure: %v", err)
		}
	}
	client := panel.newClient(t)
	ceremonyID, options := panel.beginLogin(t, client)
	response := panel.finishLogin(t, client, ceremonyID, authenticator.login(t, options))
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "invalid_credentials" {
		t.Fatalf("locked login = %d, want 401", response.StatusCode)
	}

	panel.clock.Advance(2 * time.Minute)
	ceremonyID, options = panel.beginLogin(t, client)
	if response := panel.finishLogin(t, client, ceremonyID, authenticator.login(t, options)); response.StatusCode != http.StatusNoContent {
		t.Errorf("login after the lock = %d, want 204", response.StatusCode)
	}
}

func TestPasskeyLimitAndDeletion(t *testing.T) {
	panel := newPasskeyTestPanel(t, testPasskeyRPID)
	for i := 0; i < store.MaxPasskeys; i++ {
		authenticator := newSoftwareAuthenticator(testOrigin)
		panel.registerPasskey(t, panel.client, authenticator, fmt.Sprintf("passkey-%d", i))
	}
	response := panel.post(t, panel.client, "/api/v1/auth/passkeys/register/begin", map[string]string{
		"password": testPassword,
	})
	if response.StatusCode != http.StatusConflict || errorCode(t, response) != "passkey_limit" {
		t.Fatalf("eleventh begin = %d, want 409 passkey_limit", response.StatusCode)
	}

	passkeys, _ := panel.listPasskeys(t, panel.client)
	if len(passkeys.Passkeys) != store.MaxPasskeys {
		t.Fatalf("passkeys = %d, want %d", len(passkeys.Passkeys), store.MaxPasskeys)
	}
	response = panel.do(t, panel.client, http.MethodDelete,
		"/api/v1/auth/passkeys/"+passkeys.Passkeys[0].ID, nil)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", response.StatusCode)
	}
	passkeys, _ = panel.listPasskeys(t, panel.client)
	if len(passkeys.Passkeys) != store.MaxPasskeys-1 {
		t.Errorf("passkeys after delete = %d", len(passkeys.Passkeys))
	}
	entries, err := panel.store.ListAudit(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range entries {
		if event.Action == "passkey_removed" {
			found = true
		}
	}
	if !found {
		t.Error("the removal was not audited")
	}
}

func TestPasskeysAreUnavailableWithoutADNSName(t *testing.T) {
	panel := newPasskeyTestPanel(t, "")
	passkeys, _ := panel.listPasskeys(t, panel.client)
	if passkeys.Available {
		t.Error("passkeys are available without a relying-party DNS name")
	}
	response := panel.post(t, panel.client, "/api/v1/auth/passkeys/register/begin", map[string]string{
		"password": testPassword,
	})
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, response) != "passkeys_unavailable" {
		t.Errorf("register begin = %d, want 503", response.StatusCode)
	}
	login := panel.newClient(t)
	response = panel.post(t, login, "/api/v1/auth/passkey/login/begin", nil)
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, response) != "passkeys_unavailable" {
		t.Errorf("login begin = %d, want 503", response.StatusCode)
	}
}

func TestPasskeyRegistrationRequiresAFullSession(t *testing.T) {
	panel := newPasskeyTestPanel(t, testPasskeyRPID)
	anonymous := panel.newClient(t)
	for _, request := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/v1/auth/passkeys/register/begin", map[string]string{"password": testPassword}},
		{http.MethodGet, "/api/v1/auth/passkeys", nil},
		{http.MethodDelete, "/api/v1/auth/passkeys/" + uuid.NewString(), nil},
	} {
		response := panel.do(t, anonymous, request.method, request.path, request.body)
		if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "session_required" {
			t.Errorf("%s %s = %d, want 401 session_required", request.method, request.path, response.StatusCode)
		}
	}
}

func TestPasskeyConfigDerivation(t *testing.T) {
	cfg := config.Config{
		PublicHostnames: []string{"192.0.2.10", "panel.example.com:8443"},
	}
	if got := cfg.PasskeyRPID(); got != "panel.example.com" {
		t.Errorf("PasskeyRPID() = %q", got)
	}
	origins := cfg.PasskeyOrigins()
	if len(origins) == 0 {
		t.Error("PasskeyOrigins() is empty")
	}
	found := false
	for _, origin := range origins {
		if origin == "https://panel.example.com:8443" {
			found = true
		}
	}
	if !found {
		t.Errorf("origins = %v, want the panel port", origins)
	}

	withPort := config.Config{PublicHostnames: []string{"panel.example.com"}, PanelPort: "9443"}
	origins = withPort.PasskeyOrigins()
	found = false
	for _, origin := range origins {
		if origin == "https://panel.example.com:9443" {
			found = true
		}
	}
	if !found {
		t.Errorf("origins with a panel port = %v, want the port", origins)
	}

	ipOnly := config.Config{PublicHostnames: []string{"192.0.2.10"}}
	if got := ipOnly.PasskeyRPID(); got != "" {
		t.Errorf("PasskeyRPID() for IPs = %q, want empty", got)
	}
	explicit := config.Config{AllowedOrigins: []string{testOrigin}}
	if origins := explicit.PasskeyOrigins(); len(origins) != 1 || origins[0] != testOrigin {
		t.Errorf("explicit origins = %v", origins)
	}
}

func mustParseURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	return parsed
}
