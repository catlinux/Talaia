-- +goose Up
ALTER TABLE organization
    ADD COLUMN allow_private_targets boolean NOT NULL DEFAULT false;

CREATE TABLE alert_rule (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    type text NOT NULL CHECK (type IN ('server_offline','cpu_high','mem_high','disk_high','container_down','container_unhealthy','check_failed','cert_expiry')),
    enabled boolean NOT NULL DEFAULT true,
    severity text NOT NULL CHECK (severity IN ('warning','critical')),
    params jsonb NOT NULL DEFAULT '{}',
    scope_type text NOT NULL DEFAULT 'organization' CHECK (scope_type IN ('organization','group','server','stack','container')),
    scope_server_id uuid REFERENCES server (id) ON DELETE CASCADE,
    scope_group_id uuid REFERENCES server_group (id) ON DELETE CASCADE,
    scope_name text,
    channel_ids uuid[] NOT NULL DEFAULT '{}',
    repeat_interval_seconds int NOT NULL DEFAULT 0,
    builtin boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT '',
    CONSTRAINT alert_rule_scope_check CHECK (
        (scope_type = 'organization' AND scope_server_id IS NULL AND scope_group_id IS NULL AND scope_name IS NULL)
        OR (scope_type = 'group' AND scope_server_id IS NULL AND scope_group_id IS NOT NULL AND scope_name IS NULL)
        OR (scope_type = 'server' AND scope_server_id IS NOT NULL AND scope_group_id IS NULL AND scope_name IS NULL)
        OR (scope_type IN ('stack','container') AND scope_server_id IS NOT NULL AND scope_group_id IS NULL AND scope_name IS NOT NULL AND btrim(scope_name) <> '')
    )
);

INSERT INTO alert_rule (name, type, severity, params, builtin) VALUES
    ('Server offline', 'server_offline', 'critical', '{"seconds":45}', true),
    ('Disk usage high', 'disk_high', 'critical', '{"threshold":90,"for_seconds":0}', true),
    ('Container down', 'container_down', 'critical', '{"seconds":20}', true),
    ('Container unhealthy', 'container_unhealthy', 'warning', '{"seconds":60}', true),
    ('Health check failed', 'check_failed', 'critical', '{"consecutive_failures":2}', true),
    ('Certificate expiring', 'cert_expiry', 'warning', '{"days":14}', true);

CREATE TABLE alert (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id uuid REFERENCES alert_rule (id) ON DELETE SET NULL,
    rule_type text NOT NULL,
    severity text NOT NULL CHECK (severity IN ('warning','critical')),
    fingerprint text NOT NULL,
    server_id uuid REFERENCES server (id) ON DELETE CASCADE,
    subject_kind text NOT NULL CHECK (subject_kind IN ('server','container','check')),
    subject_name text NOT NULL DEFAULT '',
    stack text NOT NULL DEFAULT '',
    state text NOT NULL CHECK (state IN ('firing','resolved')),
    summary jsonb NOT NULL DEFAULT '{}',
    started_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    acked_at timestamptz,
    acked_by text NOT NULL DEFAULT '',
    last_notified_at timestamptz,
    silenced boolean NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX alert_firing_fingerprint_idx ON alert (fingerprint) WHERE state = 'firing';
CREATE INDEX alert_started_at_idx ON alert (started_at DESC);
CREATE INDEX alert_server_id_idx ON alert (server_id);

CREATE TABLE alert_watch (
    server_id uuid NOT NULL REFERENCES server (id) ON DELETE CASCADE,
    container_name text NOT NULL,
    last_running_at timestamptz NOT NULL,
    PRIMARY KEY (server_id, container_name)
);

CREATE TABLE silence (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_type text NOT NULL CHECK (scope_type IN ('organization','group','server','stack','container')),
    scope_server_id uuid REFERENCES server (id) ON DELETE CASCADE,
    scope_group_id uuid REFERENCES server_group (id) ON DELETE CASCADE,
    scope_name text,
    rule_type text,
    reason text NOT NULL DEFAULT '',
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    ends_at timestamptz NOT NULL,
    CONSTRAINT silence_scope_check CHECK (
        (scope_type = 'organization' AND scope_server_id IS NULL AND scope_group_id IS NULL AND scope_name IS NULL)
        OR (scope_type = 'group' AND scope_server_id IS NULL AND scope_group_id IS NOT NULL AND scope_name IS NULL)
        OR (scope_type = 'server' AND scope_server_id IS NOT NULL AND scope_group_id IS NULL AND scope_name IS NULL)
        OR (scope_type IN ('stack','container') AND scope_server_id IS NOT NULL AND scope_group_id IS NULL AND scope_name IS NOT NULL AND btrim(scope_name) <> '')
    ),
    CONSTRAINT silence_max_duration_check CHECK (ends_at <= created_at + interval '30 days')
);

CREATE TABLE alert_check (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('http','tcp','tls')),
    target text NOT NULL,
    config jsonb NOT NULL DEFAULT '{}',
    interval_seconds int NOT NULL DEFAULT 60 CHECK (interval_seconds >= 30),
    enabled boolean NOT NULL DEFAULT true,
    server_id uuid REFERENCES server (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT ''
);
CREATE TABLE alert_check_result (
    time timestamptz NOT NULL,
    check_id uuid NOT NULL REFERENCES alert_check (id) ON DELETE CASCADE,
    ok boolean NOT NULL,
    latency_ms int NOT NULL DEFAULT 0,
    status_code int,
    error text NOT NULL DEFAULT '',
    cert_not_after timestamptz
);
CREATE INDEX alert_check_result_check_time_idx ON alert_check_result (check_id, time DESC);

CREATE TABLE notification_channel (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    type text NOT NULL CHECK (type IN ('telegram','ntfy','email','discord','webhook')),
    enabled boolean NOT NULL DEFAULT true,
    language text NOT NULL DEFAULT 'en' CHECK (language IN ('ca','es','en')),
    config jsonb NOT NULL DEFAULT '{}',
    secret_enc bytea,
    last_error text NOT NULL DEFAULT '',
    last_ok_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT ''
);
CREATE TABLE notification_delivery (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id uuid REFERENCES alert (id) ON DELETE CASCADE,
    channel_id uuid NOT NULL REFERENCES notification_channel (id) ON DELETE CASCADE,
    event text NOT NULL CHECK (event IN ('firing','resolved','test')),
    attempts int NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sent','failed')),
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz
);
CREATE INDEX notification_delivery_due_idx ON notification_delivery (status, next_attempt_at);

-- +goose Down
DROP TABLE notification_delivery;
DROP TABLE notification_channel;
DROP INDEX alert_check_result_check_time_idx;
DROP TABLE alert_check_result;
DROP TABLE alert_check;
DROP TABLE silence;
DROP TABLE alert_watch;
DROP INDEX alert_server_id_idx;
DROP INDEX alert_started_at_idx;
DROP INDEX alert_firing_fingerprint_idx;
DROP TABLE alert;
DROP TABLE alert_rule;
ALTER TABLE organization DROP COLUMN allow_private_targets;
