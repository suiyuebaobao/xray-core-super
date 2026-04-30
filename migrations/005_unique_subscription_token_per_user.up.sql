-- 订阅 Token 收敛为一用户一条记录。
-- 若存在历史重复记录，优先保留可用记录；同等条件下保留最新 ID。
INSERT INTO subscription_tokens (user_id, subscription_id, token, is_revoked)
SELECT u.id, NULL, LOWER(HEX(RANDOM_BYTES(16))), 0
FROM users u
LEFT JOIN subscription_tokens st ON st.user_id = u.id
WHERE st.id IS NULL;

DELETE st
FROM subscription_tokens st
JOIN (
    SELECT id
    FROM (
        SELECT
            id,
            ROW_NUMBER() OVER (
                PARTITION BY user_id
                ORDER BY
                    CASE
                        WHEN is_revoked = 0 AND (expires_at IS NULL OR expires_at > NOW()) THEN 0
                        ELSE 1
                    END,
                    id DESC
            ) AS rn
        FROM subscription_tokens
    ) ranked
    WHERE ranked.rn > 1
) duplicate_rows ON duplicate_rows.id = st.id;

ALTER TABLE subscription_tokens
    ADD UNIQUE KEY uk_subscription_tokens_user_id (user_id);
