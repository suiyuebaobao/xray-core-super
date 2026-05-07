ALTER TABLE plans
    ADD COLUMN residential_traffic_limit BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER traffic_limit;

ALTER TABLE user_subscriptions
    ADD COLUMN residential_traffic_limit BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER used_traffic,
    ADD COLUMN residential_used_traffic BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER residential_traffic_limit;

ALTER TABLE nodes
    ADD COLUMN traffic_pool VARCHAR(32) NOT NULL DEFAULT 'normal' AFTER transport;

ALTER TABLE usage_ledgers
    ADD COLUMN traffic_pool VARCHAR(32) NOT NULL DEFAULT 'normal' AFTER node_id;

CREATE INDEX idx_nodes_traffic_pool ON nodes (traffic_pool);
CREATE INDEX idx_usage_ledgers_sub_pool_recorded ON usage_ledgers (subscription_id, traffic_pool, recorded_at);

UPDATE nodes SET traffic_pool = 'normal' WHERE traffic_pool IS NULL OR traffic_pool = '';
UPDATE usage_ledgers SET traffic_pool = 'normal' WHERE traffic_pool IS NULL OR traffic_pool = '';
