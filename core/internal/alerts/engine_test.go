package alerts

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time                 { return c.now }
func (c *testClock) NewTicker(time.Duration) Ticker { return testTicker{make(chan time.Time)} }

type testTicker struct{ ch <-chan time.Time }

func (t testTicker) C() <-chan time.Time { return t.ch }
func (testTicker) Stop()                 {}

type testSource struct{ states []ServerState }

func (s *testSource) Snapshot(context.Context) ([]ServerState, error) { return s.states, nil }

type testStore struct {
	rules      []store.AlertRule
	alerts     map[string]store.Alert
	channels   []store.NotificationChannel
	deliveries []store.NotificationDelivery
	watches    []store.AlertWatch
	silences   []store.Silence
	checks     []store.AlertCheck
	results    map[string]store.AlertCheckResult
	purges     [][2]time.Time
}

func newTestStore() *testStore                                                 { return &testStore{alerts: map[string]store.Alert{}} }
func (s *testStore) ListAlertRules(context.Context) ([]store.AlertRule, error) { return s.rules, nil }
func (s *testStore) OpenAlert(_ context.Context, a store.Alert) (store.Alert, bool, error) {
	if old, ok := s.alerts[a.Fingerprint]; ok && old.State == "firing" {
		return old, false, nil
	}
	a.ID = "alert-" + a.Fingerprint
	s.alerts[a.Fingerprint] = a
	return a, true, nil
}
func (s *testStore) ResolveAlert(_ context.Context, id string, at time.Time) error {
	for fp, a := range s.alerts {
		if a.ID == id {
			a.State = "resolved"
			a.ResolvedAt = &at
			s.alerts[fp] = a
		}
	}
	return nil
}
func (s *testStore) ListActiveAlerts(context.Context) ([]store.Alert, error) {
	out := []store.Alert{}
	for _, a := range s.alerts {
		if a.State == "firing" {
			out = append(out, a)
		}
	}
	return out, nil
}
func (s *testStore) MarkAlertNotified(_ context.Context, id string, at time.Time) error {
	for fp, a := range s.alerts {
		if a.ID == id {
			a.LastNotifiedAt = &at
			s.alerts[fp] = a
		}
	}
	return nil
}
func (s *testStore) UpsertWatch(_ context.Context, w store.AlertWatch) error {
	s.watches = append(s.watches, w)
	return nil
}
func (s *testStore) DeleteWatch(context.Context, string, string) error       { return nil }
func (s *testStore) ListWatches(context.Context) ([]store.AlertWatch, error) { return s.watches, nil }
func (s *testStore) CreateSilence(_ context.Context, v store.Silence) (store.Silence, error) {
	s.silences = append(s.silences, v)
	return v, nil
}
func (s *testStore) ListActiveSilences(_ context.Context, now time.Time) ([]store.Silence, error) {
	out := []store.Silence{}
	for _, v := range s.silences {
		if v.EndsAt.After(now) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *testStore) ListNotificationChannels(context.Context) ([]store.NotificationChannel, error) {
	return s.channels, nil
}
func (s *testStore) EnqueueDelivery(_ context.Context, v store.NotificationDelivery) (store.NotificationDelivery, error) {
	s.deliveries = append(s.deliveries, v)
	return v, nil
}
func (s *testStore) ListAlertChecks(context.Context) ([]store.AlertCheck, error) {
	return s.checks, nil
}
func (s *testStore) LatestAlertCheckResult(_ context.Context, id string) (store.AlertCheckResult, error) {
	if result, ok := s.results[id]; ok {
		return result, nil
	}
	return store.AlertCheckResult{}, store.ErrNotFound
}
func (s *testStore) PurgeAlertHistory(_ context.Context, alertsBefore, deliveriesBefore time.Time) (int64, int64, error) {
	s.purges = append(s.purges, [2]time.Time{alertsBefore, deliveriesBefore})
	return 0, 0, nil
}
func (s *testStore) SetAlertSilenced(_ context.Context, id string, silenced bool) error {
	for fp, a := range s.alerts {
		if a.ID == id {
			a.Silenced = silenced
			s.alerts[fp] = a
		}
	}
	return nil
}

func TestDiskAlertHysteresisAndNotifications(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	params, _ := json.Marshal(map[string]any{"threshold": 90, "for_seconds": 0})
	st := newTestStore()
	st.rules = []store.AlertRule{{ID: "disk", Type: "disk_high", Enabled: true, Severity: "critical", Params: params}}
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}, {ID: "two", Enabled: true}}
	source := &testSource{states: []ServerState{{ID: "srv", Connected: true, HasMetrics: true, DiskUsed: 91, DiskTotal: 100}}}
	e := New(st, source, clock)
	e.started = now.Add(-time.Minute)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if len(st.alerts) != 1 || len(st.deliveries) != 2 {
		t.Fatalf("opened alerts=%d deliveries=%d", len(st.alerts), len(st.deliveries))
	}
	source.states[0].DiskUsed = 89
	clock.now = clock.now.Add(5 * time.Second)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := st.alerts["disk:srv"].State; got != "firing" {
		t.Fatalf("at 89%% state=%q", got)
	}
	source.states[0].DiskUsed = 87
	clock.now = clock.now.Add(5 * time.Second)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := st.alerts["disk:srv"].State; got != "resolved" {
		t.Fatalf("at 87%% state=%q", got)
	}
	if len(st.deliveries) != 4 {
		t.Fatalf("deliveries=%d, want firing and resolved for both channels", len(st.deliveries))
	}
}

