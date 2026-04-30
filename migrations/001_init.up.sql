-- 001_init.sql — 代理订阅系统 suiyue 初始数据库迁移
--
-- 创建所有核心业务表：
-- users, plans, node_groups, plan_node_groups, nodes,
-- user_subscriptions, subscription_tokens, orders,
-- payment_records, traffic_snapshots, node_access_tasks,
-- usage_ledgers, redeem_codes

-- ============================================================
-- 用户表
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    uuid            VARCHAR(36)    NOT NULL UNIQUE,
    username        VARCHAR(64)    NOT NULL UNIQUE,
    password_hash   VARCHAR(255)   NOT NULL,
    email           VARCHAR(255)   DEFAULT NULL,
    xray_user_key   VARCHAR(255)   NOT NULL UNIQUE,
    status          VARCHAR(16)    NOT NULL DEFAULT 'active',
    is_admin        TINYINT(1)     NOT NULL DEFAULT 0,
    last_login_at   DATETIME       DEFAULT NULL,
    last_login_ip   VARCHAR(45)    DEFAULT NULL,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_xray_user_key (xray_user_key),
    INDEX idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 套餐表
-- ============================================================
CREATE TABLE IF NOT EXISTS plans (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(128)   NOT NULL,
    price           DECIMAL(10, 2) NOT NULL,
    currency        VARCHAR(8)     NOT NULL DEFAULT 'USDT',
    traffic_limit   BIGINT UNSIGNED NOT NULL DEFAULT 0,  -- 字节
    duration_days   INT UNSIGNED    NOT NULL DEFAULT 0,
    sort_weight     INT             NOT NULL DEFAULT 0,
    is_active       TINYINT(1)      NOT NULL DEFAULT 1,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_is_active_sort (is_active, sort_weight)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 节点分组表
-- ============================================================
CREATE TABLE IF NOT EXISTS node_groups (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(128)   NOT NULL,
    description     TEXT           DEFAULT NULL,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 套餐-节点分组关联表
-- ============================================================
CREATE TABLE IF NOT EXISTS plan_node_groups (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    plan_id         BIGINT UNSIGNED NOT NULL,
    node_group_id   BIGINT UNSIGNED NOT NULL,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_plan_node_group (plan_id, node_group_id),

    CONSTRAINT fk_png_plan  FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
    CONSTRAINT fk_png_group FOREIGN KEY (node_group_id) REFERENCES node_groups(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 节点表
-- ============================================================
CREATE TABLE IF NOT EXISTS nodes (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name                VARCHAR(128)   NOT NULL,
    protocol            VARCHAR(32)    NOT NULL DEFAULT 'vless',
    host                VARCHAR(255)   NOT NULL,
    port                INT UNSIGNED   NOT NULL DEFAULT 443,
    server_name         VARCHAR(255)   NOT NULL DEFAULT '',
    public_key          VARCHAR(255)   NOT NULL DEFAULT '',
    short_id            VARCHAR(32)    NOT NULL DEFAULT '',
    fingerprint         VARCHAR(32)    NOT NULL DEFAULT 'chrome',
    flow                VARCHAR(32)    NOT NULL DEFAULT 'xtls-rprx-vision',
    node_group_id       BIGINT UNSIGNED DEFAULT NULL,
    agent_base_url      VARCHAR(255)   NOT NULL,
    agent_token_hash    VARCHAR(255)   NOT NULL,
    agent_version       VARCHAR(32)    DEFAULT NULL,
    last_heartbeat_at   DATETIME       DEFAULT NULL,
    is_enabled          TINYINT(1)     NOT NULL DEFAULT 1,
    sort_weight         INT             NOT NULL DEFAULT 0,
    created_at          DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_node_group (node_group_id),
    INDEX idx_is_enabled (is_enabled),

    CONSTRAINT fk_node_group FOREIGN KEY (node_group_id) REFERENCES node_groups(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 用户订阅表
-- ============================================================
CREATE TABLE IF NOT EXISTS user_subscriptions (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id         BIGINT UNSIGNED NOT NULL,
    plan_id         BIGINT UNSIGNED NOT NULL,
    start_date      DATETIME       NOT NULL,
    expire_date     DATETIME       NOT NULL,
    traffic_limit   BIGINT UNSIGNED NOT NULL DEFAULT 0,
    used_traffic    BIGINT UNSIGNED NOT NULL DEFAULT 0,
    status          VARCHAR(16)    NOT NULL DEFAULT 'PENDING',  -- PENDING, ACTIVE, EXPIRED, SUSPENDED
    active_user_id  BIGINT UNSIGNED DEFAULT NULL,  -- 辅助列，实现同一用户只有一张活动订阅
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_user_id (user_id),
    INDEX idx_status (status),
    INDEX idx_expire_date (expire_date),
    UNIQUE KEY uk_active_user (active_user_id),

    CONSTRAINT fk_sub_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_sub_plan  FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 订阅 Token 表
-- ============================================================
CREATE TABLE IF NOT EXISTS subscription_tokens (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id         BIGINT UNSIGNED NOT NULL,
    subscription_id BIGINT UNSIGNED NOT NULL,
    token           VARCHAR(128)   NOT NULL UNIQUE,
    is_revoked      TINYINT(1)     NOT NULL DEFAULT 0,
    last_used_at    DATETIME       DEFAULT NULL,
    expires_at      DATETIME       DEFAULT NULL,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_token (token),
    INDEX idx_user_id (user_id),
    INDEX idx_subscription_id (subscription_id),

    CONSTRAINT fk_st_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_st_sub  FOREIGN KEY (subscription_id) REFERENCES user_subscriptions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- Refresh Token 表（服务端存储，用于黑名单/吊销）
-- ============================================================
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id         BIGINT UNSIGNED NOT NULL,
    token_hash      VARCHAR(255)   NOT NULL UNIQUE,
    expires_at      DATETIME       NOT NULL,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_user_id (user_id),
    INDEX idx_expires_at (expires_at),

    CONSTRAINT fk_rt_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 订单表（v1 保留骨架）
-- ============================================================
CREATE TABLE IF NOT EXISTS orders (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    order_no        VARCHAR(64)    NOT NULL UNIQUE,
    user_id         BIGINT UNSIGNED NOT NULL,
    plan_id         BIGINT UNSIGNED NOT NULL,
    amount          DECIMAL(10, 2) NOT NULL,
    currency        VARCHAR(8)     NOT NULL DEFAULT 'USDT',
    pay_address     VARCHAR(255)   NOT NULL DEFAULT '',
    expected_chain  VARCHAR(32)    NOT NULL DEFAULT 'TRC20',
    status          VARCHAR(16)    NOT NULL DEFAULT 'PENDING',  -- PENDING, PAID, EXPIRED, CANCELLED
    paid_at         DATETIME       DEFAULT NULL,
    expired_at      DATETIME       DEFAULT NULL,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_order_no (order_no),
    INDEX idx_user_id (user_id),
    INDEX idx_status (status),

    CONSTRAINT fk_order_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_order_plan  FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 支付记录表（v1 保留骨架）
-- ============================================================
CREATE TABLE IF NOT EXISTS payment_records (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    order_id        BIGINT UNSIGNED NOT NULL,
    user_id         BIGINT UNSIGNED NOT NULL,
    tx_id           VARCHAR(255)   NOT NULL UNIQUE,
    chain           VARCHAR(32)    NOT NULL DEFAULT 'TRC20',
    amount          DECIMAL(18, 6) NOT NULL,
    from_address    VARCHAR(255)   NOT NULL DEFAULT '',
    to_address      VARCHAR(255)   NOT NULL DEFAULT '',
    raw_payload     JSON           DEFAULT NULL,
    status          VARCHAR(16)    NOT NULL DEFAULT 'PENDING',
    confirmed_at    DATETIME       DEFAULT NULL,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_tx_id (tx_id),
    INDEX idx_order_id (order_id),

    CONSTRAINT fk_pr_order FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE,
    CONSTRAINT fk_pr_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 流量快照表
-- ============================================================
CREATE TABLE IF NOT EXISTS traffic_snapshots (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    node_id         BIGINT UNSIGNED NOT NULL,
    xray_user_key   VARCHAR(255)   NOT NULL,
    uplink_total    BIGINT UNSIGNED NOT NULL DEFAULT 0,
    downlink_total  BIGINT UNSIGNED NOT NULL DEFAULT 0,
    captured_at     DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_node_user_captured (node_id, xray_user_key, captured_at),

    CONSTRAINT fk_ts_node FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 节点访问同步任务表
-- ============================================================
CREATE TABLE IF NOT EXISTS node_access_tasks (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    node_id         BIGINT UNSIGNED NOT NULL,
    subscription_id BIGINT UNSIGNED DEFAULT NULL,
    action          VARCHAR(32)    NOT NULL,  -- UPSERT_USER, DISABLE_USER, DELETE_USER
    payload         JSON           DEFAULT NULL,
    status          VARCHAR(16)    NOT NULL DEFAULT 'PENDING',  -- PENDING, PROCESSING, DONE, FAILED
    retry_count     INT UNSIGNED   NOT NULL DEFAULT 0,
    last_error      TEXT           DEFAULT NULL,
    scheduled_at    DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    locked_at       DATETIME       DEFAULT NULL,
    lock_token      VARCHAR(64)    DEFAULT NULL,
    executed_at     DATETIME       DEFAULT NULL,
    idempotency_key VARCHAR(128)   NOT NULL UNIQUE,

    INDEX idx_status_scheduled (status, scheduled_at),
    INDEX idx_status_locked (status, locked_at),
    INDEX idx_node_id (node_id),

    CONSTRAINT fk_nat_node FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE,
    CONSTRAINT fk_nat_sub  FOREIGN KEY (subscription_id) REFERENCES user_subscriptions(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 流量账本表
-- ============================================================
CREATE TABLE IF NOT EXISTS usage_ledgers (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id         BIGINT UNSIGNED NOT NULL,
    subscription_id BIGINT UNSIGNED DEFAULT NULL,
    node_id         BIGINT UNSIGNED NOT NULL,
    delta_upload    BIGINT UNSIGNED NOT NULL DEFAULT 0,
    delta_download  BIGINT UNSIGNED NOT NULL DEFAULT 0,
    delta_total     BIGINT UNSIGNED NOT NULL DEFAULT 0,
    recorded_at     DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_sub_recorded (subscription_id, recorded_at),
    INDEX idx_user_id (user_id),

    CONSTRAINT fk_ul_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_ul_sub  FOREIGN KEY (subscription_id) REFERENCES user_subscriptions(id) ON DELETE SET NULL,
    CONSTRAINT fk_ul_node FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================
-- 兑换码表
-- ============================================================
CREATE TABLE IF NOT EXISTS redeem_codes (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    code            VARCHAR(64)    NOT NULL UNIQUE,
    plan_id         BIGINT UNSIGNED NOT NULL,
    duration_days   INT UNSIGNED   NOT NULL DEFAULT 0,
    is_used         TINYINT(1)     NOT NULL DEFAULT 0,
    used_by_user_id BIGINT UNSIGNED DEFAULT NULL,
    used_at         DATETIME       DEFAULT NULL,
    expires_at      DATETIME       DEFAULT NULL,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_code (code),
    INDEX idx_is_used (is_used),

    CONSTRAINT fk_rc_plan  FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE RESTRICT,
    CONSTRAINT fk_rc_user  FOREIGN KEY (used_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
