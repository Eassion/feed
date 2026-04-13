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
	MySQL MySQLConfig `yaml:"mysql"`
	Redis RedisConfig `yaml:"redis"`
	JWT   JWTConfig   `yaml:"jwt"`
	Kafka KafkaConfig `yaml:"kafka"`
}

type AppConfig struct {
	Name     string `yaml:"name"`
	HTTPAddr string `yaml:"http_addr"`
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
	Enabled bool     `yaml:"enabled"`
	Brokers []string `yaml:"brokers"`
	GroupID string   `yaml:"group_id"`
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

	cfg.MySQL.DSN = strings.TrimSpace(cfg.MySQL.DSN)
	cfg.Redis.Addr = strings.TrimSpace(cfg.Redis.Addr)
	cfg.JWT.Secret = strings.TrimSpace(cfg.JWT.Secret)
}
