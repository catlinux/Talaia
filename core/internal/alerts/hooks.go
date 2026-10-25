package alerts

import (
	"context"
	"fmt"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

func (e *Engine) Unwatch(ctx context.Context, serverID, container string) {
	e.mu.Lock()
	key := serverID + "\x00" + container
	delete(e.watches, key)
	delete(e.watchWrites, key)
	e.unwatched[key] = e.clock.Now()
	e.mu.Unlock()
	_ = e.store.DeleteWatch(ctx, serverID, container)
}

func (e *Engine) UnwatchStack(ctx context.Context, serverID, stack string) {
	if e.source == nil {
		return
	}
	states, err := e.source.Snapshot(ctx)
	if err != nil {
		return
	}
	for _, s := range states {
		if s.ID != serverID {
			continue
		}
		for _, c := range s.Containers {
			if c.Stack == stack {
				e.Unwatch(ctx, serverID, c.Name)
			}
		}
	}
}

func (e *Engine) SilenceServerFor(ctx context.Context, serverID string, duration time.Duration, reason string) error {
	if duration <= 0 || duration > 30*24*time.Hour {
		return fmt.Errorf("invalid silence duration")
	}
	_, err := e.store.CreateSilence(ctx, store.Silence{ScopeType: "server", ScopeServerID: &serverID, Reason: reason, CreatedBy: "system", EndsAt: e.clock.Now().Add(duration)})
	if err != nil {
		return fmt.Errorf("create server silence: %w", err)
	}
	e.pokeServer(serverID)
	return nil
}

func (e *Engine) SilenceServer(ctx context.Context, serverID string, duration time.Duration, reason string) {
	_ = e.SilenceServerFor(ctx, serverID, duration, reason)
}
