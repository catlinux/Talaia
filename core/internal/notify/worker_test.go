package notify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

type workerStore struct {
	mu             sync.Mutex
	channel        store.NotificationChannel
	sealed         []byte
	channelErr     error
	deliveries     []store.NotificationDelivery
	claims         int
	claimHook      func()
	sent           []string
	retries        map[string]time.Time
	deliveryErrors map[string]string
	failed         []string
	statuses       []channelStatus
	alert          store.Alert
	server         store.Server
}

type channelStatus struct {
	ok        bool
	lastError string
}

func (s *workerStore) ClaimDue(context.Context, time.Time, int) ([]store.NotificationDelivery, error) {
	s.mu.Lock()
	s.claims++
	ds := append([]store.NotificationDelivery(nil), s.deliveries...)
	hook := s.claimHook
	s.claimHook = nil
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return ds, nil
}
func (s *workerStore) GetNotificationChannel(context.Context, string) (store.NotificationChannel, error) {
	return s.channel, s.channelErr
}
func (s *workerStore) GetChannelSecret(context.Context, string) ([]byte, error) {
	return append([]byte(nil), s.sealed...), nil
}
func (s *workerStore) GetAlert(context.Context, string) (store.Alert, error)   { return s.alert, nil }
func (s *workerStore) GetServer(context.Context, string) (store.Server, error) { return s.server, nil }
func (s *workerStore) SetChannelStatus(_ context.Context, _ string, ok bool, msg string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses = append(s.statuses, channelStatus{ok, msg})
	return nil
}
func (s *workerStore) MarkDeliverySent(_ context.Context, id string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, id)
	return nil
}
func (s *workerStore) MarkDeliveryRetry(_ context.Context, id string, at time.Time, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retries[id] = at
	s.deliveryErrors[id] = msg
	return nil
}
func (s *workerStore) MarkDeliveryFailed(_ context.Context, id, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed = append(s.failed, id)
	s.deliveryErrors[id] = msg
	return nil
}
func (s *workerStore) GetAllowPrivateTargets(context.Context) (bool, error) { return false, nil }

type workerSender func(context.Context, Message) error

func (f workerSender) Send(ctx context.Context, m Message) error { return f(ctx, m) }

func newWorkerFixture(t *testing.T) (*Worker, *workerStore, *time.Time) {
	t.Helper()
	box, err := auth.NewSecretBox(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed := box.Seal([]byte(`{"token":"fake-token"}`))
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	nowp := &now
	s := &workerStore{
		channel: store.NotificationChannel{ID: "channel", Type: "telegram", Enabled: true, Language: "en", Config: json.RawMessage(`{"chat_id":"123"}`)},
		sealed:  sealed, retries: map[string]time.Time{}, deliveryErrors: map[string]string{},
		alert:  store.Alert{ID: "alert", RuleType: "server_offline", Severity: "critical", ServerID: workerStringPointer("server"), SubjectName: "api", Summary: json.RawMessage(`{"key":"alerts.server_offline","params":{"value":"offline"}}`), StartedAt: now},
		server: store.Server{Name: "prod-server"},
	}
	w := New(Options{Store: s, Box: box, Clock: func() time.Time { return *nowp }, AllowPrivate: func() bool { return false }})
	w.senderFactory = func(store.NotificationChannel, map[string]string, func() bool, HTTPClientFactory) Sender {
		return workerSender(func(_ context.Context, m Message) error { return nil })
	}
	return w, s, nowp
}

func workerStringPointer(value string) *string { return &value }

func TestWorkerDeliverSuccessLocalizesAndMarksSent(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{{"ca", "fora de línia"}, {"es", "sin conexión"}} {
		t.Run(tc.lang, func(t *testing.T) {
			w, s, _ := newWorkerFixture(t)
			s.channel.Language = tc.lang
			alertID, channelID := "alert", "channel"
			s.deliveries = []store.NotificationDelivery{{ID: "delivery", AlertID: &alertID, ChannelID: &channelID, Event: "firing", Attempts: 1}}
			var got Message
			w.senderFactory = func(store.NotificationChannel, map[string]string, func() bool, HTTPClientFactory) Sender {
				return workerSender(func(_ context.Context, m Message) error { got = m; return nil })
			}
			w.process(context.Background())
			if len(s.sent) != 1 || s.sent[0] != "delivery" {
				t.Fatalf("sent = %v", s.sent)
			}
			if len(s.statuses) != 1 || !s.statuses[0].ok {
				t.Fatalf("statuses = %+v", s.statuses)
			}
			if !strings.Contains(strings.ToLower(got.Title), strings.ToLower(tc.want)) || !strings.Contains(got.Title, "prod-server") {
				t.Fatalf("title = %q", got.Title)
			}
			if !strings.Contains(got.Body, "prod-server") {
				t.Fatalf("body = %q", got.Body)
			}
		})
	}
}

func TestWorkerTestDeliverySendsLocalizedTestMessage(t *testing.T) {
	w, s, _ := newWorkerFixture(t)
	s.channel.Language = "ca"
	channelID := "channel"
	s.deliveries = []store.NotificationDelivery{{ID: "test", ChannelID: &channelID, Event: "test", Attempts: 1}}
	var got Message
	w.senderFactory = func(store.NotificationChannel, map[string]string, func() bool, HTTPClientFactory) Sender {
		return workerSender(func(_ context.Context, m Message) error { got = m; return nil })
	}
	w.process(context.Background())
	if len(s.sent) != 1 || got.Event != "test" || !strings.Contains(strings.ToLower(got.Body), "prova") {
		t.Fatalf("sent=%v message=%+v", s.sent, got)
	}
}

func TestWorkerFailureBackoffAndRedaction(t *testing.T) {
	backoff := []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute}
	for attempt := 1; attempt <= 5; attempt++ {
		t.Run(string(rune('0'+attempt)), func(t *testing.T) {
			w, s, now := newWorkerFixture(t)
			channelID := "channel"
			s.deliveries = []store.NotificationDelivery{{ID: "delivery", ChannelID: &channelID, Event: "firing", Attempts: attempt}}
			w.senderFactory = func(_ store.NotificationChannel, secrets map[string]string, _ func() bool, _ HTTPClientFactory) Sender {
				return workerSender(func(context.Context, Message) error {
					return errors.New("request failed with token " + secrets["token"])
				})
			}
			w.process(context.Background())
			if len(s.statuses) != 1 || s.statuses[0].ok || strings.Contains(s.statuses[0].lastError, "fake-token") {
				t.Fatalf("statuses = %+v", s.statuses)
			}
			if strings.Contains(s.deliveryErrors["delivery"], "fake-token") {
				t.Fatalf("stored error contains secret: %q", s.deliveryErrors["delivery"])
			}
			if attempt < 5 {
				if !s.retries["delivery"].Equal(now.Add(backoff[attempt-1])) {
					t.Fatalf("retry at %s", s.retries["delivery"])
				}
			} else if len(s.failed) != 1 {
				t.Fatalf("failed = %v", s.failed)
			}
		})
	}
}

