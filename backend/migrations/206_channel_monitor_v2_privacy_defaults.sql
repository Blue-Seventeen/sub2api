-- Privacy-safe default for a missing setting, never a forced preference change.
INSERT INTO settings (key, value)
VALUES ('channel_monitor_hide_throughput', 'true')
ON CONFLICT (key) DO NOTHING;
