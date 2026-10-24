package alerts

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

var scenarioStart = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func scenarioRule(id, kind string, params any) store.AlertRule {
	b, _ := json.Marshal(params)
	return store.AlertRule{ID: id, Type: kind, Enabled: true, Severity: "critical", Params: b}
}

func scenarioEngine(st *testStore, source *testSource) (*Engine, *testClock) {
	c := &testClock{now: scenarioStart}
	e := New(st, source, c)
	e.started = scenarioStart
	return e, c
}

func evaluateAt(t *testing.T, e *Engine, c *testClock, d time.Duration) {
	t.Helper()
	c.now = c.now.Add(d)
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
}

func TestContainerDownFiresWithinThirtySecondsAndNotifiesEnabledChannels(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("down", "container_down", map[string]any{"seconds": 20})}
	st.channels = []store.NotificationChannel{{ID: "enabled-a", Enabled: true}, {ID: "disabled", Enabled: false}, {ID: "enabled-b", Enabled: true}}
	source := &testSource{states: []ServerState{{ID: "srv", Containers: []ContainerState{{Name: "web", State: "running"}}}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	source.states[0].Containers[0].State = "exited"
	for elapsed := 0; elapsed < 30 && len(st.alerts) == 0; elapsed += 5 {
		evaluateAt(t, e, c, 5*time.Second)
	}
	if len(st.alerts) != 1 || st.alerts["down:web"].State != "firing" {
		t.Fatalf("alerts=%+v, want one firing alert within 30 seconds", st.alerts)
	}
	if len(st.deliveries) != 2 {
		t.Fatalf("deliveries=%+v, want one per enabled channel", st.deliveries)
	}
	for _, d := range st.deliveries {
		if d.Event != "firing" || d.ChannelID == nil || *d.ChannelID == "disabled" {
			t.Fatalf("unexpected delivery: %+v", d)
		}
	}
}

func TestRepeatedFiringEvaluationsDoNotCreateIncidentOrDelivery(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("cpu", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})}
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
	source := &testSource{states: []ServerState{{ID: "srv", HasMetrics: true, CPUPercent: 95}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		evaluateAt(t, e, c, 5*time.Second)
	}
	if len(st.alerts) != 1 || len(st.deliveries) != 1 {
		t.Fatalf("alerts=%d deliveries=%d, want one each", len(st.alerts), len(st.deliveries))
	}
}

func TestUnwatchBeforeContainerDisappearsAllowsLaterRewatch(t *testing.T) {
	ctx := context.Background()
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("down", "container_down", map[string]any{"seconds": 0})}
	source := &testSource{states: []ServerState{{ID: "srv", Containers: []ContainerState{{Name: "web", State: "running"}}}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	e.Unwatch(ctx, "srv", "web")
	source.states[0].Containers = nil
	evaluateAt(t, e, c, time.Second)
	if len(st.alerts) != 0 {
		t.Fatalf("alert fired for unwatched container: %+v", st.alerts)
	}
	source.states[0].Containers = []ContainerState{{Name: "web", State: "running"}}
	evaluateAt(t, e, c, time.Second)
	source.states[0].Containers[0].State = "exited"
	evaluateAt(t, e, c, time.Second)
	if got := st.alerts["down:web"].State; got != "firing" {
		t.Fatalf("later stop state=%q, want firing", got)
	}
}

func TestServerOfflineGraceLastSeenReconnectAndExcludedServers(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("offline", "server_offline", map[string]any{"seconds": 45})}
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
	lastSeen := scenarioStart.Add(-45 * time.Second)
	neverSeen := ServerState{ID: "never", Connected: false}
	revoked := ServerState{ID: "revoked", Connected: false, Revoked: true, LastSeen: scenarioStart.Add(-time.Hour)}
	source := &testSource{states: []ServerState{{ID: "srv", Connected: false, LastSeen: lastSeen}, neverSeen, revoked}}
	e, c := scenarioEngine(st, source)
	for range 17 {
		evaluateAt(t, e, c, 5*time.Second)
	}
	if len(st.alerts) != 0 {
		t.Fatalf("offline alert fired during first 90 seconds: %+v", st.alerts)
	}
	evaluateAt(t, e, c, 5*time.Second)
	if st.alerts["offline:srv"].State != "firing" || len(st.alerts) != 1 {
		t.Fatalf("alerts after grace=%+v, want only srv firing", st.alerts)
	}
	source.states[0].Connected = true
	evaluateAt(t, e, c, time.Second)
	if st.alerts["offline:srv"].State != "resolved" || len(st.deliveries) != 2 || st.deliveries[1].Event != "resolved" {
		t.Fatalf("reconnect alerts=%+v deliveries=%+v", st.alerts, st.deliveries)
	}
}

