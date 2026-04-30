-- 节点与节点分组多对多关联表。
CREATE TABLE IF NOT EXISTS node_group_nodes (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    node_id         BIGINT UNSIGNED NOT NULL,
    node_group_id   BIGINT UNSIGNED NOT NULL,
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_node_group_node (node_id, node_group_id),
    INDEX idx_ngn_node (node_id),
    INDEX idx_ngn_group (node_group_id),

    CONSTRAINT fk_ngn_node FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE,
    CONSTRAINT fk_ngn_group FOREIGN KEY (node_group_id) REFERENCES node_groups(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 兼容旧数据：把原 nodes.node_group_id 回填到新的关联表。
INSERT IGNORE INTO node_group_nodes (node_id, node_group_id)
SELECT id, node_group_id
FROM nodes
WHERE node_group_id IS NOT NULL;
