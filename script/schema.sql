CREATE TABLE IF NOT EXISTS ran_feed_user (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    username VARCHAR(64) NOT NULL,
    nickname VARCHAR(64) NOT NULL DEFAULT '',
    avatar VARCHAR(512) NOT NULL DEFAULT '',
    bio VARCHAR(1024) NOT NULL DEFAULT '',
    mobile VARCHAR(32) NULL DEFAULT NULL,
    email VARCHAR(128) NULL DEFAULT NULL,
    password_hash VARCHAR(255) NOT NULL,
    password_salt VARCHAR(128) NOT NULL DEFAULT '',
    gender TINYINT NOT NULL DEFAULT 0,
    birthday DATE NULL,
    status INT NOT NULL DEFAULT 10,
    version BIGINT NOT NULL DEFAULT 1,
    is_deleted TINYINT(1) NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_user_username (username),
    UNIQUE KEY uk_user_mobile (mobile),
    UNIQUE KEY uk_user_email (email),
    KEY idx_user_status (status)
);

CREATE TABLE IF NOT EXISTS ran_feed_content (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    content_type INT NOT NULL,
    status INT NOT NULL DEFAULT 10,
    visibility VARCHAR(32) NOT NULL DEFAULT 'public',
    like_count BIGINT NOT NULL DEFAULT 0,
    favorite_count BIGINT NOT NULL DEFAULT 0,
    comment_count BIGINT NOT NULL DEFAULT 0,
    hot_score DOUBLE NOT NULL DEFAULT 0,
    last_hot_score_at DATETIME NULL,
    version BIGINT NOT NULL DEFAULT 1,
    is_deleted TINYINT(1) NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY idx_content_hot (hot_score DESC, id DESC),
    KEY idx_content_user_created (user_id, created_at DESC, id DESC),
    KEY idx_content_published (published_at DESC, id DESC),
    KEY idx_content_user_published (user_id, published_at DESC, id DESC)
);

CREATE TABLE IF NOT EXISTS ran_feed_article (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    content_id BIGINT NOT NULL,
    title VARCHAR(255) NOT NULL,
    description VARCHAR(1024) NOT NULL DEFAULT '',
    cover VARCHAR(512) NOT NULL DEFAULT '',
    content LONGTEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    is_deleted TINYINT(1) NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_article_content_id (content_id)
);

CREATE TABLE IF NOT EXISTS ran_feed_video (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    content_id BIGINT NOT NULL,
    title VARCHAR(255) NOT NULL,
    media_id VARCHAR(255) NOT NULL DEFAULT '',
    origin_url VARCHAR(1024) NOT NULL DEFAULT '',
    hls_url VARCHAR(1024) NOT NULL DEFAULT '',
    cover_url VARCHAR(1024) NOT NULL DEFAULT '',
    duration BIGINT NOT NULL DEFAULT 0,
    transcode_status INT NOT NULL DEFAULT 10,
    fail_reason VARCHAR(1024) NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    is_deleted TINYINT(1) NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY idx_video_content_id (content_id)
);

CREATE TABLE IF NOT EXISTS ran_feed_like (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    content_id BIGINT NOT NULL,
    content_user_id BIGINT NOT NULL DEFAULT 0,
    status INT NOT NULL DEFAULT 10,
    version BIGINT NOT NULL DEFAULT 1,
    is_deleted TINYINT(1) NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_like_user_content (user_id, content_id),
    KEY idx_like_content_id (content_id),
    KEY idx_like_user_id (user_id),
    KEY idx_like_content_user_id (content_user_id)
);

CREATE TABLE IF NOT EXISTS ran_feed_favorite (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    status INT NOT NULL DEFAULT 10,
    content_id BIGINT NOT NULL,
    content_user_id BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_favorite_user_content (user_id, content_id),
    KEY idx_favorite_user_created (user_id, created_at DESC),
    KEY idx_favorite_content_id (content_id),
    KEY idx_favorite_content_user_id (content_user_id)
);

CREATE TABLE IF NOT EXISTS ran_feed_comment (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    content_id BIGINT NOT NULL,
    content_user_id BIGINT NOT NULL DEFAULT 0,
    user_id BIGINT NOT NULL,
    reply_to_user_id BIGINT NOT NULL DEFAULT 0,
    parent_id BIGINT NOT NULL DEFAULT 0,
    root_id BIGINT NOT NULL DEFAULT 0,
    comment TEXT NOT NULL,
    status INT NOT NULL DEFAULT 10,
    version BIGINT NOT NULL DEFAULT 1,
    is_deleted TINYINT(1) NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY idx_comment_content_id (content_id),
    KEY idx_comment_root_id (root_id),
    KEY idx_comment_parent_id (parent_id),
    KEY idx_comment_content_user_id (content_user_id)
);

CREATE TABLE IF NOT EXISTS ran_feed_follow (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    follow_user_id BIGINT NOT NULL,
    status INT NOT NULL DEFAULT 10,
    version BIGINT NOT NULL DEFAULT 1,
    is_deleted TINYINT(1) NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_follow_relation (user_id, follow_user_id),
    KEY idx_follow_user_id (user_id),
    KEY idx_follow_follow_user_id (follow_user_id)
);

CREATE TABLE IF NOT EXISTS ran_feed_mq_consume_dedup (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    consumer VARCHAR(128) NOT NULL,
    event_id VARCHAR(255) NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_mq_consume_dedup (consumer, event_id),
    KEY idx_mq_consume_dedup_created_at (created_at)
);

CREATE TABLE IF NOT EXISTS ran_feed_count_value (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    biz_type INT NOT NULL,
    target_type INT NOT NULL,
    target_id BIGINT NOT NULL,
    value BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    owner_id BIGINT NOT NULL DEFAULT 0,
    UNIQUE KEY uk_count_value (biz_type, target_type, target_id),
    KEY idx_count_owner_id (owner_id),
    KEY idx_count_target (target_type, target_id)
);
