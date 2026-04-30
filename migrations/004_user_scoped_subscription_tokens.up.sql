-- 订阅 Token 改为用户级凭证：允许用户在无订阅时预生成 Token。
ALTER TABLE subscription_tokens
    DROP FOREIGN KEY fk_st_sub;

ALTER TABLE subscription_tokens
    MODIFY COLUMN subscription_id BIGINT UNSIGNED NULL;

ALTER TABLE subscription_tokens
    ADD CONSTRAINT fk_st_sub
        FOREIGN KEY (subscription_id) REFERENCES user_subscriptions(id)
        ON DELETE SET NULL;
