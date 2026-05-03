ALTER TABLE nodes
    ADD COLUMN last_traffic_report_at DATETIME DEFAULT NULL AFTER last_heartbeat_at,
    ADD COLUMN last_traffic_success_at DATETIME DEFAULT NULL AFTER last_traffic_report_at,
    ADD COLUMN last_traffic_error_at DATETIME DEFAULT NULL AFTER last_traffic_success_at,
    ADD COLUMN traffic_error_count INT UNSIGNED NOT NULL DEFAULT 0 AFTER last_traffic_error_at,
    ADD COLUMN last_traffic_error TEXT DEFAULT NULL AFTER traffic_error_count;

CREATE INDEX idx_nodes_last_traffic_success ON nodes (last_traffic_success_at);
