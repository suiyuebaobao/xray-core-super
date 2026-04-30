-- 006_add_relays.sql — 中转节点与 HAProxy 配置任务

ALTER TABLE nodes
    ADD COLUMN line_mode VARCHAR(32) NOT NULL DEFAULT 'direct_and_relay' AFTER flow;

CREATE TABLE IF NOT EXISTS relays (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name                VARCHAR(128)   NOT NULL,
    host                VARCHAR(255)   NOT NULL,
    forwarder_type      VARCHAR(32)    NOT NULL DEFAULT 'haproxy',
    agent_base_url      VARCHAR(255)   NOT NULL,
    agent_token_hash    VARCHAR(255)   NOT NULL,
    agent_version       VARCHAR(32)    DEFAULT NULL,
    status              VARCHAR(16)    NOT NULL DEFAULT 'offline',
    last_heartbeat_at   DATETIME       DEFAULT NULL,
    is_enabled          TINYINT(1)     NOT NULL DEFAULT 1,
    created_at          DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_relay_enabled (is_enabled),
    INDEX idx_relay_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS relay_backends (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    relay_id        BIGINT UNSIGNED NOT NULL,
    exit_node_id    BIGINT UNSIGNED NOT NULL,
    name            VARCHAR(128)   NOT NULL DEFAULT '',
    listen_port     INT UNSIGNED   NOT NULL,
    target_host     VARCHAR(255)   NOT NULL,
    target_port     INT UNSIGNED   NOT NULL,
    is_enabled      TINYINT(1)     NOT NULL DEFAULT 1,
    sort_weight     INT            NOT NULL DEFAULT 0,
    created_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_relay_listen_port (relay_id, listen_port),
    INDEX idx_relay_backend_relay (relay_id),
    INDEX idx_relay_backend_exit_node (exit_node_id),
    INDEX idx_relay_backend_enabled (is_enabled),

    CONSTRAINT fk_rb_relay FOREIGN KEY (relay_id) REFERENCES relays(id) ON DELETE CASCADE,
    CONSTRAINT fk_rb_exit_node FOREIGN KEY (exit_node_id) REFERENCES nodes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS relay_config_tasks (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    relay_id        BIGINT UNSIGNED NOT NULL,
    action          VARCHAR(32)    NOT NULL,
    payload         JSON           DEFAULT NULL,
    status          VARCHAR(16)    NOT NULL DEFAULT 'PENDING',
    retry_count     INT UNSIGNED   NOT NULL DEFAULT 0,
    last_error      TEXT           DEFAULT NULL,
    scheduled_at    DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    locked_at       DATETIME       DEFAULT NULL,
    lock_token      VARCHAR(64)    DEFAULT NULL,
    executed_at     DATETIME       DEFAULT NULL,
    idempotency_key VARCHAR(128)   NOT NULL UNIQUE,

    INDEX idx_relay_task_status_scheduled (status, scheduled_at),
    INDEX idx_relay_task_status_locked (status, locked_at),
    INDEX idx_relay_task_relay (relay_id),

    CONSTRAINT fk_rct_relay FOREIGN KEY (relay_id) REFERENCES relays(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS relay_traffic_snapshots (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    relay_id            BIGINT UNSIGNED NOT NULL,
    relay_backend_id    BIGINT UNSIGNED DEFAULT NULL,
    listen_port         INT UNSIGNED    NOT NULL,
    bytes_in_total      BIGINT UNSIGNED NOT NULL DEFAULT 0,
    bytes_out_total     BIGINT UNSIGNED NOT NULL DEFAULT 0,
    captured_at         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_relay_traffic_relay_time (relay_id, captured_at),
    INDEX idx_relay_traffic_backend_time (relay_backend_id, captured_at),

    CONSTRAINT fk_rts_relay FOREIGN KEY (relay_id) REFERENCES relays(id) ON DELETE CASCADE,
    CONSTRAINT fk_rts_backend FOREIGN KEY (relay_backend_id) REFERENCES relay_backends(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
