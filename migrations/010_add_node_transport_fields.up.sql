-- 010_add_node_transport_fields.sql — 节点传输层配置

ALTER TABLE nodes
    ADD COLUMN transport VARCHAR(32) NOT NULL DEFAULT 'tcp' AFTER protocol,
    ADD COLUMN xhttp_path VARCHAR(255) NOT NULL DEFAULT '' AFTER line_mode,
    ADD COLUMN xhttp_host VARCHAR(255) NOT NULL DEFAULT '' AFTER xhttp_path,
    ADD COLUMN xhttp_mode VARCHAR(32) NOT NULL DEFAULT 'auto' AFTER xhttp_host;

CREATE INDEX idx_nodes_transport ON nodes (transport);