func TestLoadedOpenAlertIsNotRenotified(t *testing.T) {
	last := scenarioStart.Add(-time.Minute)
	rule := scenarioRule("cpu", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})
	serverID := "srv"
	alert := store.Alert{ID: "existing", RuleID: &rule.ID, RuleType: rule.Type, Fingerprint: "cpu:srv", ServerID: &serverID, SubjectKind: "server", SubjectName: "srv", State: "firing", LastNotifiedAt: &last}
	st := newTestStore()
	st.rules = []store.AlertRule{rule}
	st.alerts[alert.Fingerprint] = alert
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
	source := &testSource{states: []ServerState{{ID: "srv", HasMetrics: true, CPUPercent: 95}}}
	e, _ := scenarioEngine(st, source)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = e.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for len(st.deliveries) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if len(st.deliveries) != 0 {
		t.Fatalf("loaded alert was re-notified: %+v", st.deliveries)
	}
}

func TestSilenceScopesAndRuleTypeFilter(t *testing.T) {
	tests := []struct {
		name       string
		silence    store.Silence
		wantSilent bool
	}{
		{name: "server", silence: store.Silence{ScopeType: "server", ScopeServerID: scenarioPtr("srv"), EndsAt: scenarioStart.Add(time.Hour)}, wantSilent: true},
		{name: "group", silence: store.Silence{ScopeType: "group", ScopeGroupID: scenarioPtr("group-a"), EndsAt: scenarioStart.Add(time.Hour)}, wantSilent: true},
		{name: "other rule type", silence: store.Silence{ScopeType: "server", ScopeServerID: scenarioPtr("srv"), RuleType: scenarioPtr("disk_high"), EndsAt: scenarioStart.Add(time.Hour)}, wantSilent: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newTestStore()
			st.rules = []store.AlertRule{scenarioRule("cpu", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})}
			st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
			st.silences = []store.Silence{tt.silence}
			source := &testSource{states: []ServerState{{ID: "srv", GroupID: "group-a", HasMetrics: true, CPUPercent: 95}}}
			e, _ := scenarioEngine(st, source)
			if err := e.evaluate(context.Background(), ""); err != nil {
				t.Fatal(err)
			}
			a := st.alerts["cpu:srv"]
			if a.Silenced != tt.wantSilent || (len(st.deliveries) == 0) != tt.wantSilent {
				t.Fatalf("alert=%+v deliveries=%d", a, len(st.deliveries))
			}
		})
	}
}

func TestSilenceStartingAfterIncidentUpdatesSilencedAndSuppressesRepeat(t *testing.T) {
	st := newTestStore()
	rule := scenarioRule("cpu", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})
	rule.RepeatIntervalSeconds = 5
	st.rules = []store.AlertRule{rule}
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
	source := &testSource{states: []ServerState{{ID: "srv", HasMetrics: true, CPUPercent: 95}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	st.silences = []store.Silence{{ScopeType: "server", ScopeServerID: scenarioPtr("srv"), EndsAt: c.now.Add(time.Minute)}}
	evaluateAt(t, e, c, 5*time.Second)
	if !st.alerts["cpu:srv"].Silenced || len(st.deliveries) != 1 {
		t.Fatalf("silenced alert=%+v deliveries=%d", st.alerts["cpu:srv"], len(st.deliveries))
	}
}

func TestExpiredSilenceNoLongerApplies(t *testing.T) {
	st := newTestStore()
	rule := scenarioRule("cpu", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})
	rule.RepeatIntervalSeconds = 5
	st.rules = []store.AlertRule{rule}
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
	st.silences = []store.Silence{{ScopeType: "server", ScopeServerID: scenarioPtr("srv"), EndsAt: scenarioStart.Add(5 * time.Second)}}
	source := &testSource{states: []ServerState{{ID: "srv", HasMetrics: true, CPUPercent: 95}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !st.alerts["cpu:srv"].Silenced || len(st.deliveries) != 0 {
		t.Fatal("initial incident should be silenced")
	}
	evaluateAt(t, e, c, 5*time.Second)
	if st.alerts["cpu:srv"].Silenced || len(st.deliveries) != 0 {
		t.Fatalf("expired silence still applies: alert=%+v deliveries=%d", st.alerts["cpu:srv"], len(st.deliveries))
	}
}

func TestSilenceServerForCreatesTimedSystemSilence(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("cpu", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})}
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
	source := &testSource{states: []ServerState{{ID: "srv", HasMetrics: true, CPUPercent: 95}, {ID: "other", HasMetrics: true, CPUPercent: 95}}}
	e, c := scenarioEngine(st, source)
	if err := e.SilenceServerFor(context.Background(), "srv", 10*time.Minute, "reboot"); err != nil {
		t.Fatal(err)
	}
	if len(st.silences) != 1 || st.silences[0].ScopeType != "server" || st.silences[0].CreatedBy != "system" || st.silences[0].Reason != "reboot" || !st.silences[0].EndsAt.Equal(c.now.Add(10*time.Minute)) {
		t.Fatalf("silence=%+v", st.silences)
	}
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !st.alerts["cpu:srv"].Silenced || st.alerts["cpu:other"].Silenced || len(st.deliveries) != 1 {
		t.Fatalf("alerts=%+v deliveries=%d", st.alerts, len(st.deliveries))
	}
	source.states = source.states[:1]
	evaluateAt(t, e, c, 10*time.Minute)
	source.states[0].CPUPercent = 0
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	source.states[0].CPUPercent = 95
	evaluateAt(t, e, c, 11*time.Minute)
	if len(st.deliveries) != 3 || st.deliveries[2].Event != "firing" {
		t.Fatalf("post-silence deliveries=%+v", st.deliveries)
	}
}

