-- +goose Up
-- The host port is chosen by the agent for external mode and reused by later
-- deployments. Zero means it has not been chosen yet.
ALTER TABLE app ADD COLUMN host_port integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE app DROP COLUMN host_port;
