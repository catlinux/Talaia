package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

const testPIN = "482915"

// cookieValueNamed returns a cookie of a client, or "" when it is absent.
func (p *testPanel) cookieValueNamed(t *testing.T, client *http.Client, name string) string {
	t.Helper()

	parsed, err := url.Parse(p.server.URL)
	if err != nil {
		t.Fatalf("parse the server URL: %v", err)
	}
	if client.Jar == nil {
		return ""
	}
	for _, cookie := range client.Jar.Cookies(parsed) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

// clientWithCookies returns a fresh client that carries exactly these cookies.
func (p *testPanel) clientWithCookies(t *testing.T, cookies ...*http.Cookie) *http.Client {
	t.Helper()

	client := p.newClient(t)
	parsed, err := url.Parse(p.server.URL)
	if err != nil {
		t.Fatalf("parse the server URL: %v", err)
	}
	for _, cookie := range cookies {
		if cookie.Path == "" {
			cookie.Path = "/"
		}
		cookie.Secure = true
	}
	client.Jar.SetCookies(parsed, cookies)
	return client
}

func (p *testPanel) pinLogin(t *testing.T, client *http.Client, pin string) *http.Response {
	t.Helper()
	return p.do(t, client, http.MethodPost, "/api/v1/auth/pin/login", map[string]string{"pin": pin})
}

func (p *testPanel) createTrustedDevice(t *testing.T, client *http.Client, name string) *http.Response {
	t.Helper()
	return p.do(t, client, http.MethodPost, "/api/v1/auth/devices", map[string]string{
		"password": testPassword,
		"pin":      testPIN,
		"name":     name,
	})
}

func (p *testPanel) completeTOTP(t *testing.T, client *http.Client, secret []byte) {
	t.Helper()

	response := p.do(t, client, http.MethodPost, "/api/v1/auth/totp",
		map[string]string{"code": p.code(secret)})
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("totp status = %d, want 204", response.StatusCode)
	}
	p.clock.Advance(30 * time.Second)
}

func readResponseBody(t *testing.T, response *http.Response) []byte {
	t.Helper()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read the response body: %v", err)
	}
	return body
}

