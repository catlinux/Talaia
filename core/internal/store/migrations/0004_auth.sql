-- +goose Up
CREATE TABLE app_user (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    totp_secret_enc bytea,
    totp_enabled_at timestamptz,
    totp_last_step bigint,
    failed_attempts int NOT NULL DEFAULT 0,
    locked_until timestamptz,
    password_changed_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recovery_code (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    code_hash bytea NOT NULL UNIQUE,
    used_at timestamptz
);
CREATE INDEX recovery_code_user_id_idx ON recovery_code (user_id);

CREATE TABLE session (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash bytea NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    stage text NOT NULL CHECK (stage IN ('mfa_pending', 'full')),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    ip text,
    user_agent text,
    mfa_failures int NOT NULL DEFAULT 0
);
CREATE INDEX session_user_id_idx ON session (user_id);

CREATE TABLE audit_event (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    at timestamptz NOT NULL DEFAULT now(),
    actor text NOT NULL DEFAULT '',
    action text NOT NULL,
    ip text,
    result text NOT NULL,
    details jsonb
);
CREATE INDEX audit_event_at_idx ON audit_event (at);

-- +goose Down
DROP TABLE audit_event;
DROP TABLE session;
DROP TABLE recovery_code;
DROP TABLE app_user;
