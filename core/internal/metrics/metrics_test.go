package metrics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

type fakeStore struct {
	servers []store.MetricServerSample
	fail    bool
}

func (f *fakeStore) InsertServerSamples(_ context.Context, values []store.MetricServerSample) error {
	if f.fail {
		return errors.New("db")
	}
	f.servers = append(f.servers, values...)
	return nil
}
func (f *fakeStore) InsertContainerSamples(context.Context, []store.MetricContainerSample) error {
	if f.fail {
		return errors.New("db")
	}
	return nil
}
func (*fakeStore) DownsampleServer(context.Context, store.Resolution, store.Resolution, time.Time) error {
	return nil
}
func (*fakeStore) DownsampleContainer(context.Context, store.Resolution, store.Resolution, time.Time) error {
	return nil
}
func (*fakeStore) DropOldMetrics(context.Context, string, time.Time) error { return nil }

func TestRecorderBatchAndError(t *testing.T) {
	f := &fakeStore{}
	r := NewRecorder(f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.AddServerSample(store.MetricServerSample{ServerID: "batch"})
	r.flush(context.Background())
	if len(f.servers) != 1 {
		t.Fatalf("batch size = %d", len(f.servers))
	}
	f.fail = true
	r.AddServerSample(store.MetricServerSample{ServerID: "dropped"})
	r.flush(context.Background())
	if len(r.servers) != 0 {
		t.Fatal("failed batch retained")
	}
}

func TestRecorderCapDropsOldest(t *testing.T) {
	r := NewRecorder(&fakeStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < MaxSamples+1; i++ {
		r.AddServerSample(store.MetricServerSample{ServerID: string(rune(i + 1))})
	}
	if len(r.servers) != MaxSamples || r.servers[0].ServerID == string(rune(1)) {
		t.Fatalf("cap or oldest sample incorrect")
	}
}

func TestForbiddenTimescaleFeatures(t *testing.T) {
	files := []string{"../store/migrations/0014_metrics.sql", "../store/metrics.go", "recorder.go", "maintenance.go"}
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(data))
		for _, forbidden := range []string{"add_retention_" + "policy", "add_continuous_aggregate_" + "policy", "add_com" + "pression_policy", "add_reorder_" + "policy", "timescaledb." + "continuous", "com" + "press"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains forbidden feature %q", name, forbidden)
			}
		}
	}
}
