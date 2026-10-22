-- +goose Up
-- One organization only. The migration that created the table left it empty.
INSERT INTO organization (name)
SELECT 'Talaia'
WHERE NOT EXISTS (SELECT 1 FROM organization);

CREATE TABLE server_group (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE server
    ADD COLUMN group_id uuid REFERENCES server_group (id) ON DELETE SET NULL;
CREATE INDEX server_group_id_idx ON server (group_id);

CREATE TABLE team (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE team_member (
    team_id uuid NOT NULL REFERENCES team (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_member_user_id_idx ON team_member (user_id);

ALTER TABLE app_user
    ADD COLUMN display_name text NOT NULL DEFAULT '',
    ADD COLUMN email text,
    ADD COLUMN disabled_at timestamptz,
    ADD COLUMN oidc_subject text;

CREATE UNIQUE INDEX app_user_email_lower_idx ON app_user (lower(email))
    WHERE email IS NOT NULL;
CREATE UNIQUE INDEX app_user_oidc_subject_idx ON app_user (oidc_subject)
    WHERE oidc_subject IS NOT NULL;

CREATE TABLE role_binding (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'team', 'api_key')),
    subject_id uuid NOT NULL,
    scope_type text NOT NULL CHECK (scope_type IN ('organization', 'group', 'server', 'stack', 'container')),
    scope_server_id uuid REFERENCES server (id) ON DELETE CASCADE,
    scope_group_id uuid REFERENCES server_group (id) ON DELETE CASCADE,
    scope_name text,
    role text NOT NULL CHECK (role IN ('admin', 'server_operator', 'app_operator', 'viewer', 'metrics_only')),
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT role_binding_scope_check CHECK (
        (scope_type = 'organization'
            AND scope_server_id IS NULL AND scope_group_id IS NULL AND scope_name IS NULL)
        OR (scope_type = 'group'
            AND scope_server_id IS NULL AND scope_group_id IS NOT NULL AND scope_name IS NULL)
        OR (scope_type = 'server'
            AND scope_server_id IS NOT NULL AND scope_group_id IS NULL AND scope_name IS NULL)
        OR (scope_type IN ('stack', 'container')
            AND scope_server_id IS NOT NULL AND scope_group_id IS NULL
            AND scope_name IS NOT NULL AND btrim(scope_name) <> '')
    )
);
CREATE INDEX role_binding_subject_idx ON role_binding (subject_type, subject_id);
CREATE INDEX role_binding_scope_server_idx ON role_binding (scope_server_id);
CREATE INDEX role_binding_scope_group_idx ON role_binding (scope_group_id);
-- NULLs would defeat a plain unique constraint, so use a total expression.
CREATE UNIQUE INDEX role_binding_unique_idx ON role_binding (
    subject_type,
    subject_id,
    scope_type,
    COALESCE(scope_server_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(scope_group_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(scope_name, '')
);

CREATE TABLE user_invite (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash bytea NOT NULL UNIQUE,
    username text NOT NULL,
    email text,
    role text NOT NULL CHECK (role IN ('admin', 'server_operator', 'app_operator', 'viewer', 'metrics_only')),
    scope_type text NOT NULL CHECK (scope_type IN ('organization', 'group', 'server', 'stack', 'container')),
    scope_server_id uuid REFERENCES server (id) ON DELETE CASCADE,
    scope_group_id uuid REFERENCES server_group (id) ON DELETE CASCADE,
    scope_name text,
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    CONSTRAINT user_invite_scope_check CHECK (
        (scope_type = 'organization'
            AND scope_server_id IS NULL AND scope_group_id IS NULL AND scope_name IS NULL)
        OR (scope_type = 'group'
            AND scope_server_id IS NULL AND scope_group_id IS NOT NULL AND scope_name IS NULL)
        OR (scope_type = 'server'
            AND scope_server_id IS NOT NULL AND scope_group_id IS NULL AND scope_name IS NULL)
        OR (scope_type IN ('stack', 'container')
            AND scope_server_id IS NOT NULL AND scope_group_id IS NULL
            AND scope_name IS NOT NULL AND btrim(scope_name) <> '')
    )
);
CREATE INDEX user_invite_pending_idx ON user_invite (expires_at)
    WHERE used_at IS NULL;

CREATE TABLE api_key (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id text NOT NULL UNIQUE,
    name text NOT NULL,
    owner_id uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    secret_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz
);
CREATE INDEX api_key_owner_id_idx ON api_key (owner_id);
CREATE INDEX api_key_active_idx ON api_key (key_id)
    WHERE revoked_at IS NULL;

ALTER TABLE audit_event
    ADD COLUMN actor_id uuid,
    ADD COLUMN scope jsonb;

-- The installation that predates roles gets an organization admin for every
-- existing user. A database with bindings already (for example a re-run after a
-- partial manual migration) does not get a second one.
INSERT INTO role_binding (subject_type, subject_id, scope_type, role, created_by)
SELECT 'user', u.id, 'organization', 'admin', 'migration'
FROM app_user AS u
WHERE NOT EXISTS (
    SELECT 1 FROM role_binding AS b
    WHERE b.subject_type = 'user' AND b.subject_id = u.id
);

-- +goose Down
DROP TABLE api_key;
DROP TABLE user_invite;
DROP TABLE role_binding;
DROP TABLE team_member;
DROP TABLE team;

ALTER TABLE server DROP COLUMN group_id;
DROP TABLE server_group;

DROP INDEX app_user_email_lower_idx;
DROP INDEX app_user_oidc_subject_idx;
ALTER TABLE app_user
    DROP COLUMN display_name,
    DROP COLUMN email,
    DROP COLUMN disabled_at,
    DROP COLUMN oidc_subject;

ALTER TABLE audit_event
    DROP COLUMN actor_id,
    DROP COLUMN scope;
