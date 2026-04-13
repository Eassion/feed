package svc

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"feed/internal/cache"
	"feed/internal/config"
	"feed/internal/mq"
	"feed/internal/repository"
	feedrepo "feed/internal/repository/feed"
	userrepo "feed/internal/repository/user"
	feedsvc "feed/internal/service/feed"
	usersvc "feed/internal/service/user"
	"feed/pkg/jwtutil"
)

type ServiceContext struct {
	Config      config.Config
	Logger      *slog.Logger
	MySQL       *gorm.DB
	Redis       *redis.Client
	JWT         *jwtutil.Manager
	Kafka       *mq.Manager
	UserRepo    *userrepo.Repository
	FeedRepo    *feedrepo.Repository
	UserService *usersvc.Service
	FeedService *feedsvc.Service
}

func NewServiceContext(ctx context.Context, cfg config.Config, logger *slog.Logger) (*ServiceContext, error) {
	mysqlDB, err := repository.NewMySQL(ctx, cfg.MySQL)
	if err != nil {
		return nil, err
	}

	redisClient, err := cache.NewRedis(ctx, cfg.Redis)
	if err != nil {
		if mysqlDB != nil {
			if sqlDB, dbErr := mysqlDB.DB(); dbErr == nil {
				_ = sqlDB.Close()
			}
		}
		return nil, err
	}

	tokenManager := jwtutil.NewManager(cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.Expire)
	sessionStore := cache.NewSessionStore(redisClient, cfg.JWT.Expire)
	userRepository := userrepo.New(mysqlDB)
	feedRepository := feedrepo.New(mysqlDB, redisClient)
	userService := usersvc.New(userRepository, sessionStore, tokenManager)
	feedService := feedsvc.New(feedRepository)
	kafkaManager := mq.NewManager(cfg.Kafka, logger)
	mq.RegisterDefaultHandlers(kafkaManager, logger)

	return &ServiceContext{
		Config:      cfg,
		Logger:      logger,
		MySQL:       mysqlDB,
		Redis:       redisClient,
		JWT:         tokenManager,
		Kafka:       kafkaManager,
		UserRepo:    userRepository,
		FeedRepo:    feedRepository,
		UserService: userService,
		FeedService: feedService,
	}, nil
}

func (s *ServiceContext) Start(ctx context.Context) error {
	if s == nil || s.Kafka == nil {
		return nil
	}

	return s.Kafka.Start(ctx)
}

func (s *ServiceContext) Close() {
	if s == nil {
		return
	}

	if s.Kafka != nil {
		_ = s.Kafka.Close()
	}
	if s.Redis != nil {
		_ = s.Redis.Close()
	}
	if s.MySQL != nil {
		if sqlDB, err := s.MySQL.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}
