-- +goose Up
CREATE TABLE oidc_config (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled boolean NOT NULL DEFAULT false,
    issuer text NOT NULL DEFAULT '',
    client_id text NOT NULL DEFAULT '',
    client_secret_enc bytea,
    button_label text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE oidc_config;
