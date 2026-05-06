CREATE TABLE operation_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    actor_type VARCHAR(16) NOT NULL,
    actor_user_id BIGINT UNSIGNED NULL,
    actor_username VARCHAR(64) NULL,
    client_ip VARCHAR(45) NULL,
    forwarded_for TEXT NULL,
    real_ip VARCHAR(45) NULL,
    user_agent TEXT NULL,
    action VARCHAR(64) NOT NULL,
    target_type VARCHAR(32) NULL,
    target_id BIGINT UNSIGNED NULL,
    result VARCHAR(16) NOT NULL DEFAULT 'success',
    summary VARCHAR(255) NOT NULL,
    extra_json JSON NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_operation_logs_actor_user_id (actor_user_id),
    KEY idx_operation_logs_action (action),
    KEY idx_operation_logs_target (target_type, target_id),
    KEY idx_operation_logs_result_created_at (result, created_at)
);

CREATE TABLE deployment_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    operator_user_id BIGINT UNSIGNED NULL,
    operator_username VARCHAR(64) NULL,
    operator_ip VARCHAR(45) NULL,
    deploy_type VARCHAR(32) NOT NULL,
    target_server_ip VARCHAR(255) NOT NULL,
    target_role VARCHAR(16) NOT NULL,
    request_summary JSON NULL,
    result VARCHAR(16) NOT NULL DEFAULT 'success',
    duration_ms BIGINT UNSIGNED NULL,
    node_id BIGINT UNSIGNED NULL,
    node_ids JSON NULL,
    node_host_id BIGINT UNSIGNED NULL,
    relay_id BIGINT UNSIGNED NULL,
    backend_ids JSON NULL,
    error_detail TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_deployment_logs_operator_user_id (operator_user_id),
    KEY idx_deployment_logs_type_result_created_at (deploy_type, result, created_at),
    KEY idx_deployment_logs_target_server_ip (target_server_ip)
);

CREATE TABLE deployment_log_steps (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    deployment_log_id BIGINT UNSIGNED NOT NULL,
    step_order INT NOT NULL DEFAULT 0,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_deployment_log_steps_log FOREIGN KEY (deployment_log_id) REFERENCES deployment_logs(id) ON DELETE CASCADE,
    KEY idx_deployment_log_steps_log_order (deployment_log_id, step_order)
);
