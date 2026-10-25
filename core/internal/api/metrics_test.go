package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/go-chi/chi/v5"
)

type fakeMetricsReader struct {
	serverPoints []store.ServerMetricPoint
}

func (f fakeMetricsReader) QueryServerSeries(_ context.Context, _ string, _, _ time.Time, _ store.Resolution) ([]store.ServerMetricPoint, error) {
	return f.serverPoints, nil
}

func (f fakeMetricsReader) QueryContainerSeries(context.Context, string, string, time.Time, time.Time, store.Resolution) ([]store.ContainerMetricPoint, error) {
	return nil, nil
}

func TestServerMetricsRangeAndPointLimit(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		rangeName string
		want      store.Resolution
	}{{"1h", store.ResolutionRaw}, {"6h", store.ResolutionRaw}, {"24h", store.Resolution5m}, {"7d", store.Resolution5m}, {"30d", store.Resolution1h}} {
		t.Run(tt.rangeName, func(t *testing.T) {
			reader := fakeMetricsReader{}
			router := chi.NewRouter()
			router.Get("/servers/{serverID}/metrics", NewServerMetricsHandler(MetricsDeps{Reader: reader, Now: func() time.Time { return now }}).ServeHTTP)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/servers/s1/metrics?range="+tt.rangeName, nil))
			if recorder.Code != 200 {
				t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
			}
			var body metricsResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Resolution != tt.want {
				t.Fatalf("resolution %s, want %s", body.Resolution, tt.want)
			}
		})
	}
	points := make([]store.ServerMetricPoint, 1201)
	for i := range points {
		points[i].Time = now.Add(time.Duration(i) * time.Second)
	}
	router := chi.NewRouter()
	router.Get("/servers/{serverID}/metrics", NewServerMetricsHandler(MetricsDeps{Reader: fakeMetricsReader{serverPoints: points}, Now: func() time.Time { return now }}).ServeHTTP)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/servers/s1/metrics", nil))
	var body metricsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := len(body.Points.([]any)); got > 500 {
		t.Fatalf("got %d points", got)
	}
}

func TestServerMetricsRejectsInvalidRanges(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/servers/{serverID}/metrics", NewServerMetricsHandler(MetricsDeps{Reader: fakeMetricsReader{}}).ServeHTTP)
	for _, path := range []string{"/servers/s1/metrics?range=2h", "/servers/s1/metrics?from=bad", "/servers/s1/metrics?from=2026-10-24T00:00:00Z&to=2026-11-25T00:00:00Z", "/servers/s1/metrics?from=2026-10-25T00:00:00Z&to=2026-10-24T00:00:00Z"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != 400 {
			t.Errorf("%s: status %d, want 400", path, recorder.Code)
		}
	}
}
