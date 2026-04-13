package repository

import (
	"context"
	"errors"

	gmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"feed/internal/config"
)

func NewMySQL(ctx context.Context, cfg config.MySQLConfig) (*gorm.DB, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	if cfg.DSN == "" {
		return nil, errors.New("mysql enabled but dsn is empty")
	}

	db, err := gorm.Open(gmysql.Open(cfg.DSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return db, nil
}
