package alerts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/notify"
	"github.com/catlinux/Talaia/core/internal/store"
)

type webhookCall struct {
	timestamp, signature string
	body                 []byte
}

type webhookRecorder struct {
	mu    sync.Mutex
	calls []webhookCall
}

func (r *webhookRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.calls = append(r.calls, webhookCall{req.Header.Get("X-Talaia-Timestamp"), req.Header.Get("X-Talaia-Signature"), body})
	r.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (r *webhookRecorder) wait(t *testing.T, count int) []webhookCall {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		got := append([]webhookCall(nil), r.calls...)
		r.mu.Unlock()
		if len(got) >= count {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("webhook received %d calls, want %d", len(r.calls), count)
	return nil
}

// TestStoppedContainerReachesAWebhookEndToEnd runs the real store, the engine
// and the notification worker against a fake webhook: a watched container
// stops, the incident opens, the signed notification arrives, and when the
// container runs again the resolution arrives.
func TestStoppedContainerReachesAWebhookEndToEnd(t *testing.T) {
	url := os.Getenv("TALAIA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TALAIA_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprint(time.Now().UnixNano())
	serverID, err := st.CreateServer(ctx, "e2e-server-"+suffix)
	if err != nil {
		t.Fatal(err)
	}

	// The built-in rules would also notify every other channel of the shared
	// test database: switch them off while this test runs.
	rules, err := st.ListAlertRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range rules {
		if rule.Builtin && rule.Enabled {
			rule.Enabled = false
			if err := st.UpdateAlertRule(ctx, rule); err != nil {
				t.Fatal(err)
			}
			original := rule
			original.Enabled = true
			t.Cleanup(func() { _ = st.UpdateAlertRule(context.Background(), original) })
		}
	}
	// Silences left by other tests of the shared database would mute this one.
	silences, err := st.ListActiveSilences(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, silence := range silences {
		if err := st.DeleteSilence(ctx, silence.ID); err != nil {
			t.Fatal(err)
		}
	}
	previous, err := st.GetAllowPrivateTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAllowPrivateTargets(ctx, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.SetAllowPrivateTargets(context.Background(), previous) })

	recorder := &webhookRecorder{}
	hook := httptest.NewServer(recorder)
	t.Cleanup(hook.Close)

	key := make([]byte, 32)
	box, err := auth.NewSecretBox(key)
	if err != nil {
		t.Fatal(err)
	}
	const hmacSecret = "e2e-hmac-secret"
	secret, _ := json.Marshal(map[string]string{"url": hook.URL, "hmac_secret": hmacSecret})
	channel, err := st.CreateNotificationChannel(ctx, store.NotificationChannel{Name: "e2e-" + suffix, Type: "webhook", Enabled: true, Language: "es"}, box.Seal(secret))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.DeleteNotificationChannel(context.Background(), channel.ID) })

	rule, err := st.CreateAlertRule(ctx, store.AlertRule{Name: "e2e-down-" + suffix, Type: "container_down", Enabled: true, Severity: "critical",
		Params: json.RawMessage(`{"seconds":20}`), ScopeType: "server", ScopeServerID: &serverID, ChannelIDs: []string{channel.ID}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.DeleteAlertRule(context.Background(), rule.ID) })

	source := &testSource{states: []ServerState{{ID: serverID, Connected: true, LastSeen: time.Now(), Containers: []ContainerState{{Name: "shop-web-1", State: "running", Stack: "shop"}}}}}
	clock := &testClock{now: time.Now()}
	engine := New(st, source, clock)
	// The worker shares the simulated clock, or its deliveries would not be due
	// yet when the engine (30 simulated seconds ahead) enqueues them.
	var clockMu sync.Mutex
	worker := notify.New(notify.Options{Store: st, Box: box, AllowPrivate: func() bool { return true },
		Clock: func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return clock.now }})
	go worker.Run(ctx)

	step := func(d time.Duration) {
		t.Helper()
		clockMu.Lock()
		clock.now = clock.now.Add(d)
		clockMu.Unlock()
		if err := engine.evaluate(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	step(0)

	// The container stops on its own: it must be noticed in under 30 seconds.
	source.states[0].Containers[0].State = "exited"
	for elapsed := time.Duration(0); elapsed < 30*time.Second; elapsed += 5 * time.Second {
		step(5 * time.Second)
	}
	worker.Wake()
	calls := recorder.wait(t, 1)
	var payload struct {
		Version int    `json:"version"`
		Event   string `json:"event"`
		Alert   struct {
			RuleType    string `json:"RuleType"`
			Severity    string `json:"Severity"`
			ServerName  string `json:"ServerName"`
			SubjectName string `json:"SubjectName"`
			Title       string `json:"Title"`
			Body        string `json:"Body"`
		} `json:"alert"`
	}
	if err := json.Unmarshal(calls[0].body, &payload); err != nil {
		t.Fatalf("webhook body is not JSON: %v: %s", err, calls[0].body)
	}
	if payload.Version != 1 || payload.Event != "firing" || payload.Alert.RuleType != "container_down" || payload.Alert.Severity != "critical" || payload.Alert.SubjectName != "shop-web-1" {
		t.Fatalf("firing payload = %+v", payload)
	}
	if !strings.Contains(payload.Alert.Body, "shop-web-1") || strings.Contains(payload.Alert.Body, "<no value>") {
		t.Fatalf("message body = %q", payload.Alert.Body)
	}
	mac := hmac.New(sha256.New, []byte(hmacSecret))
	mac.Write([]byte(calls[0].timestamp + "."))
	mac.Write(calls[0].body)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); calls[0].signature != want {
		t.Fatalf("signature = %q, want %q", calls[0].signature, want)
	}

	// Repeated evaluations while it keeps failing send nothing more.
	step(5 * time.Second)
	step(5 * time.Second)
	time.Sleep(300 * time.Millisecond)
	if got := len(recorder.wait(t, 1)); got != 1 {
		t.Fatalf("webhook calls while still firing = %d, want 1", got)
	}

	// The container runs again: the resolution arrives.
	source.states[0].Containers[0].State = "running"
	step(5 * time.Second)
	worker.Wake()
	calls = recorder.wait(t, 2)
	if err := json.Unmarshal(calls[1].body, &payload); err != nil || payload.Event != "resolved" {
		t.Fatalf("second call = %s (err %v), want a resolved event", calls[1].body, err)
	}
	active, err := st.ListActiveAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range active {
		if a.RuleID != nil && *a.RuleID == rule.ID {
			t.Fatalf("incident still open after recovery: %+v", a)
		}
	}
	reloaded, err := st.GetNotificationChannel(ctx, channel.ID)
	if err != nil || reloaded.LastOKAt == nil || reloaded.LastError != "" {
		t.Fatalf("channel status after delivery = %+v (err %v)", reloaded, err)
	}
}
