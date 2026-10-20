package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

var testFingerprint = strings.Repeat("ab", 32)

type tokenResponse struct {
	Token         string `json:"token"`
	CAFingerprint string `json:"ca_fingerprint"`
	ExpiresIn     int    `json:"expires_in"`
	Command       string `json:"command"`
}

func TestEnrollmentTokenNeedsAFullSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	body := map[string]string{"name": "vps-web-01"}

	// Anonymous.
	if response := panel.post(t, "/api/v1/servers/enrollment-tokens", body); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous status = %d, want 401", response.StatusCode)
	}

	// Password only: the second factor is still pending.
	panel.login(t, "admin", testPassword)
	if response := panel.post(t, "/api/v1/servers/enrollment-tokens", body); response.StatusCode == http.StatusCreated {
		t.Error("a session without the second factor created a token")
	}
}

func TestEnrollmentTokenCreation(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	response := panel.post(t, "/api/v1/servers/enrollment-tokens", map[string]string{"name": "vps-web-01"})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", response.StatusCode)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var created tokenResponse
	decodeBody(t, response, &created)
	if !strings.HasPrefix(created.Token, "tl_enr_") {
		t.Errorf("token = %q", created.Token)
	}
	if created.CAFingerprint != testFingerprint || created.ExpiresIn != 900 {
		t.Errorf("fingerprint = %q, expires_in = %d", created.CAFingerprint, created.ExpiresIn)
	}
	wantCommand := "curl -fsSk https://192.0.2.10:8443/install.sh | sudo bash -s -- --url https://192.0.2.10:8443 --token " +
		created.Token + " --ca-fingerprint " + testFingerprint
	if created.Command != wantCommand {
		t.Errorf("command = %q, want %q", created.Command, wantCommand)
	}

	// The audit log records who created it, never the token itself.
	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != "enrollment_token_created" {
			continue
		}
		found = true
		if entry.Actor != "admin" || entry.Details["server_name"] != "vps-web-01" {
			t.Errorf("audit entry = %+v", entry)
		}
		if strings.Contains(entry.Actor+strings.Join(stringValues(entry.Details), ""), created.Token) {
			t.Error("the audit log holds the token")
		}
	}
	if !found {
		t.Error("the token creation was not audited")
	}

	// The same name cannot have two tokens in flight.
	again := panel.post(t, "/api/v1/servers/enrollment-tokens", map[string]string{"name": "vps-web-01"})
	if again.StatusCode != http.StatusConflict {
		t.Errorf("second token status = %d, want 409", again.StatusCode)
	}
}

func TestEnrollmentTokenRejectsBadNames(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	for _, name := range []string{"", "   ", "-starts-with-dash", "has space", "semi;colon", "$(id)", strings.Repeat("a", 64)} {
		response := panel.post(t, "/api/v1/servers/enrollment-tokens", map[string]string{"name": name})
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("name %q: status = %d, want 400", name, response.StatusCode)
		}
	}
}

func stringValues(details map[string]any) []string {
	values := make([]string, 0, len(details))
	for _, value := range details {
		if text, ok := value.(string); ok {
			values = append(values, text)
		}
	}
	return values
}
