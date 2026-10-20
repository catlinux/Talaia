-- +goose Up
-- The text a job produced, for example the lines of a container log.
ALTER TABLE job ADD COLUMN output text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE job DROP COLUMN output;
