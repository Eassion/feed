package mq

import (
	"context"
	"log/slog"

	"github.com/segmentio/kafka-go"
)

const (
	TopicInteractionCreated = "feed.interaction.created"
	TopicCountRefresh       = "feed.count.refresh"
)

func RegisterDefaultHandlers(manager *Manager, logger *slog.Logger) {
	if manager == nil {
		return
	}

	manager.Register(TopicInteractionCreated, func(_ context.Context, msg kafka.Message) error {
		logger.Info("received interaction event", "topic", msg.Topic, "key", string(msg.Key))
		return nil
	})

	manager.Register(TopicCountRefresh, func(_ context.Context, msg kafka.Message) error {
		logger.Info("received count refresh event", "topic", msg.Topic, "key", string(msg.Key))
		return nil
	})
}
