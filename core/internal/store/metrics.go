package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type MetricServerSample struct {
	Time                                   time.Time
	ServerID                               string
	CPUPct                                 *float32
	MemUsed, MemTotal, DiskUsed, DiskTotal *int64
	NetRxBPS, NetTxBPS                     *float64
}

type MetricContainerSample struct {
	Time                           time.Time
	ServerID, ContainerName, Stack string
	CPUPct                         *float32
	MemUsed, MemLimit              *int64
}

type Resolution string

const (
	ResolutionRaw Resolution = "raw"
	Resolution5m  Resolution = "5m"
	Resolution1h  Resolution = "1h"
)

type ServerMetricPoint struct {
	Time                                         time.Time
	CPUAvg, CPUMax                               *float32
	MemUsedAvg, DiskUsedAvg                      *float64
	MemUsedMax, MemTotal, DiskUsedMax, DiskTotal *int64
	NetRxBPSAvg, NetTxBPSAvg                     *float64
}

type ContainerMetricPoint struct {
	Time                 time.Time
	CPUAvg, CPUMax       *float32
	MemUsedAvg           *float64
	MemUsedMax, MemLimit *int64
}

func ResolutionFor(from, to time.Time) Resolution {
	d := to.Sub(from)
	if d <= 6*time.Hour {
		return ResolutionRaw
	}
	if d <= 7*24*time.Hour {
		return Resolution5m
	}
	return Resolution1h
}

func (s *Store) InsertServerSamples(ctx context.Context, samples []MetricServerSample) error {
	rows := make([][]any, 0, len(samples))
	for _, v := range samples {
		rows = append(rows, []any{v.Time, v.ServerID, v.CPUPct, v.MemUsed, v.MemTotal, v.DiskUsed, v.DiskTotal, v.NetRxBPS, v.NetTxBPS})
	}
	if len(rows) == 0 {
		return nil
	}
	_, err := s.pool.CopyFrom(ctx, pgx.Identifier{"metric_server"}, []string{"time", "server_id", "cpu_pct", "mem_used", "mem_total", "disk_used", "disk_total", "net_rx_bps", "net_tx_bps"}, pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("insert server metrics: %w", err)
	}
	return nil
}

func (s *Store) InsertContainerSamples(ctx context.Context, samples []MetricContainerSample) error {
	rows := make([][]any, 0, len(samples))
	for _, v := range samples {
		rows = append(rows, []any{v.Time, v.ServerID, v.ContainerName, v.Stack, v.CPUPct, v.MemUsed, v.MemLimit})
	}
	if len(rows) == 0 {
		return nil
	}
	_, err := s.pool.CopyFrom(ctx, pgx.Identifier{"metric_container"}, []string{"time", "server_id", "container_name", "stack", "cpu_pct", "mem_used", "mem_limit"}, pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("insert container metrics: %w", err)
	}
	return nil
}

func (s *Store) DownsampleServer(ctx context.Context, from, to Resolution, until time.Time) error {
	fromTable, interval, err := downsampleSource(from, to)
	if err != nil {
		return err
	}
	toTable := "metric_server_" + string(to)
	query := fmt.Sprintf(`INSERT INTO %s (time, server_id, cpu_avg, cpu_max, mem_used_avg, mem_used_max, mem_total, disk_used_avg, disk_used_max, disk_total, net_rx_bps_avg, net_tx_bps_avg)
SELECT bucket, server_id, avg(cpu_pct), max(cpu_pct), avg(mem_used)::double precision, max(mem_used), max(mem_total), avg(disk_used)::double precision, max(disk_used), max(disk_total), avg(net_rx_bps), avg(net_tx_bps)
FROM (SELECT time_bucket('%s', time) bucket, * FROM %s) m
GROUP BY bucket, server_id HAVING bucket + interval '%s' <= $1
ON CONFLICT (server_id, time) DO NOTHING`, toTable, interval, fromTable, interval)
	if _, err := s.pool.Exec(ctx, query, until); err != nil {
		return fmt.Errorf("downsample server metrics: %w", err)
	}
	return nil
}

func (s *Store) DownsampleContainer(ctx context.Context, from, to Resolution, until time.Time) error {
	fromTable, interval, err := downsampleSource(from, to)
	if err != nil {
		return err
	}
	fromTable = strings.Replace(fromTable, "metric_server", "metric_container", 1)
	toTable := "metric_container_" + string(to)
	query := fmt.Sprintf(`INSERT INTO %s (time, server_id, container_name, stack, cpu_avg, cpu_max, mem_used_avg, mem_used_max, mem_limit)
SELECT bucket, server_id, container_name, max(stack), avg(cpu_pct), max(cpu_pct), avg(mem_used)::double precision, max(mem_used), max(mem_limit)
FROM (SELECT time_bucket('%s', time) bucket, * FROM %s) m
GROUP BY bucket, server_id, container_name HAVING bucket + interval '%s' <= $1
ON CONFLICT (server_id, container_name, time) DO NOTHING`, toTable, interval, fromTable, interval)
	if _, err := s.pool.Exec(ctx, query, until); err != nil {
		return fmt.Errorf("downsample container metrics: %w", err)
	}
	return nil
}

