-- 回滚到订阅级 Token：无法关联订阅的用户级 Token 会被删除。
ALTER TABLE subscription_tokens
    DROP FOREIGN KEY fk_st_sub;

UPDATE subscription_tokens st
JOIN (
    SELECT user_id, MAX(id) AS subscription_id
    FROM user_subscriptions
    GROUP BY user_id
) latest_sub ON latest_sub.user_id = st.user_id
SET st.subscription_id = latest_sub.subscription_id
WHERE st.subscription_id IS NULL;

DELETE FROM subscription_tokens
WHERE subscription_id IS NULL;

ALTER TABLE subscription_tokens
    MODIFY COLUMN subscription_id BIGINT UNSIGNED NOT NULL;

ALTER TABLE subscription_tokens
    ADD CONSTRAINT fk_st_sub
        FOREIGN KEY (subscription_id) REFERENCES user_subscriptions(id)
        ON DELETE CASCADE;
