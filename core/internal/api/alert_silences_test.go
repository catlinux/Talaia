package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

func TestSilenceEntryUsesSnakeCase(t *testing.T) {
	s := store.Silence{ID: "s", ScopeType: "server", Reason: "maintenance"}
	b, err := json.Marshal(silenceEntry(s))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err = json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"scope_type", "server_id", "group_id", "rule_type", "created_by", "created_at", "ends_at"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing %q", key)
		}
	}
}

func TestSilenceValidationVisibilityAndDelete(t *testing.T) {
	p := newTestPanel(t)
	serverID, err := p.store.CreateServer(context.Background(), "silence-server-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	group, err := p.store.CreateGroup(context.Background(), "silence-group-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err = p.store.SetServerGroup(context.Background(), serverID, group.ID); err != nil {
		t.Fatal(err)
	}
	user, effective := alertBinding(t, p, "silence-operator", string(rbac.RoleAppOperator), store.ScopeGroup, "", group.ID, "")
	create := NewSilenceCreateHandler(AlertsDeps{Store: p.store, Audit: p.store, Now: p.clock.Now})
	otherGroup, err := p.store.CreateGroup(context.Background(), "silence-other-group-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	outside := `{"scope_type":"group","group_id":"` + otherGroup.ID + `","duration_minutes":5,"reason":"maintenance"}`
	denied := authorizedRequest(p, user, effective, http.MethodPost, "/silences", outside, "", "", create)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("outside scope silence status=%d", denied.Code)
	}
	for _, body := range []string{
		`{"scope_type":"group","group_id":"` + group.ID + `","duration_minutes":0,"reason":"maint"}`,
		`{"scope_type":"group","group_id":"` + group.ID + `","duration_minutes":43201,"reason":"maint"}`,
		`{"scope_type":"group","group_id":"` + group.ID + `","duration_minutes":5,"reason":" "}`,
		`{"scope_type":"unknown","duration_minutes":5,"reason":"maint"}`,
	} {
		w := authorizedRequest(p, user, effective, http.MethodPost, "/silences", body, "", "", create)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s status=%d body=%s", body, w.Code, w.Body)
		}
	}
	valid := `{"scope_type":"group","group_id":"` + group.ID + `","duration_minutes":15,"reason":"maintenance"}`
	created := authorizedRequest(p, user, effective, http.MethodPost, "/silences", valid, "", "", create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body)
	}
	var entry silenceJSON
	if err = json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.GroupID == nil || *entry.GroupID != group.ID {
		t.Fatalf("silence=%+v", entry)
	}
	list := authorizedRequest(p, user, effective, http.MethodGet, "/silences", "", "", "", NewSilenceListHandler(AlertsDeps{Store: p.store, Now: p.clock.Now}))
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body)
	}
	var rows []silenceJSON
	if err = json.Unmarshal(list.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != entry.ID {
		t.Fatalf("visible silences=%+v", rows)
	}
	remove := authorizedRequest(p, user, effective, http.MethodDelete, "/silences/"+entry.ID, "", "silenceID", entry.ID, NewSilenceDeleteHandler(AlertsDeps{Store: p.store, Audit: p.store, Now: p.clock.Now}))
	if remove.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", remove.Code, remove.Body)
	}
}

func TestSilenceListUsesVisibilityForServerGroups(t *testing.T) {
	p := newTestPanel(t)
	serverID, err := p.store.CreateServer(context.Background(), "silence-visible-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	group, err := p.store.CreateGroup(context.Background(), "silence-visible-group-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err = p.store.SetServerGroup(context.Background(), serverID, group.ID); err != nil {
		t.Fatal(err)
	}
	created, err := p.store.CreateSilence(context.Background(), store.Silence{ScopeType: "server", ScopeServerID: &serverID, Reason: "maintenance", CreatedBy: "admin", EndsAt: p.clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	user, effective := alertBinding(t, p, "silence-group-viewer", string(rbac.RoleViewer), store.ScopeGroup, "", group.ID, "")
	w := authorizedRequest(p, user, effective, http.MethodGet, "/silences", "", "", "", NewSilenceListHandler(AlertsDeps{Store: p.store, Now: p.clock.Now}))
	var rows []silenceJSON
	if w.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body)
	}
	if err = json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != created.ID {
		t.Fatalf("group-visible silences=%+v", rows)
	}
}