func responseCookie(response *http.Response, name string) *http.Cookie {
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func decodeDeviceResponse(t *testing.T, body []byte) trustedDeviceResponse {
	t.Helper()

	var device trustedDeviceResponse
	if err := json.Unmarshal(body, &device); err != nil {
		t.Fatalf("decode the device: %v", err)
	}
	return device
}

func (p *testPanel) listDevices(t *testing.T, client *http.Client) trustedDevicesResponse {
	t.Helper()

	response := p.do(t, client, http.MethodGet, "/api/v1/auth/devices", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list devices status = %d, want 200", response.StatusCode)
	}
	var body trustedDevicesResponse
	decodeBody(t, response, &body)
	return body
}

func assertNoSecretsInAudit(t *testing.T, entries []store.AuditEntry, secrets ...string) {
	t.Helper()

	for _, entry := range entries {
		details := fmt.Sprint(entry.Details)
		for _, secret := range secrets {
			if secret != "" && strings.Contains(details, secret) {
				t.Errorf("audit action %q leaks a secret: %v", entry.Action, entry.Details)
			}
		}
	}
}

func TestTrustedDeviceCreationAndPinLogin(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	_, _ = panel.enableTOTP(t, "admin", testPassword)

	response := panel.createTrustedDevice(t, panel.client, "laptop")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create device status = %d, want 201", response.StatusCode)
	}
	raw := readResponseBody(t, response)
	device := decodeDeviceResponse(t, raw)
	if device.ID == "" || device.Name != "laptop" || !device.Current || device.CreatedAt.IsZero() {
		t.Errorf("device = %+v", device)
	}
	if device.LastUsedAt != nil {
		t.Errorf("a new device has last_used_at: %+v", device)
	}

	deviceToken := panel.cookieValueNamed(t, panel.client, DeviceCookieName)
	browserToken := panel.cookieValueNamed(t, panel.client, BrowserCookieName)
	if deviceToken == "" || browserToken == "" {
		t.Fatalf("cookies: device %q, browser %q", deviceToken, browserToken)
	}
	for _, secret := range []string{testPIN, deviceToken, browserToken} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("the create response leaks %q", secret)
		}
	}

	deviceCookie := responseCookie(response, DeviceCookieName)
	if deviceCookie == nil {
		t.Fatal("the create response did not set the device cookie")
	}
	if deviceCookie.MaxAge != 180*24*60*60 || !deviceCookie.Secure || !deviceCookie.HttpOnly ||
		deviceCookie.SameSite != http.SameSiteStrictMode || deviceCookie.Path != "/" {
		t.Errorf("device cookie = %+v", deviceCookie)
	}
	browserCookie := responseCookie(response, BrowserCookieName)
	if browserCookie == nil {
		t.Fatal("the create response did not set the browser cookie")
	}
	if browserCookie.MaxAge != 0 || !browserCookie.Secure || !browserCookie.HttpOnly ||
		browserCookie.SameSite != http.SameSiteStrictMode || browserCookie.Path != "/" {
		t.Errorf("browser cookie = %+v", browserCookie)
	}

	listResponse := panel.do(t, panel.client, http.MethodGet, "/api/v1/auth/devices", nil)
	listRaw := readResponseBody(t, listResponse)
	for _, secret := range []string{testPIN, deviceToken, browserToken} {
		if bytes.Contains(listRaw, []byte(secret)) {
			t.Errorf("the list response leaks %q", secret)
		}
	}
	var list trustedDevicesResponse
	if err := json.Unmarshal(listRaw, &list); err != nil {
		t.Fatalf("decode the list: %v", err)
	}
	if len(list.Devices) != 1 || list.Devices[0].ID != device.ID || !list.Devices[0].Current {
		t.Errorf("devices = %+v", list.Devices)
	}

	panel.logout(t, panel.client)
	response = panel.pinLogin(t, panel.client, testPIN)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("pin login status = %d, want 204", response.StatusCode)
	}
	if stage := panel.stage(t, panel.client); stage != store.StageFull {
		t.Errorf("stage = %q, want full", stage)
	}

	list = panel.listDevices(t, panel.client)
	if len(list.Devices) != 1 || list.Devices[0].LastUsedAt == nil {
		t.Errorf("devices after a PIN login = %+v", list.Devices)
	}

	entries, err := panel.store.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	assertNoSecretsInAudit(t, entries, testPIN, deviceToken, browserToken)
}

func TestPinLoginExpiredMFAWindowRequiresTOTP(t *testing.T) {
	panel := newTestPanel(t, withIdleTimeout(48*time.Hour))
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	response := panel.createTrustedDevice(t, panel.client, "laptop")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create device status = %d", response.StatusCode)
	}
	deviceToken := panel.cookieValueNamed(t, panel.client, DeviceCookieName)
	panel.logout(t, panel.client)

	panel.clock.Advance(13 * time.Hour)
	response = panel.pinLogin(t, panel.client, testPIN)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "mfa_required" {
		t.Fatalf("expired pin login = %d, want 401 mfa_required", response.StatusCode)
	}
	if stage := panel.stage(t, panel.client); stage != store.StageMFAPending {
		t.Fatalf("stage = %q, want mfa_pending", stage)
	}

	panel.completeTOTP(t, panel.client, secret)
	if stage := panel.stage(t, panel.client); stage != store.StageFull {
		t.Fatalf("stage after totp = %q, want full", stage)
	}

	device, err := panel.store.GetTrustedDeviceByTokenHash(context.Background(), auth.HashToken(deviceToken))
	if err != nil {
		t.Fatalf("get device after totp: %v", err)
	}
	if device.LastMFAAt.IsZero() || device.LastUsedAt.IsZero() {
		t.Errorf("totp did not refresh the device: %+v", device)
	}
	if device.LastMFAAt.Before(panel.clock.Now().Add(-time.Minute)) {
		t.Errorf("the device mfa time was not refreshed: %s", device.LastMFAAt)
	}
	browserToken := panel.cookieValueNamed(t, panel.client, BrowserCookieName)
	if browserToken == "" || !bytes.Equal(device.BrowserHash, auth.HashToken(browserToken)) {
		t.Error("the browser marker was not bound to the device")
	}

	panel.logout(t, panel.client)
	response = panel.pinLogin(t, panel.client, testPIN)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("pin login after the new totp = %d, want 204", response.StatusCode)
	}
}

