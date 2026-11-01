-- +goose Up
-- The immutable user id of the creator complements the username: a name reused
-- by another account must not grant access to an old job. Legacy rows keep ''.
ALTER TABLE job ADD COLUMN created_by_id text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE job DROP COLUMN created_by_id;
