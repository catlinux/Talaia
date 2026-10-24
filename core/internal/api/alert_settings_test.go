package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

func TestAlertSettingsRequestRequiresExplicitValue(t *testing.T) {
	var missing alertSettingsJSON
	if err := json.Unmarshal([]byte(`{}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.AllowPrivateTargets != nil {
		t.Fatal("missing allow_private_targets should remain distinguishable")
	}
	var disabled alertSettingsJSON
	if err := json.Unmarshal([]byte(`{"allow_private_targets":false}`), &disabled); err != nil {
		t.Fatal(err)
	}
	if disabled.AllowPrivateTargets == nil || *disabled.AllowPrivateTargets {
		t.Fatal("explicit false setting was not retained")
	}
}

func TestAlertSettingsRoundTrip(t *testing.T) {
	p := newTestPanel(t)
	user, effective := alertBinding(t, p, "alert-settings-admin", string(rbac.RoleAdmin), store.ScopeOrganization, "", "", "")
	deps := AlertsDeps{Store: p.store, Audit: p.store}
	update := authorizedRequest(p, user, effective, http.MethodPut, "/settings/alerts", `{"allow_private_targets":true}`, "", "", NewAlertSettingsUpdateHandler(deps))
	if update.Code != http.StatusOK || update.Body.String() != "{\"allow_private_targets\":true}\n" {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body)
	}
	get := authorizedRequest(p, user, effective, http.MethodGet, "/settings/alerts", `{}`, "", "", NewAlertSettingsHandler(deps))
	var got alertSettingsJSON
	if get.Code != http.StatusOK || json.Unmarshal(get.Body.Bytes(), &got) != nil || got.AllowPrivateTargets == nil || !*got.AllowPrivateTargets {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body)
	}
	audit, err := p.store.ListAudit(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range audit {
		if event.Action == "alert_settings_update" {
			found = true
		}
	}
	if !found {
		t.Fatal("alert settings update was not audited")
	}
}