func TestRuleChannelIDsRestrictDeliveriesAndEmptyMeansAllEnabled(t *testing.T) {
	for _, tt := range []struct {
		name string
		ids  []string
		want []string
	}{{"restricted", []string{"two"}, []string{"two"}}, {"all", nil, []string{"one", "two"}}} {
		t.Run(tt.name, func(t *testing.T) {
			st := newTestStore()
			rule := scenarioRule("cpu", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})
			rule.ChannelIDs = tt.ids
			st.rules = []store.AlertRule{rule}
			st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}, {ID: "two", Enabled: true}, {ID: "disabled", Enabled: false}}
			e, _ := scenarioEngine(st, &testSource{states: []ServerState{{ID: "srv", HasMetrics: true, CPUPercent: 95}}})
			if err := e.evaluate(context.Background(), ""); err != nil {
				t.Fatal(err)
			}
			if len(st.deliveries) != len(tt.want) {
				t.Fatalf("deliveries=%+v", st.deliveries)
			}
			for i, d := range st.deliveries {
				if d.ChannelID == nil || *d.ChannelID != tt.want[i] {
					t.Fatalf("delivery %d=%+v, want channel %s", i, d, tt.want[i])
				}
			}
		})
	}
}

func TestDisabledAndNonmatchingScopedRulesDoNotFire(t *testing.T) {
	st := newTestStore()
	disabled := scenarioRule("disabled", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})
	disabled.Enabled = false
	server := scenarioRule("server", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})
	server.ScopeType, server.ScopeServerID = "server", scenarioPtr("other-server")
	stack := scenarioRule("stack", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})
	stack.ScopeType, stack.ScopeName = "stack", scenarioPtr("other-stack")
	container := scenarioRule("container", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})
	container.ScopeType, container.ScopeName = "container", scenarioPtr("other-container")
	group := scenarioRule("group", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})
	group.ScopeType, group.ScopeGroupID = "group", scenarioPtr("other-group")
	st.rules = []store.AlertRule{disabled, server, stack, container, group}
	e, _ := scenarioEngine(st, &testSource{states: []ServerState{{ID: "srv", GroupID: "group-a", HasMetrics: true, CPUPercent: 95}}})
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(st.alerts) != 0 {
		t.Fatalf("nonmatching rules fired: %+v", st.alerts)
	}
}

