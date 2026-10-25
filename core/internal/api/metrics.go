package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/go-chi/chi/v5"
)

type MetricsReader interface {
	QueryServerSeries(context.Context, string, time.Time, time.Time, store.Resolution) ([]store.ServerMetricPoint, error)
	QueryContainerSeries(context.Context, string, string, time.Time, time.Time, store.Resolution) ([]store.ContainerMetricPoint, error)
}

type MetricsDeps struct {
	Reader  MetricsReader
	Servers ServerGetter
	Agents  LiveAgents
	Now     func() time.Time
}

type metricsResponse struct {
	Resolution store.Resolution `json:"resolution"`
	From       time.Time        `json:"from"`
	To         time.Time        `json:"to"`
	Points     any              `json:"points"`
}

type serverMetricJSON struct {
	Time        time.Time `json:"time"`
	CPU         *float32  `json:"cpu"`
	CPUMax      *float32  `json:"cpu_max,omitempty"`
	MemUsed     *float64  `json:"mem_used"`
	MemUsedMax  *int64    `json:"mem_used_max,omitempty"`
	MemTotal    *int64    `json:"mem_total"`
	DiskUsed    *float64  `json:"disk_used"`
	DiskUsedMax *int64    `json:"disk_used_max,omitempty"`
	DiskTotal   *int64    `json:"disk_total"`
	NetRxBPS    *float64  `json:"net_rx_bps"`
	NetTxBPS    *float64  `json:"net_tx_bps"`
}

type containerMetricJSON struct {
	Time     time.Time `json:"time"`
	CPU      *float32  `json:"cpu"`
	CPUMax   *float32  `json:"cpu_max,omitempty"`
	MemUsed  *float64  `json:"mem_used"`
	MemLimit *int64    `json:"mem_limit"`
}

func NewServerMetricsHandler(deps MetricsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")
		if deps.Servers != nil {
			if _, err := deps.Servers.GetServer(r.Context(), serverID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					writeError(w, http.StatusNotFound, "not_found")
				} else {
					internal(w, "get metrics server", err)
				}
				return
			}
		}
		from, to, ok := metricsRange(w, r, metricsNow(deps))
		if !ok {
			return
		}
		resolution := store.ResolutionFor(from, to)
		if strings.HasPrefix(serverID, devServerPrefix) {
			writeMetrics(w, r, resolution, from, to, []serverMetricJSON{})
			return
		}
		points, err := deps.Reader.QueryServerSeries(r.Context(), serverID, from, to, resolution)
		if err != nil {
			internal(w, "query server metrics", err)
			return
		}
		out := make([]serverMetricJSON, 0, len(points))
		for _, p := range points {
			out = append(out, serverMetricJSON{p.Time, p.CPUAvg, p.CPUMax, p.MemUsedAvg, p.MemUsedMax, p.MemTotal, p.DiskUsedAvg, p.DiskUsedMax, p.DiskTotal, p.NetRxBPSAvg, p.NetTxBPSAvg})
		}
		writeMetrics(w, r, resolution, from, to, thinServer(out))
	})
}

func NewContainerMetricsHandler(deps MetricsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID, reference := chi.URLParam(r, "serverID"), chi.URLParam(r, "container")
		if deps.Servers != nil {
			if _, err := deps.Servers.GetServer(r.Context(), serverID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					writeError(w, http.StatusNotFound, "not_found")
				} else {
					internal(w, "get metrics server", err)
				}
				return
			}
		}
		from, to, ok := metricsRange(w, r, metricsNow(deps))
		if !ok {
			return
		}
		name := reference
		if agent, connected := connectedAgent(deps.Agents, serverID); connected {
			found := false
			for _, container := range agent.ContainerList {
				if container.GetId() == reference || container.GetName() == reference {
					name, found = container.GetName(), true
					base, err := targetFromRequest(r, targetServer)
					if err != nil {
						internal(w, "build container metrics target", err)
						return
					}
					effective, hasEffective := effectiveFromRequest(r)
					if !hasEffective || !containerVisible(effective, base, container) {
						writeError(w, http.StatusNotFound, "not_found")
						return
					}
					break
				}
			}
			if !found {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
		}
		resolution := store.ResolutionFor(from, to)
		if strings.HasPrefix(serverID, devServerPrefix) {
			writeMetrics(w, r, resolution, from, to, []containerMetricJSON{})
			return
		}
		points, err := deps.Reader.QueryContainerSeries(r.Context(), serverID, name, from, to, resolution)
		if err != nil {
			internal(w, "query container metrics", err)
			return
		}
		out := make([]containerMetricJSON, 0, len(points))
		for _, p := range points {
			out = append(out, containerMetricJSON{p.Time, p.CPUAvg, p.CPUMax, p.MemUsedAvg, p.MemLimit})
		}
		writeMetrics(w, r, resolution, from, to, thinContainer(out))
	})
}

