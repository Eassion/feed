package model

import "time"

const (
	InteractionStatusActive   = int32(10)
	InteractionStatusCanceled = int32(20)
)

type Like struct {
	ID            int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	UserID        int64     `json:"user_id" gorm:"column:user_id;uniqueIndex:uk_like_user_content"`
	ContentID     int64     `json:"content_id" gorm:"column:content_id;uniqueIndex:uk_like_user_content;index"`
	ContentUserID int64     `json:"content_user_id" gorm:"column:content_user_id;index"`
	Status        int32     `json:"status" gorm:"column:status"`
	Version       int64     `json:"version" gorm:"column:version"`
	IsDeleted     bool      `json:"-" gorm:"column:is_deleted"`
	CreatedBy     int64     `json:"-" gorm:"column:created_by"`
	UpdatedBy     int64     `json:"-" gorm:"column:updated_by"`
	CreatedAt     time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (Like) TableName() string {
	return "ran_feed_like"
}

type Favorite struct {
	ID            int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	UserID        int64     `json:"user_id" gorm:"column:user_id;uniqueIndex:uk_favorite_user_content;index:idx_favorite_user_created,priority:1"`
	Status        int32     `json:"status" gorm:"column:status"`
	ContentID     int64     `json:"content_id" gorm:"column:content_id;uniqueIndex:uk_favorite_user_content;index"`
	ContentUserID int64     `json:"content_user_id" gorm:"column:content_user_id;index"`
	CreatedBy     int64     `json:"-" gorm:"column:created_by"`
	UpdatedBy     int64     `json:"-" gorm:"column:updated_by"`
	CreatedAt     time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime;index:idx_favorite_user_created,priority:2,sort:desc"`
	UpdatedAt     time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (Favorite) TableName() string {
	return "ran_feed_favorite"
}

type Comment struct {
	ID            int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	ContentID     int64     `json:"content_id" gorm:"column:content_id;index"`
	ContentUserID int64     `json:"content_user_id" gorm:"column:content_user_id;index"`
	UserID        int64     `json:"user_id" gorm:"column:user_id;index"`
	ReplyToUserID int64     `json:"reply_to_user_id" gorm:"column:reply_to_user_id"`
	ParentID      int64     `json:"parent_id" gorm:"column:parent_id;index"`
	RootID        int64     `json:"root_id" gorm:"column:root_id;index"`
	Comment       string    `json:"comment" gorm:"column:comment"`
	Status        int32     `json:"status" gorm:"column:status"`
	Version       int64     `json:"version" gorm:"column:version"`
	IsDeleted     bool      `json:"-" gorm:"column:is_deleted"`
	CreatedBy     int64     `json:"-" gorm:"column:created_by"`
	UpdatedBy     int64     `json:"-" gorm:"column:updated_by"`
	CreatedAt     time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (Comment) TableName() string {
	return "ran_feed_comment"
}

type Follow struct {
	ID           int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	UserID       int64     `json:"user_id" gorm:"column:user_id;uniqueIndex:uk_follow_relation;index"`
	FollowUserID int64     `json:"follow_user_id" gorm:"column:follow_user_id;uniqueIndex:uk_follow_relation;index"`
	Status       int32     `json:"status" gorm:"column:status"`
	Version      int64     `json:"version" gorm:"column:version"`
	IsDeleted    bool      `json:"-" gorm:"column:is_deleted"`
	CreatedBy    int64     `json:"-" gorm:"column:created_by"`
	UpdatedBy    int64     `json:"-" gorm:"column:updated_by"`
	CreatedAt    time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (Follow) TableName() string {
	return "ran_feed_follow"
}

type ToggleResult struct {
	Changed bool `json:"changed"`
	Active  bool `json:"active"`
}
