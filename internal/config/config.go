package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App   AppConfig   `yaml:"app"`
	Cron  CronConfig  `yaml:"cron"`
	MySQL MySQLConfig `yaml:"mysql"`
	Redis RedisConfig `yaml:"redis"`
	JWT   JWTConfig   `yaml:"jwt"`
	Kafka KafkaConfig `yaml:"kafka"`
	Canal CanalConfig `yaml:"canal"`
}

type AppConfig struct {
	Name     string `yaml:"name"`
	HTTPAddr string `yaml:"http_addr"`
}

type CronConfig struct {
	Enabled                     bool          `yaml:"enabled"`
	HotSnapshotInterval         time.Duration `yaml:"hot_snapshot_interval"`
	HotSnapshotSize             int64         `yaml:"hot_snapshot_size"`
	HotSnapshotTTL              time.Duration `yaml:"hot_snapshot_ttl"`
	HotColdUpdateInterval       time.Duration `yaml:"hot_cold_update_interval"`
	HotColdUpdateSize           int64         `yaml:"hot_cold_update_size"`
	FollowInboxBackfillInterval time.Duration `yaml:"follow_inbox_backfill_interval"`
	FollowInboxBatchSize        int           `yaml:"follow_inbox_batch_size"`
}

type MySQLConfig struct {
	Enabled         bool          `yaml:"enabled"`
	DSN             string        `yaml:"dsn"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

type RedisConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type JWTConfig struct {
	Secret string        `yaml:"secret"`
	Issuer string        `yaml:"issuer"`
	Expire time.Duration `yaml:"expire"`
}

type KafkaConfig struct {
	Enabled bool              `yaml:"enabled"`
	Brokers []string          `yaml:"brokers"`
	GroupID string            `yaml:"group_id"`
	Topics  KafkaTopicsConfig `yaml:"topics"`
}

type CanalConfig struct {
	Enabled           bool          `yaml:"enabled"`
	Addr              string        `yaml:"addr"`
	Username          string        `yaml:"username"`
	Password          string        `yaml:"password"`
	Destination       string        `yaml:"destination"`
	Subscribe         string        `yaml:"subscribe"`
	BatchSize         int           `yaml:"batch_size"`
	PollInterval      time.Duration `yaml:"poll_interval"`
	ReconnectInterval time.Duration `yaml:"reconnect_interval"`
	DedupeTTL         time.Duration `yaml:"dedupe_ttl"`
}

type KafkaTopicsConfig struct {
	CountCanal string `yaml:"count_canal"`
	LikeAction string `yaml:"like_action"`
}

func Load() (Config, error) {
	cfg := defaultConfig()
	configPath, err := resolveConfigPath()
	if err != nil {
		return Config{}, err
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("read config file %q: %w", configPath, err)
	}

	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config file %q: %w", configPath, err)
	}

	applyDefaults(&cfg)
	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		App: AppConfig{
			Name:     "ran-feed-server",
			HTTPAddr: ":8080",
		},
		Cron: CronConfig{
			Enabled:                     true,
			HotSnapshotInterval:         1 * time.Minute,
			HotSnapshotSize:             1000,
			HotSnapshotTTL:              30 * time.Minute,
			HotColdUpdateInterval:       24 * time.Hour,
			HotColdUpdateSize:           5000,
			FollowInboxBackfillInterval: 5 * time.Minute,
			FollowInboxBatchSize:        200,
		},
		MySQL: MySQLConfig{
			MaxOpenConns:    20,
			MaxIdleConns:    10,
			ConnMaxLifetime: 30 * time.Minute,
		},
		Redis: RedisConfig{
			DB: 0,
		},
		JWT: JWTConfig{
			Secret: "change-me-in-production",
			Issuer: "ran-feed-server",
			Expire: 72 * time.Hour,
		},
		Kafka: KafkaConfig{
			GroupID: "ran-feed-server",
			Topics: KafkaTopicsConfig{
				CountCanal: "ran-feed-count-canal",
				LikeAction: "ran-feed-like-action",
			},
		},
		Canal: CanalConfig{
			Destination:       "example",
			Subscribe:         ".*\\.(ran_feed_like|ran_feed_favorite|ran_feed_comment|ran_feed_follow|ran_feed_content)",
			BatchSize:         100,
			PollInterval:      300 * time.Millisecond,
			ReconnectInterval: 3 * time.Second,
			DedupeTTL:         7 * 24 * time.Hour,
		},
	}
}

func resolveConfigPath() (string, error) {
	if path, ok := os.LookupEnv("CONFIG_PATH"); ok && path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("config file from CONFIG_PATH not found: %w", err)
		}
		return path, nil
	}

	searchRoots := []string{}
	if cwd, err := os.Getwd(); err == nil {
		searchRoots = append(searchRoots, cwd)
	}
	if exePath, err := os.Executable(); err == nil {
		searchRoots = append(searchRoots, filepath.Dir(exePath))
	}

	seen := make(map[string]struct{})
	for _, root := range searchRoots {
		for dir := root; dir != "" && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			cleanDir := filepath.Clean(dir)
			if _, ok := seen[cleanDir]; ok {
				continue
			}
			seen[cleanDir] = struct{}{}

			candidates := []string{
				filepath.Join(cleanDir, "config", "config.yaml"),
				filepath.Join(cleanDir, "config.yaml"),
			}

			for _, candidate := range candidates {
				if _, err := os.Stat(candidate); err == nil {
					return candidate, nil
				}
			}
		}
	}

	return "", errors.New("config file not found, expected CONFIG_PATH or a config/config.yaml in the current directory or its parent directories")
}

func applyDefaults(cfg *Config) {
	if cfg.App.Name == "" {
		cfg.App.Name = "ran-feed-server"
	}
	if cfg.App.HTTPAddr == "" {
		cfg.App.HTTPAddr = ":8080"
	}
	if !cfg.Cron.Enabled {
		cfg.Cron.Enabled = false
	}
	if cfg.Cron.HotSnapshotInterval <= 0 {
		cfg.Cron.HotSnapshotInterval = 1 * time.Minute
	}
	if cfg.Cron.HotSnapshotSize <= 0 {
		cfg.Cron.HotSnapshotSize = 1000
	}
	if cfg.Cron.HotSnapshotTTL <= 0 {
		cfg.Cron.HotSnapshotTTL = 30 * time.Minute
	}
	if cfg.Cron.HotColdUpdateInterval <= 0 {
		cfg.Cron.HotColdUpdateInterval = 24 * time.Hour
	}
	if cfg.Cron.HotColdUpdateSize <= 0 {
		cfg.Cron.HotColdUpdateSize = 5000
	}
	if cfg.Cron.FollowInboxBackfillInterval <= 0 {
		cfg.Cron.FollowInboxBackfillInterval = 5 * time.Minute
	}
	if cfg.Cron.FollowInboxBatchSize <= 0 {
		cfg.Cron.FollowInboxBatchSize = 200
	}

	if cfg.MySQL.MaxOpenConns == 0 {
		cfg.MySQL.MaxOpenConns = 20
	}
	if cfg.MySQL.MaxIdleConns == 0 {
		cfg.MySQL.MaxIdleConns = 10
	}
	if cfg.MySQL.ConnMaxLifetime == 0 {
		cfg.MySQL.ConnMaxLifetime = 30 * time.Minute
	}

	if cfg.JWT.Secret == "" {
		cfg.JWT.Secret = "change-me-in-production"
	}
	if cfg.JWT.Issuer == "" {
		cfg.JWT.Issuer = "ran-feed-server"
	}
	if cfg.JWT.Expire == 0 {
		cfg.JWT.Expire = 72 * time.Hour
	}

	if cfg.Kafka.GroupID == "" {
		cfg.Kafka.GroupID = "ran-feed-server"
	}
	if cfg.Kafka.Topics.CountCanal == "" {
		cfg.Kafka.Topics.CountCanal = "ran-feed-count-canal"
	}
	if cfg.Kafka.Topics.LikeAction == "" {
		cfg.Kafka.Topics.LikeAction = "ran-feed-like-action"
	}
	if cfg.Canal.Destination == "" {
		cfg.Canal.Destination = "example"
	}
	if cfg.Canal.Subscribe == "" {
		cfg.Canal.Subscribe = ".*\\.(ran_feed_like|ran_feed_favorite|ran_feed_comment|ran_feed_follow|ran_feed_content)"
	}
	if cfg.Canal.BatchSize <= 0 {
		cfg.Canal.BatchSize = 100
	}
	if cfg.Canal.PollInterval <= 0 {
		cfg.Canal.PollInterval = 300 * time.Millisecond
	}
	if cfg.Canal.ReconnectInterval <= 0 {
		cfg.Canal.ReconnectInterval = 3 * time.Second
	}
	if cfg.Canal.DedupeTTL <= 0 {
		cfg.Canal.DedupeTTL = 7 * 24 * time.Hour
	}

	cfg.MySQL.DSN = strings.TrimSpace(cfg.MySQL.DSN)
	cfg.Redis.Addr = strings.TrimSpace(cfg.Redis.Addr)
	cfg.JWT.Secret = strings.TrimSpace(cfg.JWT.Secret)
	cfg.Canal.Addr = strings.TrimSpace(cfg.Canal.Addr)
	cfg.Canal.Username = strings.TrimSpace(cfg.Canal.Username)
	cfg.Canal.Password = strings.TrimSpace(cfg.Canal.Password)
	cfg.Canal.Subscribe = strings.TrimSpace(cfg.Canal.Subscribe)
}
