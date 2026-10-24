package store

import (
	"context"
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
