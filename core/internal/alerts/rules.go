package alerts

import (
	"encoding/json"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

type ruleParams struct {
	Seconds             int     `json:"seconds"`
	Threshold           float64 `json:"threshold"`
	ForSeconds          int     `json:"for_seconds"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	Days                int     `json:"days"`
}

func paramsFor(r store.AlertRule) ruleParams {
	var p ruleParams
	_ = json.Unmarshal(r.Params, &p)
	return p
}

func condition(r store.AlertRule, s ServerState, check *store.AlertCheckResult, failures int, now time.Time) (bool, float64, time.Duration) {
	p := paramsFor(r)
	switch r.Type {
	case "server_offline":
		return !s.Connected && !s.LastSeen.IsZero() && now.Sub(s.LastSeen) >= time.Duration(p.Seconds)*time.Second, 0, 0
	case "cpu_high":
		return s.HasMetrics && s.CPUPercent >= p.Threshold, s.CPUPercent, time.Duration(p.ForSeconds) * time.Second
	case "mem_high":
		if s.MemTotal == 0 {
			return false, 0, time.Duration(p.ForSeconds) * time.Second
		}
		v := float64(s.MemUsed) * 100 / float64(s.MemTotal)
		return s.HasMetrics && v >= p.Threshold, v, time.Duration(p.ForSeconds) * time.Second
	case "disk_high":
		if s.DiskTotal == 0 {
			return false, 0, time.Duration(p.ForSeconds) * time.Second
		}
		v := float64(s.DiskUsed) * 100 / float64(s.DiskTotal)
		return v >= p.Threshold, v, time.Duration(p.ForSeconds) * time.Second
	case "container_down":
		return s.ContainerExists && s.ContainerState != "running", 0, time.Duration(p.Seconds) * time.Second
	case "container_unhealthy":
		return s.Health == "unhealthy", 0, time.Duration(p.Seconds) * time.Second
	case "check_failed":
		return check != nil && !check.OK && failures >= p.ConsecutiveFailures, 0, 0
	case "cert_expiry":
		return check != nil && check.CertNotAfter != nil && check.CertNotAfter.Sub(now) <= time.Duration(p.Days)*24*time.Hour, 0, 0
	default:
		return false, 0, 0
	}
}
