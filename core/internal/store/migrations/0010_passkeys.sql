-- +goose Up
-- Passkeys are WebAuthn credentials. The credential ID, public key and AAGUID
-- are needed to verify assertions; the name is only for the panel.
CREATE TABLE passkey (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    credential_id bytea NOT NULL UNIQUE,
    public_key bytea NOT NULL,
    aaguid bytea,
    sign_count bigint NOT NULL DEFAULT 0,
    transports text[] NOT NULL DEFAULT '{}',
    name text NOT NULL,
    backup_eligible boolean NOT NULL DEFAULT false,
    backup_state boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX passkey_user_id_idx ON passkey (user_id);

-- A ceremony holds the challenge and the options between begin and finish.
-- The row is deleted when it is used and expires after five minutes.
CREATE TABLE webauthn_ceremony (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid REFERENCES app_user (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('register', 'login')),
    session_data jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX webauthn_ceremony_expires_at_idx ON webauthn_ceremony (expires_at);

-- +goose Down
DROP TABLE webauthn_ceremony;
DROP TABLE passkey;
