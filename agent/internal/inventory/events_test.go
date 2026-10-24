package inventory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchEventsReconnectsAfterStreamCloses(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events" {
			t.Errorf("path = %s", r.URL.Path)
		}
		requests.Add(1)
		if requests.Load() == 1 {
			_, _ = w.Write([]byte(`{"status":"start"}`))
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	docker := NewDockerWithClient(server.URL, server.Client())
	docker.eventBackoffBase = time.Millisecond
	docker.eventBackoffMax = 4 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { docker.WatchEvents(ctx, func() {}); close(done) }()
	deadline := time.After(time.Second)
	for requests.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("watcher did not reconnect")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop")
	}
}
