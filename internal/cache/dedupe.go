package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type EventDeduper struct {
	client *redis.Client
	ttl    time.Duration
}

func NewEventDeduper(client *redis.Client, ttl time.Duration) *EventDeduper {
	return &EventDeduper{
		client: client,
		ttl:    ttl,
	}
}

func (d *EventDeduper) MarkOnce(ctx context.Context, eventID string) (bool, error) {
	if eventID == "" {
		return true, nil
	}
	if d == nil || d.client == nil {
		return true, nil
	}

	return d.client.SetNX(ctx, CanalCountDedupKey(eventID), "1", d.ttl).Result()
}
