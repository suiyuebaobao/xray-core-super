DROP INDEX idx_nodes_transport ON nodes;

ALTER TABLE nodes
    DROP COLUMN xhttp_mode,
    DROP COLUMN xhttp_host,
    DROP COLUMN xhttp_path,
    DROP COLUMN transport;
