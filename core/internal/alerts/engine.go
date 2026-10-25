package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

type Store interface {
	ListAlertRules(context.Context) ([]store.AlertRule, error)
	OpenAlert(context.Context, store.Alert) (store.Alert, bool, error)
	ResolveAlert(context.Context, string, time.Time) error
	ListActiveAlerts(context.Context) ([]store.Alert, error)
	MarkAlertNotified(context.Context, string, time.Time) error
	UpsertWatch(context.Context, store.AlertWatch) error
	DeleteWatch(context.Context, string, string) error
	ListWatches(context.Context) ([]store.AlertWatch, error)
	CreateSilence(context.Context, store.Silence) (store.Silence, error)
	ListActiveSilences(context.Context, time.Time) ([]store.Silence, error)
	ListNotificationChannels(context.Context) ([]store.NotificationChannel, error)
	EnqueueDelivery(context.Context, store.NotificationDelivery) (store.NotificationDelivery, error)
	ListAlertChecks(context.Context) ([]store.AlertCheck, error)
	LatestAlertCheckResult(context.Context, string) (store.AlertCheckResult, error)
	SetAlertSilenced(context.Context, string, bool) error
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}
type Clock interface {
	Now() time.Time
	NewTicker(time.Duration) Ticker
}
type ticker struct{ *time.Ticker }

func (t ticker) C() <-chan time.Time { return t.Ticker.C }

type realClock struct{}

func (realClock) Now() time.Time                   { return time.Now() }
func (realClock) NewTicker(d time.Duration) Ticker { return ticker{time.NewTicker(d)} }

type ContainerState struct{ Name, State, Health, Stack string }
type CheckState struct {
	Check               store.AlertCheck
	Result              *store.AlertCheckResult
	ConsecutiveFailures int
}
type ServerState struct {
	ID, GroupID                            string
	Connected, Revoked                     bool
	LastSeen                               time.Time
	CPUPercent                             float64
	MemUsed, MemTotal, DiskUsed, DiskTotal uint64
	HasMetrics                             bool
	ContainerExists                        bool
	ContainerState                         string
	Health                                 string
	Containers                             []ContainerState
	Checks                                 []CheckState
}
type Source interface {
	Snapshot(context.Context) ([]ServerState, error)
}

type pending struct {
	since time.Time
	value float64
}
type Engine struct {
	mu          sync.Mutex
	store       Store
	source      Source
	clock       Clock
	pending     map[string]pending
	active      map[string]store.Alert
	cooldown    map[string]time.Time
	watches     map[string]time.Time
	watchWrites map[string]time.Time
	unwatched   map[string]time.Time
	started     time.Time
	checks      map[string]store.AlertCheckResult
	failures    map[string]int
	poke        chan string
}

func New(s Store, source Source, c Clock) *Engine {
	if c == nil {
		c = realClock{}
	}
	now := c.Now()
	return &Engine{store: s, source: source, clock: c, pending: map[string]pending{}, active: map[string]store.Alert{}, cooldown: map[string]time.Time{}, watches: map[string]time.Time{}, watchWrites: map[string]time.Time{}, unwatched: map[string]time.Time{}, checks: map[string]store.AlertCheckResult{}, failures: map[string]int{}, poke: make(chan string, 64), started: now}
}

func (e *Engine) Run(ctx context.Context) error {
	if e.store == nil || e.source == nil {
		return errors.New("alerts engine requires store and source")
	}
	e.mu.Lock()
	e.started = e.clock.Now()
	e.mu.Unlock()
	alerts, err := e.store.ListActiveAlerts(ctx)
	if err != nil {
		return fmt.Errorf("load active alerts: %w", err)
	}
	for _, a := range alerts {
		e.mu.Lock()
		e.active[a.Fingerprint] = a
		if a.LastNotifiedAt != nil {
			e.cooldown[a.Fingerprint] = *a.LastNotifiedAt
		}
		e.mu.Unlock()
	}
	watches, err := e.store.ListWatches(ctx)
	if err != nil {
		return fmt.Errorf("load alert watches: %w", err)
	}
	for _, w := range watches {
		e.mu.Lock()
		e.watches[w.ServerID+"\x00"+w.ContainerName] = w.LastRunningAt
		e.mu.Unlock()
	}
	t := e.clock.NewTicker(5 * time.Second)
	defer t.Stop()
	if err = e.evaluate(ctx, ""); err != nil {
		slog.Warn("initial alert evaluation", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C():
			if err := e.evaluate(ctx, ""); err != nil {
				slog.Error("evaluate alerts", "error", err)
			}
		case id := <-e.poke:
			if err := e.evaluate(ctx, id); err != nil {
				slog.Error("evaluate alerts", "server_id", id, "error", err)
			}
		}
	}
}

