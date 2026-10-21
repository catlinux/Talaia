-- +goose Up
-- Removing a server is soft: terminal recordings reference server(id) and the
-- row must stay for evidence. A removed server no longer appears in the panel
-- and its name is free for a new enrollment.
ALTER TABLE server ADD COLUMN removed_at timestamptz;
ALTER TABLE server DROP CONSTRAINT server_name_key;
CREATE UNIQUE INDEX server_name_active_idx ON server (name) WHERE removed_at IS NULL;

-- +goose Down
DROP INDEX server_name_active_idx;
ALTER TABLE server ADD CONSTRAINT server_name_key UNIQUE (name);
ALTER TABLE server DROP COLUMN removed_at;