func TestWorkerDisabledOrRemovedChannelDoesNotSend(t *testing.T) {
	for _, tc := range []struct {
		name     string
		disabled bool
		removed  bool
	}{{"disabled", true, false}, {"removed", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			w, s, _ := newWorkerFixture(t)
			channelID := "channel"
			s.deliveries = []store.NotificationDelivery{{ID: "delivery", ChannelID: &channelID, Attempts: 1}}
			if tc.disabled {
				s.channel.Enabled = false
			}
			if tc.removed {
				s.channelErr = store.ErrNotFound
			}
			sends := 0
			w.senderFactory = func(store.NotificationChannel, map[string]string, func() bool, HTTPClientFactory) Sender {
				return workerSender(func(context.Context, Message) error { sends++; return nil })
			}
			w.process(context.Background())
			if sends != 0 || len(s.failed) != 1 {
				t.Fatalf("sends=%d failed=%v", sends, s.failed)
			}
		})
	}
}

func TestWorkerPanickingSenderSchedulesRetry(t *testing.T) {
	w, s, now := newWorkerFixture(t)
	channelID := "channel"
	s.deliveries = []store.NotificationDelivery{{ID: "delivery", ChannelID: &channelID, Attempts: 1}}
	w.senderFactory = func(store.NotificationChannel, map[string]string, func() bool, HTTPClientFactory) Sender {
		return workerSender(func(context.Context, Message) error { panic("sender panic") })
	}
	w.process(context.Background())
	if !s.retries["delivery"].Equal(now.Add(30*time.Second)) || len(s.statuses) != 1 || s.statuses[0].ok {
		t.Fatalf("retry=%v status=%+v", s.retries, s.statuses)
	}
}

func TestSendTestUpdatesStatusAndRedactsFailure(t *testing.T) {
	w, s, _ := newWorkerFixture(t)
	w.senderFactory = func(_ store.NotificationChannel, secrets map[string]string, _ func() bool, _ HTTPClientFactory) Sender {
		return workerSender(func(context.Context, Message) error { return errors.New("failed token " + secrets["token"]) })
	}
	err := w.SendTest(context.Background(), "channel")
	if err == nil || strings.Contains(err.Error(), "fake-token") {
		t.Fatalf("error = %v", err)
	}
	if len(s.statuses) != 1 || s.statuses[0].ok || strings.Contains(s.statuses[0].lastError, "fake-token") {
		t.Fatalf("statuses = %+v", s.statuses)
	}
	w.senderFactory = func(store.NotificationChannel, map[string]string, func() bool, HTTPClientFactory) Sender {
		return workerSender(func(context.Context, Message) error { return nil })
	}
	if err := w.SendTest(context.Background(), "channel"); err != nil {
		t.Fatal(err)
	}
	if len(s.statuses) != 2 || !s.statuses[1].ok {
		t.Fatalf("statuses = %+v", s.statuses)
	}
}

func TestWorkerRunPollsAndStopsOnCancel(t *testing.T) {
	w, s, _ := newWorkerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.claimHook = cancel
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("Run did not stop after cancellation")
	}
	if s.claims == 0 {
		t.Fatal("Run did not poll ClaimDue")
	}
}
