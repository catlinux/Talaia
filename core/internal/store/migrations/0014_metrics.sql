-- +goose Up
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Hypertables cannot be the referencing side of a foreign key, so removed
-- servers' metric rows are explicitly deleted by DeleteServerMetrics.
CREATE TABLE metric_server (
    time timestamptz NOT NULL,
    server_id uuid NOT NULL,
    cpu_pct real,
    mem_used bigint,
    mem_total bigint,
    disk_used bigint,
    disk_total bigint,
    net_rx_bps double precision,
    net_tx_bps double precision
);
SELECT create_hypertable('metric_server', 'time', if_not_exists => true);
CREATE INDEX metric_server_server_time_idx ON metric_server (server_id, time DESC);

CREATE TABLE metric_server_5m (
    time timestamptz NOT NULL, server_id uuid NOT NULL,
    cpu_avg real, cpu_max real, mem_used_avg double precision, mem_used_max bigint,
    mem_total bigint, disk_used_avg double precision, disk_used_max bigint,
    disk_total bigint, net_rx_bps_avg double precision, net_tx_bps_avg double precision,
    PRIMARY KEY (server_id, time)
);
SELECT create_hypertable('metric_server_5m', 'time', if_not_exists => true);
CREATE TABLE metric_server_1h (LIKE metric_server_5m INCLUDING ALL);
SELECT create_hypertable('metric_server_1h', 'time', if_not_exists => true);

CREATE TABLE metric_container (
    time timestamptz NOT NULL, server_id uuid NOT NULL, container_name text NOT NULL,
    stack text NOT NULL DEFAULT '', cpu_pct real, mem_used bigint, mem_limit bigint
);
SELECT create_hypertable('metric_container', 'time', if_not_exists => true);
CREATE INDEX metric_container_server_name_time_idx ON metric_container (server_id, container_name, time DESC);
CREATE TABLE metric_container_5m (
    time timestamptz NOT NULL, server_id uuid NOT NULL, container_name text NOT NULL,
    stack text NOT NULL DEFAULT '', cpu_avg real, cpu_max real,
    mem_used_avg double precision, mem_used_max bigint, mem_limit bigint,
    PRIMARY KEY (server_id, container_name, time)
);
SELECT create_hypertable('metric_container_5m', 'time', if_not_exists => true);
CREATE TABLE metric_container_1h (LIKE metric_container_5m INCLUDING ALL);
SELECT create_hypertable('metric_container_1h', 'time', if_not_exists => true);

-- +goose Down
DROP TABLE metric_container_1h;
DROP TABLE metric_container_5m;
DROP TABLE metric_container;
DROP TABLE metric_server_1h;
DROP TABLE metric_server_5m;
DROP TABLE metric_server;
