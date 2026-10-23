package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

func createInviteAPI(t *testing.T, panel *testPanel, username, role string) inviteCreatedResponse {
	t.Helper()
	response := panel.post(t, "/api/v1/users/invites", map[string]any{
		"username": username,
		"email":    username + "@example.com",
		"role":     role,
		"scope":    map[string]any{"type": string(rbac.ScopeOrganization)},
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create invite status = %d, want 201 (body %s)", response.StatusCode, response.Body)
	}
	var invite inviteCreatedResponse
	decodeBody(t, response, &invite)
	return invite
}

func TestInviteFlowAndMFA(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	invite := createInviteAPI(t, panel, "invitee", string(rbac.RoleViewer))
	if invite.Token == "" || invite.ID == "" || invite.LinkPath != "/invite#"+invite.Token {
		t.Fatalf("created invite = %+v", invite)
	}
	if len(invite.Token) < 40 {
		t.Fatalf("token is too short: %d", len(invite.Token))
	}

	pending := panel.get(t, "/api/v1/users/invites")
	raw, err := io.ReadAll(pending.Body)
	if err != nil {
		t.Fatalf("read invites: %v", err)
	}
	if strings.Contains(string(raw), invite.Token) {
		t.Fatal("the pending invite list contains the clear token")
	}
	var listed invitesResponse
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatalf("decode invites: %v", err)
	}
	found := false
	for _, entry := range listed.Invites {
		if entry.ID == invite.ID {
			found = true
			if entry.Username != "invitee" {
				t.Fatalf("invite entry = %+v", entry)
			}
		}
	}
	if !found {
		t.Fatal("created invite is not in the pending list")
	}

	info := panel.do(t, panel.rawClient(), http.MethodGet, "/api/v1/auth/invite/"+invite.Token, nil)
	if info.StatusCode != http.StatusOK {
		t.Fatalf("invite info status = %d, want 200", info.StatusCode)
	}
	var infoBody inviteInfoResponse
	decodeBody(t, info, &infoBody)
	if infoBody.Username != "invitee" || infoBody.ExpiresAt.IsZero() {
		t.Fatalf("invite info = %+v", infoBody)
	}

	client := panel.newClient(t)
	if response := panel.do(t, client, http.MethodPost, "/api/v1/auth/invite/"+invite.Token+"/accept",
		map[string]string{"password": "short"}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("weak password status = %d, want 400", response.StatusCode)
	}
	if response := panel.do(t, panel.rawClient(), http.MethodGet, "/api/v1/auth/invite/"+invite.Token, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("weak accept consumed the invite: %d", response.StatusCode)
	}

	accepted := panel.do(t, client, http.MethodPost, "/api/v1/auth/invite/"+invite.Token+"/accept",
		map[string]string{"password": testPassword})
	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("accept status = %d, want 200 (body %s)", accepted.StatusCode, accepted.Body)
	}
	var setup totpSetupResponse
	decodeBody(t, accepted, &setup)
	if setup.Secret == "" || !strings.HasPrefix(setup.URI, "otpauth://") {
		t.Fatalf("setup = %+v", setup)
	}
	if stage := panel.stage(t, client); stage != store.StageMFAPending {
		t.Fatalf("stage after accept = %q, want %q", stage, store.StageMFAPending)
	}
	if response := panel.do(t, client, http.MethodGet, "/api/v1/servers", nil); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pending invitee reached protected API: %d", response.StatusCode)
	}

	user, err := panel.store.GetUserByUsername(context.Background(), "invitee")
	if err != nil {
		t.Fatalf("get invitee: %v", err)
	}
	bindings, err := panel.store.ListRoleBindingsForUser(context.Background(), user.ID)
	if err != nil || len(bindings) != 1 || bindings[0].Role != string(rbac.RoleViewer) ||
		bindings[0].ScopeType != store.ScopeOrganization {
		t.Fatalf("invite bindings = %+v, %v", bindings, err)
	}

	secret, err := auth.DecodeBase32(setup.Secret)
	if err != nil {
		t.Fatalf("decode setup secret: %v", err)
	}
	confirmed := panel.do(t, client, http.MethodPost, "/api/v1/auth/totp/confirm",
		map[string]string{"code": panel.code(secret)})
	if confirmed.StatusCode != http.StatusOK {
		t.Fatalf("totp confirm status = %d, want 200 (body %s)", confirmed.StatusCode, confirmed.Body)
	}
	if stage := panel.stage(t, client); stage != store.StageFull {
		t.Fatalf("stage after confirm = %q, want full", stage)
	}
	if response := panel.do(t, client, http.MethodGet, "/api/v1/servers", nil); response.StatusCode != http.StatusOK {
		t.Fatalf("full invitee servers status = %d, want 200", response.StatusCode)
	}

	// Used and unknown tokens have the same answer as each other and as expired
	// tokens.
	used := panel.do(t, panel.rawClient(), http.MethodGet, "/api/v1/auth/invite/"+invite.Token, nil)
	if used.StatusCode != http.StatusNotFound || errorCode(t, used) != "invalid_invite" {
		t.Fatalf("used token = %d %q", used.StatusCode, errorCode(t, used))
	}
	unknown := panel.do(t, panel.rawClient(), http.MethodGet, "/api/v1/auth/invite/unknown-token", nil)
	if unknown.StatusCode != http.StatusNotFound || errorCode(t, unknown) != "invalid_invite" {
		t.Fatalf("unknown token = %d %q", unknown.StatusCode, errorCode(t, unknown))
	}
	expiredToken := auth.NewSessionToken()
	if _, err := panel.store.CreateInvite(context.Background(), store.Invite{
		TokenHash: auth.HashToken(expiredToken), Username: "expired-user",
		Role: string(rbac.RoleViewer), ScopeType: store.ScopeOrganization,
		ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("create expired invite: %v", err)
	}
	expired := panel.do(t, panel.rawClient(), http.MethodGet, "/api/v1/auth/invite/"+expiredToken, nil)
	if expired.StatusCode != http.StatusNotFound || errorCode(t, expired) != "invalid_invite" {
		t.Fatalf("expired token = %d %q", expired.StatusCode, errorCode(t, expired))
	}
	usedAccept := panel.do(t, panel.newClient(t), http.MethodPost, "/api/v1/auth/invite/"+invite.Token+"/accept",
		map[string]string{"password": testPassword})
	if usedAccept.StatusCode != http.StatusNotFound || errorCode(t, usedAccept) != "invalid_invite" {
		t.Fatalf("used accept = %d %q", usedAccept.StatusCode, errorCode(t, usedAccept))
	}

	// The invited user account was created without a TOTP secret before setup.
	if !user.TOTPEnabledAt.IsZero() {
		t.Fatal("invite created an already enabled TOTP")
	}

	entries, err := panel.store.ListAudit(context.Background(), 200)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	actions := map[string]bool{}
	for _, entry := range entries {
		actions[entry.Action] = true
	}
	if !actions["invite_create"] || !actions["invite_accept"] {
		t.Fatalf("invite audit actions = %v", actions)
	}

	revoked := createInviteAPI(t, panel, "revoked-invitee", string(rbac.RoleViewer))
	if response := panel.do(t, panel.client, http.MethodDelete,
		"/api/v1/users/invites/"+revoked.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", response.StatusCode)
	}
	if response := panel.do(t, panel.rawClient(), http.MethodGet,
		"/api/v1/auth/invite/"+revoked.Token, nil); response.StatusCode != http.StatusNotFound {
		t.Fatalf("revoked token status = %d, want 404", response.StatusCode)
	}
}
