-- 喵星成长屋 P0 全量建表（MySQL 版）
-- 单孩单猫设计：pet 表最多一行

CREATE TABLE IF NOT EXISTS device_token (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    token VARCHAR(64) NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS pet (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    name VARCHAR(32) NOT NULL,
    species VARCHAR(16) NOT NULL,
    personality VARCHAR(64) NOT NULL DEFAULT '热情开朗',
    level INT NOT NULL DEFAULT 1,
    earned_total INT NOT NULL DEFAULT 0,
    balance INT NOT NULL DEFAULT 0,
    hunger DOUBLE NOT NULL DEFAULT 100,
    mood DOUBLE NOT NULL DEFAULT 100,
    hunger_decay DOUBLE NOT NULL DEFAULT 2.0,
    streak INT NOT NULL DEFAULT 0,
    last_tick DATETIME NULL,
    hatched_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS species_config (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    species VARCHAR(16) NOT NULL UNIQUE,
    display_name VARCHAR(32) NOT NULL,
    description VARCHAR(128) NOT NULL DEFAULT '',
    enabled INT NOT NULL DEFAULT 1,
    sort INT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS task_template (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    name VARCHAR(64) NOT NULL,
    icon VARCHAR(16) NOT NULL DEFAULT '⭐',
    category VARCHAR(16) NOT NULL DEFAULT 'custom',
    goal_type VARCHAR(16) NOT NULL DEFAULT 'free',
    goal_value INT NOT NULL DEFAULT 0,
    evidence_type VARCHAR(32) NOT NULL DEFAULT 'none',
    confirm_mode VARCHAR(8) NOT NULL DEFAULT 'a',
    base_score INT NOT NULL DEFAULT 20,
    streak_enabled INT NOT NULL DEFAULT 1,
    extra_minutes INT NOT NULL DEFAULT 0,
    extra_stars INT NOT NULL DEFAULT 0,
    extra_cap INT NOT NULL DEFAULT 0,
    sort INT NOT NULL DEFAULT 0,
    enabled INT NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS task_schedule (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    template_id BIGINT NOT NULL,
    date_mode VARCHAR(16) NOT NULL DEFAULT 'weekday_fixed',
    weekdays VARCHAR(16) NOT NULL DEFAULT '1,2,3,4,5,6,7',
    weekly_quota INT NOT NULL DEFAULT 0,
    valid_from DATE NULL,
    valid_to DATE NULL,
    INDEX idx_schedule_template (template_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS reward_policy (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    match_type VARCHAR(16) NOT NULL,
    match_value VARCHAR(32) NOT NULL,
    multiplier DOUBLE NOT NULL DEFAULT 1,
    note VARCHAR(128) NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS holiday (
    `date` DATE PRIMARY KEY,
    name VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS leave_record (
    `date` DATE PRIMARY KEY,
    reason VARCHAR(128) NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS daily_task (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    `date` DATE NOT NULL,
    template_id BIGINT NOT NULL,
    snapshot TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    streak_snapshot INT NOT NULL DEFAULT 0,
    quota_done INT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_daily (date, template_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS task_completion (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    daily_task_id BIGINT NOT NULL,
    evidence TEXT NOT NULL,
    duration_sec INT NOT NULL DEFAULT 0,
    submitted_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    confirm_status VARCHAR(16) NOT NULL DEFAULT 'pending',
    confirmed_at DATETIME NULL,
    reject_reason VARCHAR(128) NOT NULL DEFAULT '',
    INDEX idx_completion_task (daily_task_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ai_review_job (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    completion_id BIGINT NOT NULL,
    daily_task_id BIGINT NOT NULL,
    kid_id BIGINT NOT NULL,
    snapshot TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'queued',
    stage VARCHAR(32) NOT NULL DEFAULT 'queued',
    attempt INT NOT NULL DEFAULT 0,
    error TEXT NULL,
    result TEXT NULL,
    asr_cache TEXT NULL,
    lease_token VARCHAR(64) NULL,
    lease_expires_at DATETIME(3) NULL,
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY idx_ai_review_job_completion_id (completion_id),
    INDEX idx_ai_review_job_daily_task_id (daily_task_id),
    INDEX idx_ai_review_job_kid_id (kid_id),
    INDEX idx_ai_review_queue (status, lease_expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS settlement_ledger (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    `date` DATE NOT NULL,
    daily_task_id BIGINT NULL,
    source VARCHAR(32) NOT NULL,
    base INT NOT NULL DEFAULT 0,
    date_multiplier DOUBLE NOT NULL DEFAULT 1,
    streak_multiplier DOUBLE NOT NULL DEFAULT 1,
    final INT NOT NULL DEFAULT 0,
    delta_balance INT NOT NULL DEFAULT 0,
    delta_earned INT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS wish (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    title VARCHAR(128) NOT NULL,
    photo VARCHAR(255) NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    price INT NOT NULL DEFAULT 0,
    hoped_price INT NOT NULL DEFAULT 0,
    reject_reason VARCHAR(128) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    redeemed_at DATETIME NULL,
    fulfilled_at DATETIME NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS scene_item (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    item_key VARCHAR(32) NOT NULL,
    x DOUBLE NOT NULL,
    y DOUBLE NOT NULL,
    placed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS owned_item (
    item_key VARCHAR(32) PRIMARY KEY,
    bought_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS pet_event (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    type VARCHAR(16) NOT NULL,
    payload TEXT NOT NULL,
    day DATE NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_pet_event_day (day, type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
