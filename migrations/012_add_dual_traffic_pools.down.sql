DROP INDEX idx_usage_ledgers_sub_pool_recorded ON usage_ledgers;
DROP INDEX idx_nodes_traffic_pool ON nodes;

ALTER TABLE usage_ledgers
    DROP COLUMN traffic_pool;

ALTER TABLE nodes
    DROP COLUMN traffic_pool;

ALTER TABLE user_subscriptions
    DROP COLUMN residential_used_traffic,
    DROP COLUMN residential_traffic_limit;

ALTER TABLE plans
    DROP COLUMN residential_traffic_limit;
