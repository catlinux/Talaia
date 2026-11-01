package api

import (
	"sync"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

const (
	// authzDenialWindow is the fixed window of one actor's denial budget.
	authzDenialWindow = 10 * time.Minute
	// authzDenialMaxRows is how many authz_denied rows an actor gets per window.
	authzDenialMaxRows = 30
	// authzDenialMaxActors caps the in-memory budget.
	authzDenialMaxActors = 10000
	// authzDenialSweepLimit bounds the opportunistic expired-entry sweep.
	authzDenialSweepLimit = 50

	authzDeniedAction           = "authz_denied"
	authzDeniedSuppressedAction = "authz_denied_suppressed"
)

type denialWindow struct {
	start      time.Time
	written    int
	suppressed int
	actor      string
	actorID    string
	ip         string
}

// denialBudget is the per-actor audit budget. Its mutex protects windows.
type denialBudget struct {
	mu      sync.Mutex
	windows map[string]*denialWindow
}

func newDenialBudget() denialBudget {
	return denialBudget{windows: map[string]*denialWindow{}}
}

// denialBudgetKey isolates API keys from their owner and from each other.
func denialBudgetKey(session Session) string {
	key := session.User.ID
	if session.AuthKind == AuthKindAPIKey {
		key += "/" + session.APIKeyID
	}
	return key
}

func (a *Authorizer) currentTime() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// recordAuthzDenial returns the summary rows to write before the denial, and
// whether the denial itself fits in the actor's budget.
func (a *Authorizer) recordAuthzDenial(session Session, ip string) ([]store.AuditEvent, bool) {
	return a.denials.record(
		a.currentTime(), denialBudgetKey(session),
		session.User.Username, session.User.ID, ip,
	)
}

// record consumes one denial. Callers append the returned summaries before the
// authz_denied row when write is true.
func (b *denialBudget) record(at time.Time, key, actor, actorID, ip string) ([]store.AuditEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.windows == nil {
		b.windows = map[string]*denialWindow{}
	}

	summaries := make([]store.AuditEvent, 0)
	if window, ok := b.windows[key]; ok {
		if !at.Before(window.start.Add(authzDenialWindow)) {
			if window.suppressed > 0 {
				summaries = append(summaries, suppressedDenialEvent(actor, actorID, ip, window.suppressed))
			}
			delete(b.windows, key)
		} else {
			window.actor = actor
			window.actorID = actorID
			window.ip = ip
			summaries = append(summaries, b.sweepLocked(at, key, authzDenialSweepLimit)...)
			if window.written < authzDenialMaxRows {
				window.written++
				return summaries, true
			}
			window.suppressed++
			return summaries, false
		}
	}

	wasFull := len(b.windows) >= authzDenialMaxActors
	summaries = append(summaries, b.sweepLocked(at, key, authzDenialSweepLimit)...)
	if wasFull {
		// A new actor found the cap full: sweep every expired entry before
		// deciding whether budget tracking is still possible.
		summaries = append(summaries, b.sweepLocked(at, key, 0)...)
	}
	if len(b.windows) >= authzDenialMaxActors {
		// Fail open for audit completeness: the caller writes the row without
		// tracking this actor.
		return summaries, true
	}
	b.windows[key] = &denialWindow{
		start: at, written: 1, actor: actor, actorID: actorID, ip: ip,
	}
	return summaries, true
}

// sweepLocked flushes expired windows other than exceptKey. A positive limit
// bounds the number of map entries examined in one call.
func (b *denialBudget) sweepLocked(at time.Time, exceptKey string, limit int) []store.AuditEvent {
	events := make([]store.AuditEvent, 0)
	examined := 0
	for key, window := range b.windows {
		if limit > 0 && examined >= limit {
			break
		}
		examined++
		if key == exceptKey || at.Before(window.start.Add(authzDenialWindow)) {
			continue
		}
		if window.suppressed > 0 {
			events = append(events, suppressedDenialEvent(
				window.actor, window.actorID, window.ip, window.suppressed,
			))
		}
		delete(b.windows, key)
	}
	return events
}

func suppressedDenialEvent(actor, actorID, ip string, suppressed int) store.AuditEvent {
	return store.AuditEvent{
		Actor:   actor,
		ActorID: actorID,
		Action:  authzDeniedSuppressedAction,
		IP:      ip,
		Result:  "denied",
		Details: map[string]any{
			"suppressed":     suppressed,
			"window_seconds": int(authzDenialWindow / time.Second),
		},
	}
}
