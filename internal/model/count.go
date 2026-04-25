package model

import "time"

const (
	CountBizTypeLike      = int32(10)
	CountBizTypeFavorite  = int32(20)
	CountBizTypeComment   = int32(30)
	CountBizTypeFollowed  = int32(40)
	CountBizTypeFollowing = int32(41)

	CountTargetTypeContent = int32(10)
	CountTargetTypeUser    = int32(20)
)

type CountValue struct {
	ID         int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	BizType    int32     `json:"biz_type" gorm:"column:biz_type;uniqueIndex:uk_count_biz_target"`
	TargetType int32     `json:"target_type" gorm:"column:target_type;uniqueIndex:uk_count_biz_target;index:idx_count_target,priority:1"`
	TargetID   int64     `json:"target_id" gorm:"column:target_id;uniqueIndex:uk_count_biz_target;index:idx_count_target,priority:2"`
	Value      int64     `json:"value" gorm:"column:value"`
	Version    int64     `json:"version" gorm:"column:version"`
	CreatedAt  time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
	OwnerID    int64     `json:"owner_id" gorm:"column:owner_id;index"`
}

func (CountValue) TableName() string {
	return "ran_feed_count_value"
}

type ContentCount struct {
	ContentID     int64 `json:"content_id"`
	LikeCount     int64 `json:"like_count"`
	FavoriteCount int64 `json:"favorite_count"`
	CommentCount  int64 `json:"comment_count"`
}

type UserCount struct {
	UserID                 int64 `json:"user_id"`
	TotalLikesReceived     int64 `json:"total_likes_received"`
	TotalFavoritesReceived int64 `json:"total_favorites_received"`
	FollowersCount         int64 `json:"followers_count"`
	FollowingCount         int64 `json:"following_count"`
}