func TestPasswordTOTPRefreshesTheTrustedDevice(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	response := panel.createTrustedDevice(t, panel.client, "laptop")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create device status = %d", response.StatusCode)
	}
	deviceToken := panel.cookieValueNamed(t, panel.client, DeviceCookieName)
	panel.logout(t, panel.client)

	if next := panel.login(t, "admin", testPassword); next != "totp" {
		t.Fatalf("next = %q, want totp", next)
	}
	panel.completeTOTP(t, panel.client, secret)

	device, err := panel.store.GetTrustedDeviceByTokenHash(context.Background(), auth.HashToken(deviceToken))
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	browserToken := panel.cookieValueNamed(t, panel.client, BrowserCookieName)
	if browserToken == "" || !bytes.Equal(device.BrowserHash, auth.HashToken(browserToken)) {
		t.Error("the password + TOTP login did not refresh the device marker")
	}

	panel.logout(t, panel.client)
	if response := panel.pinLogin(t, panel.client, testPIN); response.StatusCode != http.StatusNoContent {
		t.Fatalf("pin login after password + TOTP = %d, want 204", response.StatusCode)
	}
}

func TestPinLoginWithoutBrowserMarkerRequiresTOTP(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	response := panel.createTrustedDevice(t, panel.client, "laptop")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create device status = %d", response.StatusCode)
	}
	deviceToken := panel.cookieValueNamed(t, panel.client, DeviceCookieName)
	panel.logout(t, panel.client)

	// A new browser keeps the device cookie but has no session marker.
	fresh := panel.clientWithCookies(t, &http.Cookie{Name: DeviceCookieName, Value: deviceToken})
	response = panel.pinLogin(t, fresh, testPIN)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "mfa_required" {
		t.Fatalf("pin login without a browser marker = %d, want 401 mfa_required", response.StatusCode)
	}
	panel.completeTOTP(t, fresh, secret)
	if stage := panel.stage(t, fresh); stage != store.StageFull {
		t.Fatalf("stage = %q, want full", stage)
	}
	panel.logout(t, fresh)
	response = panel.pinLogin(t, fresh, testPIN)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("pin login after the browser marker was set = %d, want 204", response.StatusCode)
	}
}

func TestFiveWrongPinsDisableTheDevice(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	response := panel.createTrustedDevice(t, panel.client, "laptop")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create device status = %d", response.StatusCode)
	}
	deviceToken := panel.cookieValueNamed(t, panel.client, DeviceCookieName)
	panel.logout(t, panel.client)

	for attempt := 1; attempt <= 5; attempt++ {
		response = panel.pinLogin(t, panel.client, "482916")
		if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "invalid_credentials" {
			t.Fatalf("wrong pin %d = %d, want 401 invalid_credentials", attempt, response.StatusCode)
		}
	}

	// The device is gone, but the user is not locked: password + TOTP still works.
	withoutPIN := panel.clientWithCookies(t, &http.Cookie{Name: DeviceCookieName, Value: deviceToken})
	response = panel.pinLogin(t, withoutPIN, testPIN)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "invalid_credentials" {
		t.Fatalf("disabled device = %d, want 401 invalid_credentials", response.StatusCode)
	}

	if next := panel.login(t, "admin", testPassword); next != "totp" {
		t.Fatalf("next = %q, want totp", next)
	}
	panel.completeTOTP(t, panel.client, secret)
	if devices := panel.listDevices(t, panel.client); len(devices.Devices) != 0 {
		t.Errorf("devices after five wrong PINs = %+v", devices.Devices)
	}
}

func TestUnknownDeviceGetsTheSameAnswer(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	_, _ = panel.enableTOTP(t, "admin", testPassword)

	unknown := panel.clientWithCookies(t,
		&http.Cookie{Name: DeviceCookieName, Value: "a-token-that-was-never-issued"})
	response := panel.pinLogin(t, unknown, testPIN)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "invalid_credentials" {
		t.Fatalf("unknown device = %d, want 401 invalid_credentials", response.StatusCode)
	}
	if stage := panel.stage(t, unknown); stage != "" {
		t.Errorf("unknown device got a session: %q", stage)
	}
}

