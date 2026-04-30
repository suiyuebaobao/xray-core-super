-- 001_init.sql (down) — 回滚初始迁移，按外键依赖顺序反向删除所有表

DROP TABLE IF EXISTS redeem_codes;
DROP TABLE IF EXISTS usage_ledgers;
DROP TABLE IF EXISTS node_access_tasks;
DROP TABLE IF EXISTS traffic_snapshots;
DROP TABLE IF EXISTS payment_records;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS subscription_tokens;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS user_subscriptions;
DROP TABLE IF EXISTS nodes;
DROP TABLE IF EXISTS plan_node_groups;
DROP TABLE IF EXISTS node_groups;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS users;
