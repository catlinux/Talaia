-- +goose Up
-- The audit screen filters by action and reads the newest events first.
CREATE INDEX audit_event_action_idx ON audit_event (action, at DESC);

-- +goose Down
DROP INDEX audit_event_action_idx;