func TestContainerDownEvaluatesWatchedContainer(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	params, _ := json.Marshal(map[string]any{"seconds": 20})
	st := newTestStore()
	st.rules = []store.AlertRule{{ID: "down", Type: "container_down", Enabled: true, Severity: "critical", Params: params}}
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
	source := &testSource{states: []ServerState{{ID: "srv", Connected: true, Containers: []ContainerState{{Name: "web", State: "running", Stack: "shop"}}}}}
	e := New(st, source, clock)
	e.started = now
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	source.states[0].Containers[0].State = "exited"
	e.InventoryChanged("srv")
	if err := e.evaluate(ctx, "srv"); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(19 * time.Second)
	if err := e.evaluate(ctx, "srv"); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	if err := e.evaluate(ctx, "srv"); err != nil {
		t.Fatal(err)
	}
	if len(st.alerts) != 1 || st.alerts["down:srv/web"].State != "firing" {
		t.Fatalf("alerts=%+v", st.alerts)
	}
}

func TestRepeatAckAndCooldown(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	params, _ := json.Marshal(map[string]any{"threshold": 90, "for_seconds": 0})
	st := newTestStore()
	st.rules = []store.AlertRule{{ID: "cpu", Type: "cpu_high", Enabled: true, Severity: "critical", Params: params, RepeatIntervalSeconds: 10}}
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
	source := &testSource{states: []ServerState{{ID: "srv", Connected: true, HasMetrics: true, CPUPercent: 95}}}
	e := New(st, source, clock)
	e.started = now.Add(-time.Minute)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if len(st.deliveries) != 1 {
		t.Fatalf("duplicate initial delivery count %d", len(st.deliveries))
	}
	clock.now = clock.now.Add(10 * time.Second)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if len(st.deliveries) != 2 {
		t.Fatalf("repeat delivery count %d", len(st.deliveries))
	}
	a := st.alerts["cpu:srv"]
	acked := clock.now
	a.AckedAt = &acked
	st.alerts["cpu:srv"] = a
	clock.now = clock.now.Add(10 * time.Second)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if len(st.deliveries) != 2 {
		t.Fatalf("acknowledged alert repeated: %d", len(st.deliveries))
	}
	source.states[0].CPUPercent = 0
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	source.states[0].CPUPercent = 95
	clock.now = clock.now.Add(time.Minute)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if len(st.deliveries) != 3 {
		t.Fatalf("cooldown did not suppress re-fire: deliveries=%d", len(st.deliveries))
	}
	if st.alerts["cpu:srv"].State != "firing" {
		t.Fatalf("re-fire did not create an incident")
	}
}
