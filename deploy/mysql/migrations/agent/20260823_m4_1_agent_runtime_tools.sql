USE agent_db;

CREATE TABLE IF NOT EXISTS agent_sessions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    session_no VARCHAR(64) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_session_no (session_no),
    KEY idx_agent_session_user_updated (user_id, updated_at),
    CONSTRAINT chk_agent_session_status CHECK (status IN ('ACTIVE','ARCHIVED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS agent_messages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    session_id BIGINT UNSIGNED NOT NULL,
    seq_no INT UNSIGNED NOT NULL,
    role VARCHAR(32) NOT NULL,
    content TEXT NOT NULL,
    model_name VARCHAR(128) NULL,
    prompt_version VARCHAR(64) NULL,
    token_usage_json JSON NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_message_seq (session_id, seq_no),
    KEY idx_agent_message_session_created (session_id, created_at),
    CONSTRAINT chk_agent_message_role CHECK (role IN ('USER','ASSISTANT','TOOL','SYSTEM')),
    CONSTRAINT fk_agent_message_session FOREIGN KEY (session_id) REFERENCES agent_sessions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS agent_runs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    run_id VARCHAR(64) NOT NULL,
    session_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    trace_id VARCHAR(64) NOT NULL,
    user_input TEXT NOT NULL,
    status VARCHAR(32) NOT NULL,
    model_name VARCHAR(128) NOT NULL,
    prompt_version VARCHAR(64) NOT NULL,
    step_count INT UNSIGNED NOT NULL DEFAULT 0,
    final_result_json JSON NULL,
    error_code VARCHAR(64) NULL,
    error_message VARCHAR(255) NULL,
    started_at DATETIME(3) NOT NULL,
    ended_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_run_id (run_id),
    UNIQUE KEY uq_agent_run_trace (trace_id),
    KEY idx_agent_run_user_created (user_id, created_at),
    KEY idx_agent_run_session_created (session_id, created_at),
    KEY idx_agent_run_status_created (status, created_at),
    CONSTRAINT chk_agent_run_status CHECK (status IN ('RUNNING','SUCCEEDED','FAILED','TIMEOUT')),
    CONSTRAINT fk_agent_run_session FOREIGN KEY (session_id) REFERENCES agent_sessions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS agent_steps (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    run_id BIGINT UNSIGNED NOT NULL,
    step_no INT UNSIGNED NOT NULL,
    step_type VARCHAR(32) NOT NULL,
    tool_name VARCHAR(64) NULL,
    attempt INT UNSIGNED NOT NULL DEFAULT 1,
    input_json JSON NULL,
    output_json JSON NULL,
    status VARCHAR(32) NOT NULL,
    error_code VARCHAR(64) NULL,
    error_message VARCHAR(255) NULL,
    latency_ms INT UNSIGNED NOT NULL DEFAULT 0,
    started_at DATETIME(3) NOT NULL,
    ended_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_step_attempt (run_id, step_no, attempt),
    KEY idx_agent_step_run_step (run_id, step_no),
    CONSTRAINT chk_agent_step_status CHECK (status IN ('RUNNING','SUCCEEDED','FAILED','TIMEOUT')),
    CONSTRAINT chk_agent_step_type CHECK (step_type IN ('MODEL','TOOL')),
    CONSTRAINT fk_agent_step_run FOREIGN KEY (run_id) REFERENCES agent_runs(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
