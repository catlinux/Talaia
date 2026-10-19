-- +goose Up
CREATE TABLE server (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL UNIQUE,
    cert_serial text,
    cert_not_after timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen timestamptz,
    revoked_at timestamptz
);

CREATE TABLE enrollment_token (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash bytea NOT NULL UNIQUE,
    server_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz
);

-- +goose Down
DROP TABLE enrollment_token;
DROP TABLE server;