func downsampleSource(from, to Resolution) (string, string, error) {
	if from == ResolutionRaw && to == Resolution5m {
		return "metric_server", "5 minutes", nil
	}
	if from == Resolution5m && to == Resolution1h {
		return "metric_server_5m", "1 hour", nil
	}
	return "", "", fmt.Errorf("unsupported metric downsampling %q to %q", from, to)
}

func (s *Store) DropOldMetrics(ctx context.Context, table string, olderThan time.Time) error {
	allowed := map[string]bool{"metric_server": true, "metric_server_5m": true, "metric_server_1h": true, "metric_container": true, "metric_container_5m": true, "metric_container_1h": true}
	if !allowed[table] {
		return fmt.Errorf("invalid metrics table %q", table)
	}
	if _, err := s.pool.Exec(ctx, `SELECT drop_chunks($1::regclass, older_than => $2::timestamptz)`, table, olderThan); err != nil {
		return fmt.Errorf("drop old metrics from %s: %w", table, err)
	}
	return nil
}

func (s *Store) DeleteServerMetrics(ctx context.Context, serverID string) error {
	for _, table := range []string{"metric_server", "metric_server_5m", "metric_server_1h", "metric_container", "metric_container_5m", "metric_container_1h"} {
		if _, err := s.pool.Exec(ctx, "DELETE FROM "+table+" WHERE server_id = $1", serverID); err != nil {
			return fmt.Errorf("delete server metrics from %s: %w", table, err)
		}
	}
	return nil
}

func (s *Store) QueryServerSeries(ctx context.Context, serverID string, from, to time.Time, resolution Resolution) ([]ServerMetricPoint, error) {
	table, err := metricTable("metric_server", resolution)
	if err != nil {
		return nil, err
	}
	columns := "time, cpu_pct, cpu_pct, mem_used::double precision, mem_used, mem_total, disk_used::double precision, disk_used, disk_total, net_rx_bps, net_tx_bps"
	if resolution != ResolutionRaw {
		columns = "time, cpu_avg, cpu_max, mem_used_avg, mem_used_max, mem_total, disk_used_avg, disk_used_max, disk_total, net_rx_bps_avg, net_tx_bps_avg"
	}
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM "+table+" WHERE server_id=$1 AND time >= $2 AND time <= $3 ORDER BY time", serverID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query server metric series: %w", err)
	}
	defer rows.Close()
	var result []ServerMetricPoint
	for rows.Next() {
		var p ServerMetricPoint
		if err := rows.Scan(&p.Time, &p.CPUAvg, &p.CPUMax, &p.MemUsedAvg, &p.MemUsedMax, &p.MemTotal, &p.DiskUsedAvg, &p.DiskUsedMax, &p.DiskTotal, &p.NetRxBPSAvg, &p.NetTxBPSAvg); err != nil {
			return nil, fmt.Errorf("scan server metric: %w", err)
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read server metric series: %w", err)
	}
	return result, nil
}

func (s *Store) QueryContainerSeries(ctx context.Context, serverID, name string, from, to time.Time, resolution Resolution) ([]ContainerMetricPoint, error) {
	table, err := metricTable("metric_container", resolution)
	if err != nil {
		return nil, err
	}
	columns := "time, cpu_pct, cpu_pct, mem_used::double precision, mem_used, mem_limit"
	if resolution != ResolutionRaw {
		columns = "time, cpu_avg, cpu_max, mem_used_avg, mem_used_max, mem_limit"
	}
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM "+table+" WHERE server_id=$1 AND container_name=$2 AND time >= $3 AND time <= $4 ORDER BY time", serverID, name, from, to)
	if err != nil {
		return nil, fmt.Errorf("query container metric series: %w", err)
	}
	defer rows.Close()
	var result []ContainerMetricPoint
	for rows.Next() {
		var p ContainerMetricPoint
		if err := rows.Scan(&p.Time, &p.CPUAvg, &p.CPUMax, &p.MemUsedAvg, &p.MemUsedMax, &p.MemLimit); err != nil {
			return nil, fmt.Errorf("scan container metric: %w", err)
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read container metric series: %w", err)
	}
	return result, nil
}

func metricTable(base string, r Resolution) (string, error) {
	switch r {
	case ResolutionRaw:
		return base, nil
	case Resolution5m:
		return base + "_5m", nil
	case Resolution1h:
		return base + "_1h", nil
	default:
		return "", errors.New("invalid metric resolution")
	}
}
