-- +goose Up
CREATE TABLE resource_note (
    server_id uuid NOT NULL REFERENCES server (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('container', 'stack', 'service')),
    name text NOT NULL,
    note text NOT NULL,
    updated_by text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, kind, name)
);

-- +goose Down
DROP TABLE resource_note;
