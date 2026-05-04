ALTER TABLE nodes
    DROP FOREIGN KEY fk_nodes_node_host;

DROP INDEX idx_nodes_node_host ON nodes;
DROP INDEX idx_nodes_outbound_ip ON nodes;

ALTER TABLE nodes
    DROP COLUMN xray_outbound_tag,
    DROP COLUMN xray_inbound_tag,
    DROP COLUMN outbound_ip,
    DROP COLUMN listen_ip,
    DROP COLUMN node_host_id;

DROP TABLE IF EXISTS node_hosts;