func TestUserLockoutIsRespectedByPINLogin(t *testing.T) {
	panel := newTestPanel(t)
	user := panel.createUser(t, "admin", testPassword)
	_, _ = panel.enableTOTP(t, "admin", testPassword)
	response := panel.createTrustedDevice(t, panel.client, "laptop")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create device status = %d", response.StatusCode)
	}
	panel.logout(t, panel.client)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := panel.store.RecordFailure(ctx, user.ID, 5, time.Minute); err != nil {
			t.Fatalf("record failure: %v", err)
		}
	}
	response = panel.pinLogin(t, panel.client, testPIN)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "invalid_credentials" {
		t.Fatalf("locked pin login = %d, want 401 invalid_credentials", response.StatusCode)
	}

	panel.clock.Advance(2 * time.Minute)
	response = panel.pinLogin(t, panel.client, testPIN)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("pin login after the lock = %d, want 204", response.StatusCode)
	}
	if stage := panel.stage(t, panel.client); stage != store.StageFull {
		t.Errorf("stage = %q, want full", stage)
	}
}

func TestDeletingTheCurrentDeviceClearsCookies(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	_, _ = panel.enableTOTP(t, "admin", testPassword)
	response := panel.createTrustedDevice(t, panel.client, "laptop")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create device status = %d", response.StatusCode)
	}
	device := decodeDeviceResponse(t, readResponseBody(t, response))
	deviceToken := panel.cookieValueNamed(t, panel.client, DeviceCookieName)
	browserToken := panel.cookieValueNamed(t, panel.client, BrowserCookieName)

	response = panel.do(t, panel.client, http.MethodDelete, "/api/v1/auth/devices/"+device.ID, nil)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete device status = %d, want 204", response.StatusCode)
	}
	for _, name := range []string{DeviceCookieName, BrowserCookieName} {
		cookie := responseCookie(response, name)
		if cookie == nil || cookie.MaxAge >= 0 {
			t.Errorf("cookie %q was not cleared: %+v", name, cookie)
		}
	}
	if devices := panel.listDevices(t, panel.client); len(devices.Devices) != 0 {
		t.Errorf("devices after deleting the current one = %+v", devices.Devices)
	}

	fresh := panel.clientWithCookies(t, &http.Cookie{Name: DeviceCookieName, Value: deviceToken})
	if response := panel.pinLogin(t, fresh, testPIN); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("deleted device was accepted: %d", response.StatusCode)
	}
	if browserToken == "" {
		t.Error("the browser marker was not present before deletion")
	}
}

func TestDeletingAnotherDeviceKeepsTheCurrentCookies(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	_, _ = panel.enableTOTP(t, "admin", testPassword)

	first := panel.createTrustedDevice(t, panel.client, "first")
	firstDevice := decodeDeviceResponse(t, readResponseBody(t, first))
	second := panel.createTrustedDevice(t, panel.client, "second")
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("create second device status = %d", second.StatusCode)
	}
	currentToken := panel.cookieValueNamed(t, panel.client, DeviceCookieName)

	response := panel.do(t, panel.client, http.MethodDelete, "/api/v1/auth/devices/"+firstDevice.ID, nil)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete another device status = %d, want 204", response.StatusCode)
	}
	for _, name := range []string{DeviceCookieName, BrowserCookieName} {
		if cookie := responseCookie(response, name); cookie != nil {
			t.Errorf("deleting another device touched cookie %q: %+v", name, cookie)
		}
	}
	if got := panel.cookieValueNamed(t, panel.client, DeviceCookieName); got != currentToken {
		t.Errorf("the current device cookie changed")
	}
	devices := panel.listDevices(t, panel.client)
	if len(devices.Devices) != 1 || !devices.Devices[0].Current {
		t.Errorf("devices = %+v, want the current one", devices.Devices)
	}
}

