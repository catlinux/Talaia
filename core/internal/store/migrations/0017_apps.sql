-- +goose Up
-- An app wraps a Compose stack on one server. The stack itself stays on disk;
-- this row keeps the source, build and deploy settings and the sealed secrets.
CREATE TABLE app (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id uuid NOT NULL REFERENCES server (id) ON DELETE CASCADE,
    stack_name text NOT NULL,
    source text NOT NULL CHECK (source IN ('git', 'image', 'template')),
    repo_url text NOT NULL DEFAULT '',
    branch text NOT NULL DEFAULT '',
    build text NOT NULL DEFAULT 'dockerfile' CHECK (build IN ('dockerfile', 'auto')),
    dockerfile_path text NOT NULL DEFAULT 'Dockerfile',
    build_context text NOT NULL DEFAULT '.',
    image text NOT NULL DEFAULT '',
    service text NOT NULL DEFAULT 'app',
    port integer NOT NULL DEFAULT 0,
    auto_deploy boolean NOT NULL DEFAULT true,
    zero_downtime boolean NOT NULL DEFAULT true,
    template_id text NOT NULL DEFAULT '',
    template_version text NOT NULL DEFAULT '',
    deploy_key_pub text NOT NULL DEFAULT '',
    deploy_key_enc bytea,
    webhook_secret_enc bytea,
    known_host text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT '',
    UNIQUE (server_id, stack_name)
);

-- Values arrive already sealed by the caller; this table never sees plaintext.
CREATE TABLE app_env (
    app_id uuid NOT NULL REFERENCES app (id) ON DELETE CASCADE,
    key text NOT NULL,
    value_enc bytea NOT NULL,
    secret boolean NOT NULL DEFAULT false,
    build_time boolean NOT NULL DEFAULT false,
    PRIMARY KEY (app_id, key)
);

CREATE TABLE deployment (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id uuid NOT NULL REFERENCES app (id) ON DELETE CASCADE,
    number integer NOT NULL,
    trigger text NOT NULL CHECK (trigger IN ('push', 'poll', 'manual', 'rollback', 'create')),
    commit_sha text NOT NULL DEFAULT '',
    commit_subject text NOT NULL DEFAULT '',
    image_ref text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'building', 'releasing', 'succeeded', 'failed', 'superseded')),
    started_at timestamptz,
    finished_at timestamptz,
    error text NOT NULL DEFAULT '',
    log text NOT NULL DEFAULT '',
    UNIQUE (app_id, number)
);

CREATE TABLE domain (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id uuid NOT NULL REFERENCES app (id) ON DELETE CASCADE,
    hostname text NOT NULL UNIQUE,
    service text NOT NULL DEFAULT 'app',
    port integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX domain_app_id_idx ON domain (app_id);

-- +goose Down
DROP INDEX domain_app_id_idx;
DROP TABLE domain;
DROP TABLE deployment;
DROP TABLE app_env;
DROP TABLE app;
