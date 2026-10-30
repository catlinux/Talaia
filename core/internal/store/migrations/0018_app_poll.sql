-- +goose Up
ALTER TABLE app ADD COLUMN poll_enabled boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE app DROP COLUMN poll_enabled;