func TestTrustedDeviceLimit(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	_, _ = panel.enableTOTP(t, "admin", testPassword)

	for i := 0; i < store.MaxTrustedDevices; i++ {
		response := panel.createTrustedDevice(t, panel.client, fmt.Sprintf("device-%d", i))
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("device %d status = %d, want 201", i, response.StatusCode)
		}
	}
	response := panel.createTrustedDevice(t, panel.client, "one too many")
	if response.StatusCode != http.StatusConflict || errorCode(t, response) != "device_limit" {
		t.Fatalf("eleventh device = %d, want 409 device_limit", response.StatusCode)
	}
	if devices := panel.listDevices(t, panel.client); len(devices.Devices) != store.MaxTrustedDevices {
		t.Errorf("devices = %d, want %d", len(devices.Devices), store.MaxTrustedDevices)
	}
}

func TestPasswordChangeRevokesTrustedDevices(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	response := panel.createTrustedDevice(t, panel.client, "laptop")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create device status = %d", response.StatusCode)
	}
	deviceToken := panel.cookieValueNamed(t, panel.client, DeviceCookieName)

	response = panel.post(t, "/api/v1/auth/password", passwordRequest{
		CurrentPassword: testPassword,
		NewPassword:     "a much better password",
		Code:            panel.code(secret),
	})
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("password change status = %d, want 204", response.StatusCode)
	}
	if devices := panel.listDevices(t, panel.client); len(devices.Devices) != 0 {
		t.Errorf("devices after a password change = %+v", devices.Devices)
	}

	fresh := panel.clientWithCookies(t, &http.Cookie{Name: DeviceCookieName, Value: deviceToken})
	if response := panel.pinLogin(t, fresh, testPIN); response.StatusCode != http.StatusUnauthorized ||
		errorCode(t, response) != "invalid_credentials" {
		t.Errorf("device survived the password change: %d", response.StatusCode)
	}
}

func TestPINLoginIsRateLimited(t *testing.T) {
	panel := newTestPanel(t, withLimiter(NewRateLimiterWithClock(4, time.Minute, time.Now)))
	panel.createUser(t, "admin", testPassword)
	_, _ = panel.enableTOTP(t, "admin", testPassword)
	response := panel.createTrustedDevice(t, panel.client, "laptop")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create device status = %d", response.StatusCode)
	}
	panel.logout(t, panel.client)

	// Login, setup and confirm consumed three attempts; one PIN attempt is left.
	if response := panel.pinLogin(t, panel.client, testPIN); response.StatusCode != http.StatusNoContent {
		t.Fatalf("first pin login status = %d, want 204", response.StatusCode)
	}
	panel.logout(t, panel.client)
	response = panel.pinLogin(t, panel.client, testPIN)
	if response.StatusCode != http.StatusTooManyRequests || errorCode(t, response) != "rate_limited" {
		t.Fatalf("second pin login = %d, want 429 rate_limited", response.StatusCode)
	}
}

func TestDeviceEndpointsRequireAFullSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	_, _ = panel.enableTOTP(t, "admin", testPassword)
	panel.logout(t, panel.client)
	panel.login(t, "admin", testPassword)

	response := panel.do(t, panel.client, http.MethodPost, "/api/v1/auth/devices", map[string]string{
		"password": testPassword,
		"pin":      testPIN,
		"name":     "laptop",
	})
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "mfa_required" {
		t.Errorf("device creation with a pending session = %d, want 401 mfa_required", response.StatusCode)
	}
	response = panel.do(t, panel.client, http.MethodGet, "/api/v1/auth/devices", nil)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "mfa_required" {
		t.Errorf("device list with a pending session = %d, want 401 mfa_required", response.StatusCode)
	}
	response = panel.do(t, panel.client, http.MethodDelete, "/api/v1/auth/devices/unknown", nil)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "mfa_required" {
		t.Errorf("device delete with a pending session = %d, want 401 mfa_required", response.StatusCode)
	}

	raw := panel.rawClient()
	response = panel.do(t, raw, http.MethodGet, "/api/v1/auth/devices", nil)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, response) != "session_required" {
		t.Errorf("device list without a session = %d, want 401 session_required", response.StatusCode)
	}
}

// withIdleTimeout changes the idle window of the panel sessions. The test clock
// runs ahead of the database clock, so a long jump needs a longer window.
func withIdleTimeout(d time.Duration) panelOption {
	return func(options *testPanelOptions) { options.idleTimeout = d }
}
