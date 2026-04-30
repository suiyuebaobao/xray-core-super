DROP TABLE IF EXISTS relay_traffic_snapshots;
DROP TABLE IF EXISTS relay_config_tasks;
DROP TABLE IF EXISTS relay_backends;
DROP TABLE IF EXISTS relays;

ALTER TABLE nodes
    DROP COLUMN line_mode;
