CREATE TABLE IF NOT EXISTS behavior_outbox_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id CHAR(36) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    topic VARCHAR(128) NOT NULL,
    event_key VARCHAR(128) NOT NULL,
    payload JSON NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    attempts INT NOT NULL DEFAULT 0,
    next_retry_at DATETIME(3) NULL,
    lease_until DATETIME(3) NULL,
    claim_token VARCHAR(64) NULL,
    last_error VARCHAR(255) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_behavior_outbox_event_id (event_id),
    KEY idx_behavior_outbox_status_retry (status, next_retry_at, id),
    KEY idx_behavior_outbox_user_created (user_id, created_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS behavior_event_consumptions (
    event_id CHAR(36) NOT NULL,
    consumer_group VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    consumed_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (event_id, consumer_group)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS behavior_event_records (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id CHAR(36) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    trace_id VARCHAR(64) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(128) NOT NULL,
    payload JSON NOT NULL,
    occurred_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_behavior_event_records_event (event_id),
    KEY idx_behavior_event_records_user_created (user_id, occurred_at),
    KEY idx_behavior_event_records_type_created (event_type, occurred_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS analytics_dead_letters (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    topic VARCHAR(128) NOT NULL,
    event_key VARCHAR(128) NOT NULL,
    reason VARCHAR(128) NOT NULL,
    raw_event_base64 MEDIUMTEXT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_analytics_dead_letters_topic_created (topic, created_at)
) ENGINE=InnoDB;