func TestContainerUnhealthyFiresAfterDurationAndResolvesWhenHealthy(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("health", "container_unhealthy", map[string]any{"seconds": 60})}
	source := &testSource{states: []ServerState{{ID: "srv", Containers: []ContainerState{{Name: "web", State: "running", Health: "unhealthy"}}}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(st.alerts) != 0 {
		t.Fatal("unhealthy alert fired before 60 seconds")
	}
	evaluateAt(t, e, c, 60*time.Second)
	if st.alerts["health:web"].State != "firing" {
		t.Fatal("unhealthy alert did not fire after 60 seconds")
	}
	source.states[0].Containers[0].Health = "healthy"
	evaluateAt(t, e, c, time.Second)
	if st.alerts["health:web"].State != "resolved" {
		t.Fatalf("healthy state did not resolve alert: %+v", st.alerts["health:web"])
	}
}

func TestCheckFailedCountsConsecutiveResultsAndResolvesOnOK(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("failed", "check_failed", map[string]any{"consecutive_failures": 2})}
	st.checks = []store.AlertCheck{{ID: "check-1", Enabled: true}}
	e, c := scenarioEngine(st, &testSource{})
	e.CheckResult(store.AlertCheckResult{CheckID: "check-1", OK: false})
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(st.alerts) != 0 {
		t.Fatal("check alert fired after one failure")
	}
	e.CheckResult(store.AlertCheckResult{CheckID: "check-1", OK: false})
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if st.alerts["failed:check-1"].State != "firing" {
		t.Fatal("check alert did not fire after two failures")
	}
	e.CheckResult(store.AlertCheckResult{CheckID: "check-1", OK: true})
	evaluateAt(t, e, c, time.Second)
	if st.alerts["failed:check-1"].State != "resolved" {
		t.Fatalf("OK result did not resolve alert: %+v", st.alerts["failed:check-1"])
	}
}

func TestCertExpiryUsesConfiguredDays(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("cert", "cert_expiry", map[string]any{"days": 14})}
	st.checks = []store.AlertCheck{{ID: "near", Enabled: true}, {ID: "far", Enabled: true}}
	near, far := scenarioStart.Add(10*24*time.Hour), scenarioStart.Add(30*24*time.Hour)
	st.results = map[string]store.AlertCheckResult{
		"near": {CheckID: "near", OK: true, CertNotAfter: &near},
		"far":  {CheckID: "far", OK: true, CertNotAfter: &far},
	}
	e, _ := scenarioEngine(st, &testSource{})
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if st.alerts["cert:near"].State != "firing" || len(st.alerts) != 1 {
		t.Fatalf("cert expiry alerts=%+v, want only near cert", st.alerts)
	}
}

func TestFiringAlertResolvesWhenServerDisappears(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("cpu", "cpu_high", map[string]any{"threshold": 90, "for_seconds": 0})}
	source := &testSource{states: []ServerState{{ID: "srv", HasMetrics: true, CPUPercent: 95}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	source.states = nil
	evaluateAt(t, e, c, time.Second)
	if st.alerts["cpu:srv"].State != "resolved" {
		t.Fatalf("vanished server alert=%+v", st.alerts["cpu:srv"])
	}
}

func TestCPUAndMemoryPendingDurationAndHysteresis(t *testing.T) {
	for _, kind := range []string{"cpu_high", "mem_high"} {
		t.Run(kind, func(t *testing.T) {
			st := newTestStore()
			st.rules = []store.AlertRule{scenarioRule(kind, kind, map[string]any{"threshold": 90, "for_seconds": 60})}
			state := ServerState{ID: "srv", HasMetrics: true, CPUPercent: 95, MemUsed: 95, MemTotal: 100}
			source := &testSource{states: []ServerState{state}}
			e, c := scenarioEngine(st, source)
			if err := e.evaluate(context.Background(), ""); err != nil {
				t.Fatal(err)
			}
			for range 11 {
				evaluateAt(t, e, c, 5*time.Second)
			}
			if len(st.alerts) != 0 {
				t.Fatal("alert fired before 60 seconds")
			}
			evaluateAt(t, e, c, 5*time.Second)
			if st.alerts[kind+":srv"].State != "firing" {
				t.Fatal("alert did not fire at 60 seconds")
			}
			if kind == "cpu_high" {
				source.states[0].CPUPercent = 88
			} else {
				source.states[0].MemUsed = 88
			}
			evaluateAt(t, e, c, 5*time.Second)
			if st.alerts[kind+":srv"].State != "firing" {
				t.Fatal("alert resolved at threshold minus two")
			}
			if kind == "cpu_high" {
				source.states[0].CPUPercent = 87
			} else {
				source.states[0].MemUsed = 87
			}
			evaluateAt(t, e, c, 5*time.Second)
			if st.alerts[kind+":srv"].State != "resolved" {
				t.Fatal("alert did not resolve below threshold minus two")
			}
		})
	}
}

func scenarioPtr(v string) *string { return &v }
