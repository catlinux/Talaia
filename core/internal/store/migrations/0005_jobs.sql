-- +goose Up
-- A job is one typed command sent to the agent of a server (for example a
-- container action). Its state only moves forward: pending, running, then
-- succeeded or failed.
CREATE TABLE job (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id uuid NOT NULL REFERENCES server (id) ON DELETE CASCADE,
    kind text NOT NULL,
    params jsonb NOT NULL DEFAULT '{}',
    state text NOT NULL CHECK (state IN ('pending', 'running', 'succeeded', 'failed')),
    message text NOT NULL DEFAULT '',
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE INDEX job_server_id_idx ON job (server_id, created_at DESC);

-- +goose Down
DROP TABLE job;
