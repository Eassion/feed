package model

import "time"

const (
	ContentTypeArticle = int32(10)
	ContentTypeVideo   = int32(20)

	ContentStatusDraft      = int32(10)
	ContentStatusProcessing = int32(20)
	ContentStatusPublished  = int32(30)
	ContentStatusFailed     = int32(40)
	ContentStatusDeleted    = int32(90)

	ContentVisibilityPublic  = "public"
	ContentVisibilityPrivate = "private"

	VideoTranscodeStatusPending    = int32(10)
	VideoTranscodeStatusProcessing = int32(20)
	VideoTranscodeStatusSucceeded  = int32(30)
	VideoTranscodeStatusFailed     = int32(40)
)

type Content struct {
	ID             int64      `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	UserID         int64      `json:"user_id" gorm:"column:user_id;index"`
	ContentType    int32      `json:"content_type" gorm:"column:content_type"`
	Status         int32      `json:"status" gorm:"column:status;index"`
	Visibility     string     `json:"visibility" gorm:"column:visibility"`
	LikeCount      int64      `json:"like_count" gorm:"column:like_count"`
	FavoriteCount  int64      `json:"favorite_count" gorm:"column:favorite_count"`
	CommentCount   int64      `json:"comment_count" gorm:"column:comment_count"`
	HotScore       float64    `json:"hot_score" gorm:"column:hot_score"`
	LastHotScoreAt *time.Time `json:"last_hot_score_at,omitempty" gorm:"column:last_hot_score_at"`
	Version        int64      `json:"version" gorm:"column:version"`
	IsDeleted      bool       `json:"-" gorm:"column:is_deleted"`
	CreatedBy      int64      `json:"-" gorm:"column:created_by"`
	UpdatedBy      int64      `json:"-" gorm:"column:updated_by"`
	CreatedAt      time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	PublishedAt    time.Time  `json:"published_at" gorm:"column:published_at"`
	UpdatedAt      time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (Content) TableName() string {
	return "ran_feed_content"
}

type Article struct {
	ID          int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	ContentID   int64     `json:"content_id" gorm:"column:content_id;uniqueIndex"`
	Title       string    `json:"title" gorm:"column:title"`
	Description string    `json:"description" gorm:"column:description"`
	Cover       string    `json:"cover" gorm:"column:cover"`
	Content     string    `json:"content" gorm:"column:content"`
	Version     int64     `json:"version" gorm:"column:version"`
	IsDeleted   bool      `json:"-" gorm:"column:is_deleted"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (Article) TableName() string {
	return "ran_feed_article"
}

type Video struct {
	ID              int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	ContentID       int64     `json:"content_id" gorm:"column:content_id;index"`
	Title           string    `json:"title" gorm:"column:title"`
	MediaID         string    `json:"media_id" gorm:"column:media_id"`
	OriginURL       string    `json:"origin_url" gorm:"column:origin_url"`
	HLSURL          string    `json:"hls_url" gorm:"column:hls_url"`
	CoverURL        string    `json:"cover_url" gorm:"column:cover_url"`
	Duration        int64     `json:"duration" gorm:"column:duration"`
	TranscodeStatus int32     `json:"transcode_status" gorm:"column:transcode_status"`
	FailReason      string    `json:"fail_reason" gorm:"column:fail_reason"`
	Version         int64     `json:"version" gorm:"column:version"`
	IsDeleted       bool      `json:"-" gorm:"column:is_deleted"`
	CreatedAt       time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (Video) TableName() string {
	return "ran_feed_video"
}

type ContentDetail struct {
	ID                int64        `json:"id"`
	AuthorID          int64        `json:"author_id"`
	Author            *UserSummary `json:"author,omitempty"`
	Type              string       `json:"type"`
	Status            int32        `json:"status"`
	Visibility        string       `json:"visibility"`
	Title             string       `json:"title"`
	Description       string       `json:"description,omitempty"`
	CoverURL          string       `json:"cover_url,omitempty"`
	Text              string       `json:"text,omitempty"`
	VideoURL          string       `json:"video_url,omitempty"`
	HLSURL            string       `json:"hls_url,omitempty"`
	MediaID           string       `json:"media_id,omitempty"`
	Duration          int64        `json:"duration,omitempty"`
	IsLiked           bool         `json:"is_liked"`
	IsFavorited       bool         `json:"is_favorited"`
	IsFollowingAuthor bool         `json:"is_following_author"`
	LikeCount         int64        `json:"like_count"`
	FavoriteCount     int64        `json:"favorite_count"`
	CommentCount      int64        `json:"comment_count"`
	HotScore          float64      `json:"hot_score"`
	CreatedAt         time.Time    `json:"created_at"`
	PublishedAt       time.Time    `json:"published_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
}

type ContentListItem struct {
	ID          int64     `json:"id"`
	AuthorID    int64     `json:"author_id"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	CoverURL    string    `json:"cover_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	PublishedAt time.Time `json:"published_at"`
}

func ContentTypeName(contentType int32) string {
	switch contentType {
	case ContentTypeVideo:
		return "video"
	default:
		return "article"
	}
}
