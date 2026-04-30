-- 007_add_default_plan.down.sql — 回滚基础套餐与逻辑删除字段

DROP INDEX idx_plans_default ON plans;
DROP INDEX idx_plans_visible ON plans;

ALTER TABLE plans
    DROP COLUMN is_deleted,
    DROP COLUMN is_default;
