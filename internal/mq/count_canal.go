package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"feed/internal/cache"
	contentrepo "feed/internal/repository/content"
	countrepo "feed/internal/repository/count"
	countsvc "feed/internal/service/count"
	"github.com/segmentio/kafka-go"
)

const (
	ActionInsert = "INSERT"
	ActionUpdate = "UPDATE"
	ActionDelete = "DELETE"
)

const (
	DefaultCountCanalTopic = "ran-feed-count-canal"

	hotRankLikeDelta     = int64(3)
	hotRankFavoriteDelta = int64(5)
	hotRankCommentDelta  = int64(4)
)

type CountCanalEvent struct {
	EventID    string            `json:"event_id"`
	Schema     string            `json:"schema"`
	Table      string            `json:"table"`
	Action     string            `json:"action"`
	ContentID  int64             `json:"content_id,omitempty"`
	UserID     int64             `json:"user_id,omitempty"`
	FolloweeID int64             `json:"followee_id,omitempty"`
	AuthorID   int64             `json:"author_id,omitempty"`
	Status     string            `json:"status,omitempty"`
	OccurredAt time.Time         `json:"occurred_at"`
	Logfile    string            `json:"logfile,omitempty"`
	LogOffset  int64             `json:"log_offset,omitempty"`
	Before     map[string]string `json:"before,omitempty"`
	After      map[string]string `json:"after,omitempty"`
}

func (e CountCanalEvent) Key() string {
	switch {
	case e.ContentID > 0:
		return strconv.FormatInt(e.ContentID, 10)
	case e.UserID > 0:
		return strconv.FormatInt(e.UserID, 10)
	default:
		return e.EventID
	}
}

func decodeCountCanalEvent(payload []byte) (CountCanalEvent, error) {
	var event CountCanalEvent
	err := json.Unmarshal(payload, &event)
	return event, err
}

type CountCanalConsumer struct {
	counts   *countsvc.Service
	contents *contentrepo.Repository
	deduper  interface {
		MarkOnce(ctx context.Context, eventID string) (bool, error)
	}
	hotrank *cache.HotRankStore
	logger  *slog.Logger
}

func NewCountCanalConsumer(
	counts *countsvc.Service,
	contents *contentrepo.Repository,
	deduper interface {
		MarkOnce(ctx context.Context, eventID string) (bool, error)
	},
	hotrank *cache.HotRankStore,
	logger *slog.Logger,
) *CountCanalConsumer {
	return &CountCanalConsumer{
		counts:   counts,
		contents: contents,
		deduper:  deduper,
		hotrank:  hotrank,
		logger:   logger,
	}
}

func RegisterDefaultHandlers(manager *Manager, topic string, consumer *CountCanalConsumer, logger *slog.Logger) {
	if manager == nil || consumer == nil {
		return
	}
	if topic == "" {
		topic = DefaultCountCanalTopic
	}

	manager.Register(topic, func(ctx context.Context, msg kafka.Message) error {
		event, err := decodeCountCanalEvent(msg.Value)
		if err != nil {
			return err
		}

		if err := consumer.Handle(ctx, event); err != nil {
			return err
		}
		if logger != nil {
			logger.Info("applied count canal event", "topic", msg.Topic, "table", event.Table, "action", event.Action, "event_id", event.EventID)
		}
		return nil
	})
}

func (c *CountCanalConsumer) Handle(ctx context.Context, event CountCanalEvent) error {
	if c == nil {
		return nil
	}

	allowed, err := c.deduper.MarkOnce(ctx, event.EventID)
	if err != nil {
		return err
	}
	if !allowed {
		if c.logger != nil {
			c.logger.Info("skip duplicated canal count event", "event_id", event.EventID)
		}
		return nil
	}

	switch event.Table {
	case "ran_feed_like":
		return c.handleLikeEvent(ctx, event)
	case "ran_feed_favorite":
		return c.handleFavoriteEvent(ctx, event)
	case "ran_feed_comment":
		return c.handleCommentEvent(ctx, event)
	case "ran_feed_follow":
		return c.handleFollowEvent(ctx, event)
	case "ran_feed_content":
		return c.handleContentEvent(ctx, event)
	default:
		return nil
	}
}

func (c *CountCanalConsumer) handleLikeEvent(ctx context.Context, event CountCanalEvent) error {
	if event.ContentID <= 0 {
		return nil
	}

	authorID, err := c.resolveAuthorID(ctx, event)
	if err != nil {
		return err
	}

	delta := detectDelta(event.Action, likeRowActive(event.Before), likeRowActive(event.After))
	if delta == 0 {
		return nil
	}

	if err := c.counts.ApplyMutation(ctx, countrepo.Mutation{
		ContentID:          event.ContentID,
		LikeDelta:          delta,
		UserID:             authorID,
		LikesReceivedDelta: delta,
	}); err != nil {
		return err
	}

	if err := c.invalidateAfterMutation(ctx, event.ContentID, authorID); err != nil {
		return err
	}
	return c.hotrank.AddDelta(ctx, event.ContentID, float64(hotRankLikeDelta*delta))
}