func (e *Engine) pokeServer(id string) {
	select {
	case e.poke <- id:
	default:
	}
}
func (e *Engine) InventoryChanged(id string)  { e.pokeServer(id) }
func (e *Engine) AgentDisconnected(id string) { e.pokeServer(id) }
func (e *Engine) CheckResult(v store.AlertCheckResult) {
	e.mu.Lock()
	e.checks[v.CheckID] = v
	if v.OK {
		e.failures[v.CheckID] = 0
	} else {
		e.failures[v.CheckID]++
	}
	e.mu.Unlock()
	e.pokeServer("")
}

func (e *Engine) evaluate(ctx context.Context, only string) error {
	now := e.clock.Now()
	storedActive, err := e.store.ListActiveAlerts(ctx)
	if err != nil {
		return fmt.Errorf("refresh active alerts: %w", err)
	}
	// The store is the source of truth for open incidents: an incident resolved
	// outside the engine (stop watching from the API) must not stay open here,
	// or it would hide the next incident of the same subject.
	fresh := make(map[string]store.Alert, len(storedActive))
	for _, a := range storedActive {
		fresh[a.Fingerprint] = a
	}
	e.mu.Lock()
	e.active = fresh
	e.mu.Unlock()
	rules, err := e.store.ListAlertRules(ctx)
	if err != nil {
		return fmt.Errorf("list rules: %w", err)
	}
	states, err := e.source.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("snapshot alert subjects: %w", err)
	}
	silences, err := e.store.ListActiveSilences(ctx, now)
	if err != nil {
		return fmt.Errorf("list silences: %w", err)
	}
	stateByID := make(map[string]ServerState, len(states))
	for _, s := range states {
		stateByID[s.ID] = s
		if only == "" || only == s.ID {
			if err := e.updateWatches(ctx, s, now); err != nil {
				return err
			}
		}
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if r.Type == "server_offline" && now.Sub(e.started) < 90*time.Second {
			continue
		}
		if r.Type == "check_failed" || r.Type == "cert_expiry" {
			if err := e.evaluateChecks(ctx, r, states, stateByID, now, silences); err != nil {
				return err
			}
			continue
		}
		for _, s := range states {
			if (only != "" && only != s.ID) || s.ID == "" || s.Revoked {
				continue
			}
			sub := Subject{ServerID: s.ID, GroupID: s.GroupID, Kind: "server", Name: s.ID}
			if !ruleMatches(r, sub) {
				continue
			}
			switch r.Type {
			case "server_offline":
				ok, _, dur := condition(r, s, nil, 0, now)
				if err := e.advance(ctx, r, sub, s, nil, ok, 0, dur, now, silences); err != nil {
					return err
				}
			case "cpu_high", "mem_high", "disk_high":
				// Without a sample (agent offline) the value is unknown: keep the
				// current state instead of resolving and firing again on
				// reconnect; server_offline already reports the outage.
				if !s.HasMetrics {
					continue
				}
				ok, val, dur := condition(r, s, nil, 0, now)
				e.mu.Lock()
				_, active := e.active[r.ID+":"+s.ID]
				e.mu.Unlock()
				if active && val >= paramsFor(r).Threshold-2 {
					ok = true
				}
				if err := e.advance(ctx, r, sub, s, nil, ok, val, dur, now, silences); err != nil {
					return err
				}
			case "container_down", "container_unhealthy":
				if err := e.evaluateContainers(ctx, r, s, now, silences); err != nil {
					return err
				}
			}
		}
	}
	return e.resolveOrphans(ctx, rules, stateByID, now)
}

