-- +goose Up
INSERT INTO alert_rule (name, type, severity, params, scope_type, builtin)
SELECT 'CPU usage high', 'cpu_high', 'warning', '{"threshold":90,"for_seconds":600}'::jsonb, 'organization', true
WHERE NOT EXISTS (
    SELECT 1 FROM alert_rule WHERE type = 'cpu_high' AND severity = 'warning'
);

INSERT INTO alert_rule (name, type, severity, params, scope_type, builtin)
SELECT 'Memory usage high', 'mem_high', 'warning', '{"threshold":90,"for_seconds":600}'::jsonb, 'organization', true
WHERE NOT EXISTS (
    SELECT 1 FROM alert_rule WHERE type = 'mem_high' AND severity = 'warning'
);

INSERT INTO alert_rule (name, type, severity, params, scope_type, builtin)
SELECT 'Disk usage warning', 'disk_high', 'warning', '{"threshold":80,"for_seconds":0}'::jsonb, 'organization', true
WHERE NOT EXISTS (
    SELECT 1 FROM alert_rule WHERE type = 'disk_high' AND severity = 'warning'
);

-- +goose Down
DELETE FROM alert_rule
WHERE builtin = true
  AND name IN ('CPU usage high', 'Memory usage high', 'Disk usage warning');
