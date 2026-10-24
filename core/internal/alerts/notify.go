package alerts

import (
	"context"
	"encoding/json"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

type summary struct {
	Key    string         `json:"key"`
	Params map[string]any `json:"params"`
}

func (e *Engine) enqueue(ctx context.Context, alert store.Alert, rule store.AlertRule, subject Subject, event string, now time.Time, cooled bool) error {
	silenced := false
	silences, err := e.store.ListActiveSilences(ctx, now)
	if err != nil {
		return err
	}
	for _, silence := range silences {
		if silenceMatches(silence, rule, subject) {
			silenced = true
			break
		}
	}
	alert.Silenced = silenced
	if silenced || cooled {
		return nil
	}
	channels, err := e.store.ListNotificationChannels(ctx)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, id := range rule.ChannelIDs {
		allowed[id] = true
	}
	enqueued := false
	for _, channel := range channels {
		if !channel.Enabled || (len(rule.ChannelIDs) != 0 && !allowed[channel.ID]) {
			continue
		}
		alertID, channelID := alert.ID, channel.ID
		if _, err := e.store.EnqueueDelivery(ctx, store.NotificationDelivery{AlertID: &alertID, ChannelID: &channelID, Event: event, NextAttemptAt: now}); err != nil {
			return err
		}
		enqueued = true
	}
	if enqueued {
		if err := e.store.MarkAlertNotified(ctx, alert.ID, now); err != nil {
			return err
		}
		e.mu.Lock()
		e.cooldown[alert.Fingerprint] = now
		if current, ok := e.active[alert.Fingerprint]; ok {
			current.LastNotifiedAt = &now
			e.active[alert.Fingerprint] = current
		}
		e.mu.Unlock()
	}
	return nil
}

func makeSummary(key string, params map[string]any) json.RawMessage {
	b, _ := json.Marshal(summary{Key: key, Params: params})
	return b
}