func (e *Engine) updateWatches(ctx context.Context, s ServerState, now time.Time) error {
	present := make(map[string]string, len(s.Containers))
	for _, c := range s.Containers {
		present[c.Name] = c.State
	}
	e.mu.Lock()
	for key, since := range e.unwatched {
		if len(key) > len(s.ID) && key[:len(s.ID)+1] == s.ID+"\x00" {
			// The flag only covers the stop that Talaia asked for: once the
			// container is seen stopped, or if it still runs after the grace
			// period (the stop failed), it is watched again.
			if state, ok := present[key[len(s.ID)+1:]]; !ok || state != "running" || now.Sub(since) > unwatchGrace {
				delete(e.unwatched, key)
			}
		}
	}
	e.mu.Unlock()
	for _, c := range s.Containers {
		if c.Name == "" || c.State != "running" {
			continue
		}
		key := s.ID + "\x00" + c.Name
		e.mu.Lock()
		if _, skip := e.unwatched[key]; skip {
			e.mu.Unlock()
			continue
		}
		last, exists := e.watchWrites[key]
		if exists && now.Sub(last) < time.Minute {
			e.mu.Unlock()
			continue
		}
		e.watchWrites[key] = now
		e.watches[key] = now
		e.mu.Unlock()
		if err := e.store.UpsertWatch(ctx, store.AlertWatch{ServerID: s.ID, ContainerName: c.Name, LastRunningAt: now}); err != nil {
			return fmt.Errorf("upsert container watch: %w", err)
		}
	}
	return nil
}

