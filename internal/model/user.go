package model

import "time"

const (
	UserStatusActive   = int32(10)
	UserStatusDisabled = int32(20)
)

type User struct {
	ID           int64      `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Username     string     `json:"username" gorm:"column:username"`
	Nickname     string     `json:"nickname,omitempty" gorm:"column:nickname"`
	Avatar       string     `json:"avatar,omitempty" gorm:"column:avatar"`
	Bio          string     `json:"bio,omitempty" gorm:"column:bio"`
	Mobile       *string    `json:"mobile,omitempty" gorm:"column:mobile"`
	Email        *string    `json:"email,omitempty" gorm:"column:email"`
	PasswordHash string     `json:"-" gorm:"column:password_hash"`
	PasswordSalt string     `json:"-" gorm:"column:password_salt"`
	Gender       int32      `json:"gender,omitempty" gorm:"column:gender"`
	Birthday     *time.Time `json:"birthday,omitempty" gorm:"column:birthday"`
	Status       int32      `json:"status" gorm:"column:status"`
	Version      int64      `json:"version" gorm:"column:version"`
	IsDeleted    bool       `json:"-" gorm:"column:is_deleted"`
	CreatedBy    int64      `json:"-" gorm:"column:created_by"`
	UpdatedBy    int64      `json:"-" gorm:"column:updated_by"`
	CreatedAt    time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (User) TableName() string {
	return "ran_feed_user"
}

type UserSummary struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

type UserHomepage struct {
	ID                     int64  `json:"id"`
	Username               string `json:"username"`
	Nickname               string `json:"nickname,omitempty"`
	Avatar                 string `json:"avatar,omitempty"`
	Bio                    string `json:"bio,omitempty"`
	WorksCount             int64  `json:"works_count"`
	TotalLikesReceived     int64  `json:"total_likes_received"`
	TotalFavoritesReceived int64  `json:"total_favorites_received"`
	FollowersCount         int64  `json:"followers_count"`
	FollowingCount         int64  `json:"following_count"`
	IsFollowing            bool   `json:"is_following"`
}
