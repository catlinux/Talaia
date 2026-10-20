-- +goose Up
-- A terminal session is an interactive shell in a container. Its output is
-- recorded, sealed chunk by chunk with the secrets key, and kept for the
-- retention period. The recording is evidence: it is not deleted with the
-- server, only by the retention purge.
CREATE TABLE terminal_session (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id uuid NOT NULL REFERENCES server (id),
    container text NOT NULL,
    shell text NOT NULL,
    created_by text NOT NULL,
    privileged boolean NOT NULL DEFAULT false,
    opened_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    close_reason text NOT NULL DEFAULT '',
    recorded_bytes bigint NOT NULL DEFAULT 0
);
CREATE INDEX terminal_session_opened_at_idx ON terminal_session (opened_at DESC);

CREATE TABLE terminal_chunk (
    session_id uuid NOT NULL REFERENCES terminal_session (id) ON DELETE CASCADE,
    seq integer NOT NULL,
    data bytea NOT NULL,
    PRIMARY KEY (session_id, seq)
);

-- +goose Down
DROP TABLE terminal_chunk;
DROP TABLE terminal_session;
