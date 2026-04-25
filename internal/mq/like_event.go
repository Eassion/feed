package mq

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"feed/internal/model"
	interactionrepo "feed/internal/repository/interaction"
	"github.com/segmentio/kafka-go"
)

const DefaultLikeActionTopic = "ran-feed-like-action"

type LikeProducer struct {
	manager *Manager
	topic   string
}

func NewLikeProducer(manager *Manager, topic string) *LikeProducer {
	if topic == "" {
		topic = DefaultLikeActionTopic
	}
	return &LikeProducer{manager: manager, topic: topic}
}

func (p *LikeProducer) SendLikeEvent(ctx context.Context, event model.LikeEvent) error {
	event.EventType = model.LikeEventTypeLike
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	return p.publish(ctx, event)
}

func (p *LikeProducer) SendCancelLikeEvent(ctx context.Context, event model.LikeEvent) error {
	event.EventType = model.LikeEventTypeCancelLike
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	return p.publish(ctx, event)
}

func (p *LikeProducer) publish(ctx context.Context, event model.LikeEvent) error {
	if p == nil || p.manager == nil {
		return nil
	}
	return p.manager.PublishJSON(ctx, p.topic, event.Key(), event)
}

type LikeEventFallback interface {
	HandleUserLikeChanged(ctx context.Context, contentID, authorID, delta int64)
}

type LikeConsumer struct {
	repo    *interactionrepo.Repository
	deduper interface {
		MarkOnce(ctx context.Context, eventID string) (bool, error)
	}
	fallback LikeEventFallback
	logger   *slog.Logger
}

func NewLikeConsumer(repo *interactionrepo.Repository, deduper interface {
	MarkOnce(ctx context.Context, eventID string) (bool, error)
}, logger *slog.Logger) *LikeConsumer {
	return &LikeConsumer{
		repo:    repo,
		deduper: deduper,
		logger:  logger,
	}
}

func (c *LikeConsumer) SetFallback(fallback LikeEventFallback) {
	c.fallback = fallback
}

func RegisterLikeHandlers(manager *Manager, topic string, consumer *LikeConsumer, logger *slog.Logger) {
	if manager == nil || consumer == nil {
		return
	}
	if topic == "" {
		topic = DefaultLikeActionTopic
	}

	manager.Register(topic, func(ctx context.Context, msg kafka.Message) error {
		event, err := decodeLikeEvent(msg.Value)
		if err != nil {
			return err
		}
		if err := consumer.Consume(ctx, event); err != nil {
			return err
		}
		if logger != nil {
			logger.Info("applied like event", "topic", msg.Topic, "event_id", event.EventID, "event_type", event.EventType, "content_id", event.ContentID, "user_id", event.UserID)
		}
		return nil
	})
}

func (c *LikeConsumer) Consume(ctx context.Context, event model.LikeEvent) error {
	if c == nil {
		return nil
	}
	if event.EventID == "" || event.UserID <= 0 || event.ContentID <= 0 {
		return nil
	}

	if c.deduper != nil {
		allowed, err := c.deduper.MarkOnce(ctx, event.EventID)
		if err != nil {
			return err
		}
		if !allowed {
			if c.logger != nil {
				c.logger.Info("skip duplicated like event", "event_id", event.EventID)
			}
			return nil
		}
	}

	status := model.InteractionStatusActive
	delta := int64(1)
	if event.EventType == model.LikeEventTypeCancelLike {
		status = model.InteractionStatusCanceled
		delta = -1
	}

	if err := c.repo.UpsertLike(ctx, &model.Like{
		UserID:        event.UserID,
		ContentID:     event.ContentID,
		ContentUserID: event.ContentUserID,
		Status:        status,
		IsDeleted:     false,
		CreatedBy:     event.UserID,
		UpdatedBy:     event.UserID,
		Version:       1,
	}); err != nil {
		return err
	}

	//只有canal没有配置时才走这个同步链路
	if c.fallback != nil {
		c.fallback.HandleUserLikeChanged(ctx, event.ContentID, event.ContentUserID, delta)
	}
	return nil
}

func decodeLikeEvent(payload []byte) (model.LikeEvent, error) {
	var event model.LikeEvent
	err := json.Unmarshal(payload, &event)
	return event, err
}

type SyncLikeProducer struct {
	consumer *LikeConsumer
}

func NewSyncLikeProducer(consumer *LikeConsumer) *SyncLikeProducer {
	return &SyncLikeProducer{consumer: consumer}
}

func (p *SyncLikeProducer) SendLikeEvent(ctx context.Context, event model.LikeEvent) error {
	if p == nil || p.consumer == nil {
		return nil
	}
	event.EventType = model.LikeEventTypeLike
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	return p.consumer.Consume(ctx, event)
}

func (p *SyncLikeProducer) SendCancelLikeEvent(ctx context.Context, event model.LikeEvent) error {
	if p == nil || p.consumer == nil {
		return nil
	}
	event.EventType = model.LikeEventTypeCancelLike
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	return p.consumer.Consume(ctx, event)
}

var ErrInvalidLikeEvent = errors.New("invalid like event")
