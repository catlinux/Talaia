-- +goose Up
ALTER TABLE server ADD COLUMN prev_cert_serial text;

-- +goose Down
ALTER TABLE server DROP COLUMN prev_cert_serial;