func metricsNow(deps MetricsDeps) time.Time {
	if deps.Now != nil {
		return deps.Now()
	}
	return time.Now()
}

func metricsRange(w http.ResponseWriter, r *http.Request, now time.Time) (time.Time, time.Time, bool) {
	q := r.URL.Query()
	if q.Has("range") && (q.Has("from") || q.Has("to")) {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return time.Time{}, time.Time{}, false
	}
	to := now
	if raw := q.Get("to"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return time.Time{}, time.Time{}, false
		}
		to = parsed
	}
	from := to.Add(-time.Hour)
	if raw := q.Get("from"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return time.Time{}, time.Time{}, false
		}
		from = parsed
	} else if raw := q.Get("range"); raw != "" {
		durations := map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}
		duration, ok := durations[raw]
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return time.Time{}, time.Time{}, false
		}
		from = to.Add(-duration)
	} else if q.Has("range") {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return time.Time{}, time.Time{}, false
	}
	if !from.Before(to) || to.Sub(from) > 31*24*time.Hour {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func writeMetrics(w http.ResponseWriter, r *http.Request, resolution store.Resolution, from, to time.Time, points any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, metricsResponse{resolution, from, to, points})
}

func thinServer(points []serverMetricJSON) []serverMetricJSON {
	if len(points) <= 500 {
		return points
	}
	result := make([]serverMetricJSON, 0, 500)
	for group := 0; group < 500; group++ {
		start, end := group*len(points)/500, (group+1)*len(points)/500
		p := serverMetricJSON{Time: midpoint(points[start].Time, points[end-1].Time), CPU: avg32(points[start:end], func(p serverMetricJSON) *float32 { return p.CPU }), CPUMax: avg32(points[start:end], func(p serverMetricJSON) *float32 { return p.CPUMax }), MemUsed: avg64(points[start:end], func(p serverMetricJSON) *float64 { return p.MemUsed }), MemUsedMax: avgInt(points[start:end], func(p serverMetricJSON) *int64 { return p.MemUsedMax }), MemTotal: avgInt(points[start:end], func(p serverMetricJSON) *int64 { return p.MemTotal }), DiskUsed: avg64(points[start:end], func(p serverMetricJSON) *float64 { return p.DiskUsed }), DiskUsedMax: avgInt(points[start:end], func(p serverMetricJSON) *int64 { return p.DiskUsedMax }), DiskTotal: avgInt(points[start:end], func(p serverMetricJSON) *int64 { return p.DiskTotal }), NetRxBPS: avg64(points[start:end], func(p serverMetricJSON) *float64 { return p.NetRxBPS }), NetTxBPS: avg64(points[start:end], func(p serverMetricJSON) *float64 { return p.NetTxBPS })}
		result = append(result, p)
	}
	return result
}

func thinContainer(points []containerMetricJSON) []containerMetricJSON {
	if len(points) <= 500 {
		return points
	}
	result := make([]containerMetricJSON, 0, 500)
	for group := 0; group < 500; group++ {
		start, end := group*len(points)/500, (group+1)*len(points)/500
		chunk := points[start:end]
		result = append(result, containerMetricJSON{Time: midpoint(chunk[0].Time, chunk[len(chunk)-1].Time), CPU: avg32(chunk, func(p containerMetricJSON) *float32 { return p.CPU }), CPUMax: avg32(chunk, func(p containerMetricJSON) *float32 { return p.CPUMax }), MemUsed: avg64(chunk, func(p containerMetricJSON) *float64 { return p.MemUsed }), MemLimit: avgInt(chunk, func(p containerMetricJSON) *int64 { return p.MemLimit })})
	}
	return result
}

func midpoint(a, b time.Time) time.Time { return a.Add(b.Sub(a) / 2) }
func avg32[T any](items []T, get func(T) *float32) *float32 {
	var sum float64
	n := 0
	for _, item := range items {
		if v := get(item); v != nil {
			sum += float64(*v)
			n++
		}
	}
	if n == 0 {
		return nil
	}
	value := float32(sum / float64(n))
	return &value
}
func avg64[T any](items []T, get func(T) *float64) *float64 {
	var sum float64
	n := 0
	for _, item := range items {
		if v := get(item); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	value := sum / float64(n)
	return &value
}
func avgInt[T any](items []T, get func(T) *int64) *int64 {
	var sum int64
	n := 0
	for _, item := range items {
		if v := get(item); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	value := sum / int64(n)
	return &value
}
