package svc

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"feed/internal/cache"
	"feed/internal/config"
	"feed/internal/cron"
	"feed/internal/mq"
	"feed/internal/repository"
	contentrepo "feed/internal/repository/content"
	countrepo "feed/internal/repository/count"
	feedrepo "feed/internal/repository/feed"
	interactionrepo "feed/internal/repository/interaction"
	mqdeduprepo "feed/internal/repository/mqdedup"
	userrepo "feed/internal/repository/user"
	commentsvc "feed/internal/service/comment"
	contentsvc "feed/internal/service/content"
	countsvc "feed/internal/service/count"
	feedsvc "feed/internal/service/feed"
	interactionsvc "feed/internal/service/interaction"
	uploadsvc "feed/internal/service/upload"
	usersvc "feed/internal/service/user"
	"feed/internal/upload"
	"feed/pkg/jwtutil"
)

type ServiceContext struct {
	Config             config.Config
	Logger             *slog.Logger
	MySQL              *gorm.DB
	Redis              *redis.Client
	JWT                *jwtutil.Manager
	Kafka              *mq.Manager
	Canal              *mq.CanalManager
	Cron               *cron.Runner
	ContentRepo        *contentrepo.Repository
	CountRepo          *countrepo.Repository
	InteractionRepo    *interactionrepo.Repository
	UserRepo           *userrepo.Repository
	FeedRepo           *feedrepo.Repository
	ContentService     *contentsvc.Service
	CountService       *countsvc.Service
	CommentService     *commentsvc.Service
	InteractionService *interactionsvc.Service
	UserService        *usersvc.Service
	FeedService        *feedsvc.Service
	UploadService      *uploadsvc.Service
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
	stateStore := cache.NewHashStateStore(redisClient)
	likeStore := cache.NewLikeStore(redisClient)
	countCacheStore := cache.NewCountCacheStore(redisClient, cache.CountCacheTTL)
	hotRankStore := cache.NewHotRankStore(redisClient)
	contentRepository := contentrepo.New(mysqlDB)
	countRepository := countrepo.New(mysqlDB)
	interactionRepository := interactionrepo.New(mysqlDB)
	mqDedupRepository := mqdeduprepo.New(mysqlDB, "count-canal-consumer")
	likeDedupRepository := mqdeduprepo.New(mysqlDB, "like-event-consumer")
	userRepository := userrepo.New(mysqlDB)
	feedRepository := feedrepo.New(mysqlDB, redisClient)
	contentService := contentsvc.New(contentRepository)
	countService := countsvc.New(countRepository, countCacheStore)
	userService := usersvc.New(userRepository, sessionStore, tokenManager)
	kafkaManager := mq.NewManager(cfg.Kafka, logger)
	canalManager := mq.NewCanalManager(cfg.Canal, kafkaManager, cfg.Kafka.Topics.CountCanal, logger)
	interactionService := interactionsvc.New(interactionRepository, contentRepository, stateStore, logger)
	feedService := feedsvc.New(feedRepository, userService, interactionService, countService, contentRepository, hotRankStore)
	likeConsumer := mq.NewLikeConsumer(interactionRepository, likeDedupRepository, logger)
	if !cfg.Canal.Enabled {
		likeConsumer.SetFallback(feedService)
	}
	var likeProducer interactionsvc.LikeEventProducer
	if cfg.Kafka.Enabled {
		likeProducer = mq.NewLikeProducer(kafkaManager, cfg.Kafka.Topics.LikeAction)
	} else {
		likeProducer = mq.NewSyncLikeProducer(likeConsumer)
	}
	interactionService.SetLikeFlow(likeStore, likeProducer)
	interactionService.SetFollowSyncer(feedService)
	interactionService.SetFavoriteSyncer(feedService)
	userService.SetHomepageProviders(contentRepository, countService, interactionService, countCacheStore)
	cronRunner := cron.NewRunner(cfg.Cron, logger, hotRankStore, contentRepository, feedService)
	countCanalConsumer := mq.NewCountCanalConsumer(countService, contentRepository, mqDedupRepository, hotRankStore, logger)
	uploadRoot := filepath.Join(os.TempDir(), cfg.App.Name, "uploads")
	uploadStore := upload.NewStore(uploadRoot, "/api/v1/assets")
	uploadService := uploadsvc.New(uploadStore, cfg.JWT.Secret, "/api/v1/uploads/objects")
	contentService.SetVideoTranscoder(uploadService)
	commentService := commentsvc.New(interactionRepository, cache.NewCommentStore(redisClient), userService)
	if !cfg.Canal.Enabled {
		commentService.SetDelegate(feedService)
	}
	interactionService.SetCommentSyncer(commentService)
	contentService.SetPublisher(feedService)
	contentService.SetCommentCleaner(commentService)
	contentService.SetDetailProviders(userService, interactionService, countService)
	mq.RegisterDefaultHandlers(kafkaManager, cfg.Kafka.Topics.CountCanal, countCanalConsumer, logger)
	mq.RegisterLikeHandlers(kafkaManager, cfg.Kafka.Topics.LikeAction, likeConsumer, logger)
	mq.RegisterDefaultCanalStrategies(canalManager)

	return &ServiceContext{
		Config:             cfg,
		Logger:             logger,
		MySQL:              mysqlDB,
		Redis:              redisClient,
		JWT:                tokenManager,
		Kafka:              kafkaManager,
		Canal:              canalManager,
		Cron:               cronRunner,
		ContentRepo:        contentRepository,
		CountRepo:          countRepository,
		InteractionRepo:    interactionRepository,
		UserRepo:           userRepository,
		FeedRepo:           feedRepository,
		ContentService:     contentService,
		CountService:       countService,
		CommentService:     commentService,
		InteractionService: interactionService,
		UserService:        userService,
		FeedService:        feedService,
		UploadService:      uploadService,
	}, nil
}

func (s *ServiceContext) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}

	if s.Cron != nil {
		if err := s.Cron.Start(ctx); err != nil {
			return err
		}
	}
	if s.Kafka != nil {
		if err := s.Kafka.Start(ctx); err != nil {
			return err
		}
	}
	if s.Canal == nil {
		return nil
	}
	if err := s.Canal.Start(ctx); err != nil {
		return err
	}
	return nil
}

func (s *ServiceContext) Close() {
	if s == nil {
		return
	}

	if s.Kafka != nil {
		_ = s.Kafka.Close()
	}
	if s.Canal != nil {
		_ = s.Canal.Close()
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
