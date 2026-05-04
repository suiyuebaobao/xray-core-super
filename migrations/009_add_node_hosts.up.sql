-- 009_add_node_hosts.sql — 物理节点主机与多出口 IP 逻辑节点

CREATE TABLE IF NOT EXISTS node_hosts (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name                VARCHAR(128)   NOT NULL,
    ssh_host            VARCHAR(255)   NOT NULL,
    ssh_port            INT UNSIGNED   NOT NULL DEFAULT 22,
    agent_base_url      VARCHAR(255)   NOT NULL,
    agent_token_hash    VARCHAR(255)   NOT NULL,
    agent_version       VARCHAR(32)    DEFAULT NULL,
    last_heartbeat_at   DATETIME       DEFAULT NULL,
    is_enabled          TINYINT(1)     NOT NULL DEFAULT 1,
    created_at          DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_node_hosts_enabled (is_enabled),
    INDEX idx_node_hosts_ssh_host (ssh_host)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE nodes
    ADD COLUMN node_host_id BIGINT UNSIGNED DEFAULT NULL AFTER line_mode,
    ADD COLUMN listen_ip VARCHAR(45) NOT NULL DEFAULT '' AFTER node_host_id,
    ADD COLUMN outbound_ip VARCHAR(45) NOT NULL DEFAULT '' AFTER listen_ip,
    ADD COLUMN xray_inbound_tag VARCHAR(64) NOT NULL DEFAULT '' AFTER outbound_ip,
    ADD COLUMN xray_outbound_tag VARCHAR(64) NOT NULL DEFAULT '' AFTER xray_inbound_tag;

CREATE INDEX idx_nodes_node_host ON nodes (node_host_id);
CREATE INDEX idx_nodes_outbound_ip ON nodes (outbound_ip);

ALTER TABLE nodes
    ADD CONSTRAINT fk_nodes_node_host
        FOREIGN KEY (node_host_id) REFERENCES node_hosts(id)
        ON DELETE SET NULL;
