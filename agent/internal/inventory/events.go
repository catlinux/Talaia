package inventory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// WatchEvents watches Docker container events until ctx is cancelled. It retries
// a dropped stream with exponential backoff and jitter.
func (d *Docker) WatchEvents(ctx context.Context, notify func()) {
	backoff := d.eventBackoffBase
	for ctx.Err() == nil {
		started := time.Now()
		if err := d.watchEventsOnce(ctx, notify); err != nil && ctx.Err() == nil {
			d.logUnavailable(err)
		}
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > 30*time.Second {
			backoff = d.eventBackoffBase
		}
		delay := backoff / 2
		if backoff > 0 {
			delay += time.Duration(time.Now().UnixNano() % int64(backoff/2+1))
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, d.eventBackoffMax)
	}
}

func (d *Docker) watchEventsOnce(ctx context.Context, notify func()) error {
	endpoint, err := url.Parse(d.baseURL + "/events")
	if err != nil {
		return fmt.Errorf("parse docker events URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("filters", `{"type":["container"],"event":["start","die","stop","kill","oom","destroy","health_status"]}`)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create docker events request: %w", err)
	}
	response, err := d.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request docker events: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("docker answered %s for /events", response.Status)
	}
	decoder := json.NewDecoder(bufio.NewReader(response.Body))
	for {
		var event json.RawMessage
		if err := decoder.Decode(&event); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read docker event: %w", err)
		}
		notify()
	}
}
