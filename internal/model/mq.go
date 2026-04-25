package model

import (
	"fmt"
	"time"
)

type MQConsumeDedup struct {
	ID        int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Consumer  string    `json:"consumer" gorm:"column:consumer;uniqueIndex:uk_consumer_event"`
	EventID   string    `json:"event_id" gorm:"column:event_id;uniqueIndex:uk_consumer_event"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime;index"`
}

func (MQConsumeDedup) TableName() string {
	return "ran_feed_mq_consume_dedup"
}

const (
	LikeEventTypeLike       = "LIKE"
	LikeEventTypeCancelLike = "CANCEL_LIKE"
)

type LikeEvent struct {
	EventID       string    `json:"event_id"`
	EventType     string    `json:"event_type"`
	UserID        int64     `json:"user_id"`
	ContentID     int64     `json:"content_id"`
	ContentUserID int64     `json:"content_user_id"`
	Scene         string    `json:"scene,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

func (e LikeEvent) Key() string {
	return fmt.Sprintf("%d:%d", e.UserID, e.ContentID)
}