func (c *CountCanalConsumer) handleFavoriteEvent(ctx context.Context, event CountCanalEvent) error {
	if event.ContentID <= 0 {
		return nil
	}

	authorID, err := c.resolveAuthorID(ctx, event)
	if err != nil {
		return err
	}

	delta := detectDelta(event.Action, favoriteRowActive(event.Before), favoriteRowActive(event.After))
	if delta == 0 {
		return nil
	}

	if err := c.counts.ApplyMutation(ctx, countrepo.Mutation{
		ContentID:              event.ContentID,
		FavoriteDelta:          delta,
		UserID:                 authorID,
		FavoritesReceivedDelta: delta,
	}); err != nil {
		return err
	}

	if err := c.invalidateAfterMutation(ctx, event.ContentID, authorID); err != nil {
		return err
	}
	return c.hotrank.AddDelta(ctx, event.ContentID, float64(hotRankFavoriteDelta*delta))
}

func (c *CountCanalConsumer) handleCommentEvent(ctx context.Context, event CountCanalEvent) error {
	if event.ContentID <= 0 {
		return nil
	}

	delta := detectDelta(event.Action, commentRowActive(event.Before), commentRowActive(event.After))
	if delta == 0 {
		return nil
	}

	if err := c.counts.ApplyMutation(ctx, countrepo.Mutation{
		ContentID:    event.ContentID,
		CommentDelta: delta,
	}); err != nil {
		return err
	}

	if err := c.counts.InvalidateContentCounter(ctx, event.ContentID); err != nil {
		return err
	}
	return c.hotrank.AddDelta(ctx, event.ContentID, float64(hotRankCommentDelta*delta))
}

func (c *CountCanalConsumer) handleContentEvent(ctx context.Context, event CountCanalEvent) error {
	if event.ContentID <= 0 {
		return nil
	}
	if !isDeletedContentTransition(event.Action, event.Before, event.After) {
		return nil
	}

	if err := c.counts.InvalidateContentCounter(ctx, event.ContentID); err != nil {
		return err
	}
	if event.AuthorID > 0 {
		if err := c.counts.InvalidateUserCounter(ctx, event.AuthorID); err != nil {
			return err
		}
	}
	return c.hotrank.RemoveContent(ctx, event.ContentID)
}

func (c *CountCanalConsumer) handleFollowEvent(ctx context.Context, event CountCanalEvent) error {
	if event.UserID <= 0 || event.FolloweeID <= 0 {
		return nil
	}

	delta := detectDelta(event.Action, followRowActive(event.Before), followRowActive(event.After))
	if delta == 0 {
		return nil
	}

	if err := c.counts.ApplyMutation(ctx, countrepo.Mutation{
		UserID:         event.FolloweeID,
		FollowersDelta: delta,
	}); err != nil {
		return err
	}
	if err := c.counts.InvalidateUserCounter(ctx, event.FolloweeID); err != nil {
		return err
	}

	if err := c.counts.ApplyMutation(ctx, countrepo.Mutation{
		UserID:         event.UserID,
		FollowingDelta: delta,
	}); err != nil {
		return err
	}
	return c.counts.InvalidateUserCounter(ctx, event.UserID)
}

func (c *CountCanalConsumer) invalidateAfterMutation(ctx context.Context, contentID, authorID int64) error {
	if err := c.counts.InvalidateContentCounter(ctx, contentID); err != nil {
		return err
	}
	if authorID > 0 {
		if err := c.counts.InvalidateUserCounter(ctx, authorID); err != nil {
			return err
		}
	}
	return nil
}

func (c *CountCanalConsumer) resolveAuthorID(ctx context.Context, event CountCanalEvent) (int64, error) {
	if event.AuthorID > 0 {
		return event.AuthorID, nil
	}
	if c.contents == nil || event.ContentID <= 0 {
		return 0, nil
	}
	authorID, err := c.contents.GetAuthorID(ctx, event.ContentID)
	if err == nil {
		return authorID, nil
	}
	if err == contentrepo.ErrContentNotFound {
		return parseEventInt64(event.After["content_user_id"], event.Before["content_user_id"], event.After["user_id"], event.Before["user_id"]), nil
	}
	return 0, err
}

func detectDelta(action string, beforeActive, afterActive bool) int64 {
	switch action {
	case ActionInsert:
		if afterActive {
			return 1
		}
	case ActionUpdate:
		switch {
		case !beforeActive && afterActive:
			return 1
		case beforeActive && !afterActive:
			return -1
		}
	case ActionDelete:
		if beforeActive {
			return -1
		}
	}
	return 0
}

func likeRowActive(values map[string]string) bool {
	return interactionRowActive(values)
}

func commentRowActive(values map[string]string) bool {
	return interactionRowActive(values)
}

func followRowActive(values map[string]string) bool {
	return interactionRowActive(values)
}

func interactionRowActive(values map[string]string) bool {
	if len(values) == 0 {
		return false
	}
	return parseEventInt64(values["status"]) == 10 && !parseEventBool(values["is_deleted"])
}

func favoriteRowActive(values map[string]string) bool {
	if len(values) == 0 {
		return false
	}
	return parseEventInt64(values["status"]) == 10
}

func parseEventBool(value string) bool {
	switch value {
	case "1", "true", "TRUE", "True":
		return true
	default:
		return false
	}
}

func parseEventInt64(values ...string) int64 {
	for _, value := range values {
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func newCanalEventID(logfile string, offset int64, table string, action string, rowIndex int) string {
	return fmt.Sprintf("%s:%d:%s:%s:%d", logfile, offset, table, action, rowIndex)
}
