package store

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func alertTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	url := os.Getenv("TALAIA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TALAIA_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	return s, ctx
}

func TestAlertStorageLifecycle(t *testing.T) {
	s, ctx := alertTestStore(t)
	rules, err := s.ListAlertRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var builtin AlertRule
	for _, rule := range rules {
		if rule.Type == "server_offline" {
			builtin = rule
			break
		}
	}
	if builtin.ID == "" || !builtin.Builtin {
		t.Fatal("default server_offline rule missing")
	}
	if err := s.DeleteAlertRule(ctx, builtin.ID); err == nil {
		t.Fatal("deleted a builtin alert rule")
	}

	fingerprint := "alerts-test-" + time.Now().Format("150405.000000000")
	first, created, err := s.OpenAlert(ctx, Alert{RuleID: &builtin.ID, RuleType: builtin.Type, Severity: "critical", Fingerprint: fingerprint, SubjectKind: "server"})
	if err != nil || !created {
		t.Fatalf("first open alert = created %v, err %v", created, err)
	}
	second, created, err := s.OpenAlert(ctx, Alert{RuleID: &builtin.ID, RuleType: builtin.Type, Severity: "critical", Fingerprint: fingerprint, SubjectKind: "server"})
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("duplicate open alert = %+v, created %v, err %v", second, created, err)
	}
	if err := s.ResolveAlert(ctx, first.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.OpenAlert(ctx, Alert{RuleID: &builtin.ID, RuleType: builtin.Type, Severity: "critical", Fingerprint: fingerprint, SubjectKind: "server"}); err != nil || !created {
		t.Fatalf("reopened alert created %v, err %v", created, err)
	}
	if _, err := s.CreateSilence(ctx, Silence{ScopeType: "organization", EndsAt: time.Now().Add(31 * 24 * time.Hour)}); err == nil {
		t.Fatal("accepted silence longer than 30 days")
	}
}

func TestChannelSecretAccessAndConcurrentDeliveryClaims(t *testing.T) {
	s, ctx := alertTestStore(t)
	channel, err := s.CreateNotificationChannel(ctx, NotificationChannel{Name: "test-channel", Type: "webhook", Enabled: true, Language: "en", Config: []byte(`{}`)}, []byte("opaque-test-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNotificationChannel(ctx, channel.ID)
	if err != nil || !got.HasSecret {
		t.Fatalf("channel read = %+v, err %v", got, err)
	}
	secret, err := s.GetChannelSecret(ctx, channel.ID)
	if err != nil || string(secret) != "opaque-test-bytes" {
		t.Fatalf("secret read = %q, err %v", secret, err)
	}
	delivery, err := s.EnqueueDelivery(ctx, NotificationDelivery{ChannelID: &channel.ID, Event: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	claims := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, e := s.ClaimDue(ctx, time.Now().Add(time.Second), 10)
			if e != nil {
				t.Errorf("claim due: %v", e)
				return
			}
			count := 0
			for _, row := range rows {
				if row.ID == delivery.ID {
					count++
				}
			}
			claims <- count
		}()
	}
	wg.Wait()
	close(claims)
	count := 0
	for n := range claims {
		count += n
	}
	if count != 1 {
		t.Fatalf("delivery claimed %d times, want once", count)
	}
}

func TestChannelStatusAndGetAlert(t *testing.T) {
	s, ctx := alertTestStore(t)
	channel, err := s.CreateNotificationChannel(ctx, NotificationChannel{Name: "status-channel", Type: "webhook", Enabled: true, Language: "en", Config: []byte(`{}`)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := s.SetChannelStatus(ctx, channel.ID, false, "boom", now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetNotificationChannel(ctx, channel.ID)
	if got.LastError != "boom" || got.LastOKAt != nil {
		t.Fatalf("after failure: %+v", got)
	}
	if err := s.SetChannelStatus(ctx, channel.ID, true, "", now); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetNotificationChannel(ctx, channel.ID)
	if got.LastError != "" || got.LastOKAt == nil {
		t.Fatalf("after success: %+v", got)
	}
	if err := s.SetChannelStatus(ctx, "00000000-0000-0000-0000-000000000000", true, "", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown channel error = %v", err)
	}
	if _, err := s.GetAlert(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown alert error = %v", err)
	}
}

func TestPurgeAlertHistoryKeepsOpenAlertsAndPendingDeliveries(t *testing.T) {
	s, ctx := alertTestStore(t)
	old := time.Now().Add(-400 * 24 * time.Hour)
	if _, err := s.pool.Exec(ctx, `INSERT INTO alert(rule_type,severity,fingerprint,subject_kind,state,started_at,resolved_at) VALUES('disk_high','warning','purge-old','server','resolved',$1,$1)`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO alert(rule_type,severity,fingerprint,subject_kind,state,started_at) VALUES('disk_high','warning','purge-open','server','firing',$1)`, old); err != nil {
		t.Fatal(err)
	}
	channel, err := s.CreateNotificationChannel(ctx, NotificationChannel{Name: "purge-" + time.Now().Format("150405.000000000"), Type: "webhook", Enabled: true, Language: "en"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DeleteNotificationChannel(context.Background(), channel.ID) })
	for _, status := range []string{"sent", "failed", "pending"} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO notification_delivery(channel_id,event,status,created_at) VALUES($1,'test',$2,$3)`, channel.ID, status, old); err != nil {
			t.Fatal(err)
		}
	}
	alerts, deliveries, err := s.PurgeAlertHistory(ctx, time.Now().Add(-180*24*time.Hour), time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if alerts < 1 || deliveries != 2 {
		t.Fatalf("purged %d alerts and %d deliveries, want at least 1 and exactly 2", alerts, deliveries)
	}
	if _, err := s.GetAlertByFingerprint(ctx, "purge-open"); err != nil {
		t.Fatalf("the open incident was purged: %v", err)
	}
	if _, err := s.GetAlertByFingerprint(ctx, "purge-old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old resolved incident was kept: %v", err)
	}
	var pending int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_delivery WHERE channel_id=$1 AND status='pending'`, channel.ID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("pending deliveries = %d (err %v), want 1", pending, err)
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM alert WHERE fingerprint='purge-open'`)
}
