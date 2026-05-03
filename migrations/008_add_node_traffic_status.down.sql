DROP INDEX idx_nodes_last_traffic_success ON nodes;

ALTER TABLE nodes
    DROP COLUMN last_traffic_error,
    DROP COLUMN traffic_error_count,
    DROP COLUMN last_traffic_error_at,
    DROP COLUMN last_traffic_success_at,
    DROP COLUMN last_traffic_report_at;
