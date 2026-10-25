package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

func TestSameContainerNameOnTwoServersGivesTwoIncidents(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("down", "container_down", map[string]any{"seconds": 0})}
	source := &testSource{states: []ServerState{
		{ID: "a", Containers: []ContainerState{{Name: "web", State: "running"}}},
		{ID: "b", Containers: []ContainerState{{Name: "web", State: "running"}}},
	}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	source.states[0].Containers[0].State = "exited"
	source.states[1].Containers[0].State = "exited"
	evaluateAt(t, e, c, time.Second)
	if st.alerts["down:a/web"].State != "firing" || st.alerts["down:b/web"].State != "firing" {
		t.Fatalf("alerts=%+v, want one firing incident per server", st.alerts)
	}
	source.states[0].Containers[0].State = "running"
	evaluateAt(t, e, c, time.Second)
	if st.alerts["down:a/web"].State != "resolved" || st.alerts["down:b/web"].State != "firing" {
		t.Fatalf("recovering on one server changed the other: %+v", st.alerts)
	}
}

func TestIncidentResolvedOutsideTheEngineDoesNotHideTheNextOne(t *testing.T) {
	ctx := context.Background()
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("down", "container_down", map[string]any{"seconds": 0})}
	source := &testSource{states: []ServerState{{ID: "srv", Containers: []ContainerState{{Name: "web", State: "running"}}}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	source.states[0].Containers[0].State = "exited"
	evaluateAt(t, e, c, time.Second)
	first := st.alerts["down:srv/web"]
	if first.State != "firing" {
		t.Fatalf("first incident=%+v", first)
	}
	// What the API does for "stop watching": delete the watch and resolve.
	e.Unwatch(ctx, "srv", "web")
	if err := st.ResolveAlert(ctx, first.ID, c.now); err != nil {
		t.Fatal(err)
	}
	evaluateAt(t, e, c, time.Second)
	source.states[0].Containers[0].State = "running"
	evaluateAt(t, e, c, time.Second)
	source.states[0].Containers[0].State = "exited"
	evaluateAt(t, e, c, time.Second)
	if st.alerts["down:srv/web"].State != "firing" {
		t.Fatalf("the next failure was hidden by the stale incident: %+v", st.alerts)
	}
}

func TestFailedStopFromTalaiaIsWatchedAgainAfterTheGrace(t *testing.T) {
	ctx := context.Background()
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("down", "container_down", map[string]any{"seconds": 0})}
	source := &testSource{states: []ServerState{{ID: "srv", Containers: []ContainerState{{Name: "web", State: "running"}}}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	// The stop was dispatched but failed: the container keeps running.
	e.Unwatch(ctx, "srv", "web")
	evaluateAt(t, e, c, time.Minute)
	evaluateAt(t, e, c, 2*time.Minute)
	source.states[0].Containers[0].State = "exited"
	evaluateAt(t, e, c, time.Second)
	if st.alerts["down:srv/web"].State != "firing" {
		t.Fatalf("a container whose stop failed stayed unwatched: %+v", st.alerts)
	}
}

func TestThresholdAlertStaysOpenWhileTheAgentSendsNoMetrics(t *testing.T) {
	st := newTestStore()
	st.rules = []store.AlertRule{scenarioRule("disk", "disk_high", map[string]any{"threshold": 90, "for_seconds": 0})}
	st.channels = []store.NotificationChannel{{ID: "one", Enabled: true}}
	source := &testSource{states: []ServerState{{ID: "srv", Connected: true, HasMetrics: true, DiskUsed: 95, DiskTotal: 100}}}
	e, c := scenarioEngine(st, source)
	if err := e.evaluate(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	source.states[0] = ServerState{ID: "srv"}
	evaluateAt(t, e, c, time.Minute)
	if st.alerts["disk:srv"].State != "firing" || len(st.deliveries) != 1 {
		t.Fatalf("an offline agent resolved the disk alert: %+v deliveries=%d", st.alerts, len(st.deliveries))
	}
}
