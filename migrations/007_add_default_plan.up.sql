-- 007_add_default_plan.sql — 基础套餐与逻辑删除

ALTER TABLE plans
    ADD COLUMN is_default TINYINT(1) NOT NULL DEFAULT 0 AFTER is_active,
    ADD COLUMN is_deleted TINYINT(1) NOT NULL DEFAULT 0 AFTER is_default;

CREATE INDEX idx_plans_visible ON plans (is_deleted, is_active, sort_weight);
CREATE INDEX idx_plans_default ON plans (is_default, is_deleted);

INSERT INTO plans (
    name,
    price,
    currency,
    traffic_limit,
    duration_days,
    sort_weight,
    is_active,
    is_default,
    is_deleted,
    created_at,
    updated_at
)
SELECT
    '基础套餐',
    0.00,
    'USDT',
    0,
    3650,
    -1000,
    1,
    1,
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (SELECT 1 FROM plans);

UPDATE plans p
JOIN (
    SELECT id
    FROM plans
    WHERE is_deleted = 0
    ORDER BY sort_weight ASC, id ASC
    LIMIT 1
) d ON p.id = d.id
SET p.is_default = 1,
    p.is_active = 1
WHERE NOT EXISTS (
    SELECT 1
    FROM (
        SELECT id
        FROM plans
        WHERE is_default = 1 AND is_deleted = 0
        LIMIT 1
    ) existing_default
);

UPDATE plans
SET is_default = 0
WHERE is_default = 1
  AND id NOT IN (
      SELECT id
      FROM (
          SELECT id
          FROM plans
          WHERE is_default = 1 AND is_deleted = 0
          ORDER BY id ASC
          LIMIT 1
      ) keep_default
  );

UPDATE plans
SET is_active = 1,
    is_deleted = 0
WHERE is_default = 1;

UPDATE user_subscriptions
SET status = 'EXPIRED',
    active_user_id = NULL,
    updated_at = NOW()
WHERE status = 'ACTIVE'
  AND expire_date <= NOW();

INSERT INTO user_subscriptions (
    user_id,
    plan_id,
    start_date,
    expire_date,
    traffic_limit,
    used_traffic,
    status,
    active_user_id,
    created_at,
    updated_at
)
SELECT
    u.id,
    d.id,
    NOW(),
    DATE_ADD(NOW(), INTERVAL GREATEST(d.duration_days, 1) DAY),
    d.traffic_limit,
    0,
    'ACTIVE',
    u.id,
    NOW(),
    NOW()
FROM users u
CROSS JOIN (
    SELECT id, traffic_limit, duration_days
    FROM plans
    WHERE is_default = 1 AND is_deleted = 0
    ORDER BY id ASC
    LIMIT 1
) d
WHERE NOT EXISTS (
    SELECT 1
    FROM user_subscriptions s
    WHERE s.user_id = u.id
      AND s.status = 'ACTIVE'
      AND s.expire_date > NOW()
);

UPDATE subscription_tokens st
JOIN user_subscriptions s
  ON s.user_id = st.user_id
 AND s.status = 'ACTIVE'
 AND s.expire_date > NOW()
SET st.subscription_id = s.id
WHERE st.subscription_id IS NULL;