func (e *Engine) evaluateContainers(ctx context.Context, r store.AlertRule, s ServerState, now time.Time, silences []store.Silence) error {
	if r.Type == "container_unhealthy" {
		for _, c := range s.Containers {
			sub := Subject{ServerID: s.ID, GroupID: s.GroupID, Stack: c.Stack, Container: c.Name, Kind: "container", Name: c.Name}
			if !ruleMatches(r, sub) {
				continue
			}
			state := s
			state.Health = c.Health
			ok, _, dur := condition(r, state, nil, 0, now)
			if err := e.advance(ctx, r, sub, state, nil, ok, 0, dur, now, silences); err != nil {
				return err
			}
		}
		return nil
	}
	e.mu.Lock()
	names := make([]string, 0)
	for key := range e.watches {
		if len(key) > len(s.ID) && key[:len(s.ID)+1] == s.ID+"\x00" {
			names = append(names, key[len(s.ID)+1:])
		}
	}
	e.mu.Unlock()
	for _, name := range names {
		var found *ContainerState
		for i := range s.Containers {
			if s.Containers[i].Name == name {
				found = &s.Containers[i]
				break
			}
		}
		sub := Subject{ServerID: s.ID, GroupID: s.GroupID, Kind: "container", Name: name, Container: name}
		if found != nil {
			sub.Stack = found.Stack
		}
		if !ruleMatches(r, sub) {
			continue
		}
		state := s
		state.ContainerExists = found != nil
		if found != nil {
			state.ContainerState = found.State
		}
		bad := found == nil || found.State != "running"
		if err := e.advance(ctx, r, sub, state, nil, bad, 0, time.Duration(paramsFor(r).Seconds)*time.Second, now, silences); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) evaluateChecks(ctx context.Context, r store.AlertRule, states []ServerState, byID map[string]ServerState, now time.Time, silences []store.Silence) error {
	checks, err := e.store.ListAlertChecks(ctx)
	if err != nil {
		return fmt.Errorf("list checks: %w", err)
	}
	for _, ch := range checks {
		if !ch.Enabled {
			continue
		}
		var cr *store.AlertCheckResult
		e.mu.Lock()
		got, ok := e.checks[ch.ID]
		e.mu.Unlock()
		if ok {
			cr = &got
		} else if got, er := e.store.LatestAlertCheckResult(ctx, ch.ID); er == nil {
			cr = &got
		}
		state := ServerState{}
		if ch.ServerID != nil {
			state = byID[*ch.ServerID]
		}
		if cr == nil {
			for _, observed := range state.Checks {
				if observed.Check.ID == ch.ID {
					if observed.Result != nil {
						result := *observed.Result
						cr = &result
					}
					break
				}
			}
		}
		sub := Subject{ServerID: state.ID, GroupID: state.GroupID, CheckID: ch.ID, Kind: "check", Name: ch.ID}
		if !ruleMatches(r, sub) {
			continue
		}
		e.mu.Lock()
		fails := e.failures[ch.ID]
		e.mu.Unlock()
		for _, observed := range state.Checks {
			if observed.Check.ID == ch.ID {
				fails = observed.ConsecutiveFailures
				break
			}
		}
		if fails == 0 && cr != nil && !cr.OK {
			fails = 1
		}
		cond, _, dur := condition(r, state, cr, fails, now)
		if err := e.advance(ctx, r, sub, state, cr, cond, 0, dur, now, silences); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) advance(ctx context.Context, r store.AlertRule, sub Subject, s ServerState, check *store.AlertCheckResult, condition bool, value float64, dur time.Duration, now time.Time, silences []store.Silence) error {
	fp := fingerprint(r.ID, sub)
	e.mu.Lock()
	a, active := e.active[fp]
	p, waiting := e.pending[fp]
	e.mu.Unlock()
	isSilenced := false
	for _, silence := range silences {
		if silenceMatches(silence, r, sub) {
			isSilenced = true
			break
		}
	}
	if active && a.Silenced != isSilenced {
		if err := e.store.SetAlertSilenced(ctx, a.ID, isSilenced); err != nil {
			return fmt.Errorf("update alert silence: %w", err)
		}
		a.Silenced = isSilenced
		e.mu.Lock()
		e.active[fp] = a
		e.mu.Unlock()
	}
	if !condition {
		deleteSafe(e, fp)
		if active {
			if err := e.store.ResolveAlert(ctx, a.ID, now); err != nil {
				return fmt.Errorf("resolve alert: %w", err)
			}
			e.mu.Lock()
			delete(e.active, fp)
			e.mu.Unlock()
			if err := e.enqueue(ctx, a, r, sub, "resolved", now, a.Silenced); err != nil {
				return err
			}
		}
		return nil
	}
	if !active {
		if !waiting {
			e.mu.Lock()
			e.pending[fp] = pending{since: now, value: value}
			e.mu.Unlock()
			if dur > 0 {
				return nil
			}
		} else if now.Sub(p.since) < dur {
			return nil
		}
		params := map[string]any{}
		if value != 0 {
			params["value"] = value
		}
		alert := store.Alert{RuleID: &r.ID, RuleType: r.Type, Severity: r.Severity, Fingerprint: fp, ServerID: stringPtr(sub.ServerID), SubjectKind: sub.Kind, SubjectName: sub.Name, Stack: sub.Stack, State: "firing", Summary: makeSummary("alerts."+r.Type, params), StartedAt: now, Silenced: isSilenced}
		opened, created, err := e.store.OpenAlert(ctx, alert)
		if err != nil {
			return fmt.Errorf("open alert: %w", err)
		}
		e.mu.Lock()
		delete(e.pending, fp)
		lastNotified, hasNotified := e.cooldown[fp]
		e.mu.Unlock()
		if created {
			e.mu.Lock()
			e.active[fp] = opened
			e.mu.Unlock()
			cooled := false
			if hasNotified && now.Sub(lastNotified) < 10*time.Minute {
				cooled = true
			}
			if err = e.enqueue(ctx, opened, r, sub, "firing", now, cooled); err != nil {
				return err
			}
		}
		return nil
	}
	if r.RepeatIntervalSeconds > 0 && a.AckedAt == nil && a.LastNotifiedAt != nil && now.Sub(*a.LastNotifiedAt) >= time.Duration(r.RepeatIntervalSeconds)*time.Second {
		if err := e.enqueue(ctx, a, r, sub, "firing", now, isSilenced); err != nil {
			return err
		}
	}
	return nil
}

// unwatchGrace is how long a container stopped from Talaia may keep running
// before it is watched again.
const unwatchGrace = 2 * time.Minute

// fingerprint identifies one incident: the rule plus its subject. Container
// names repeat across servers, so a container is keyed by server and name.
func fingerprint(ruleID string, sub Subject) string {
	if sub.Kind == "container" {
		return ruleID + ":" + sub.ServerID + "/" + sub.Name
	}
	return ruleID + ":" + sub.Name
}

func deleteSafe(e *Engine, fp string) { e.mu.Lock(); delete(e.pending, fp); e.mu.Unlock() }
func stringPtr(v string) *string      { return &v }

func (e *Engine) resolveOrphans(ctx context.Context, rules []store.AlertRule, states map[string]ServerState, now time.Time) error {
	enabled := map[string]bool{}
	for _, r := range rules {
		if r.Enabled {
			enabled[r.ID] = true
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for fp, a := range e.active {
		if a.RuleID == nil || !enabled[*a.RuleID] {
			if err := e.store.ResolveAlert(ctx, a.ID, now); err != nil {
				return fmt.Errorf("resolve orphan alert: %w", err)
			}
			delete(e.active, fp)
			continue
		}
		if a.ServerID != nil && *a.ServerID != "" {
			if _, ok := states[*a.ServerID]; !ok {
				if err := e.store.ResolveAlert(ctx, a.ID, now); err != nil {
					return fmt.Errorf("resolve vanished server alert: %w", err)
				}
				delete(e.active, fp)
			}
		}
	}
	return nil
}
