package model

import "time"

type FeedItem struct {
	ID            int64        `json:"id"`
	AuthorID      int64        `json:"author_id"`
	Author        *UserSummary `json:"author,omitempty"`
	ContentType   string       `json:"content_type"`
	Title         string       `json:"title"`
	Summary       string       `json:"summary"`
	Score         float64      `json:"score"`
	IsLiked       bool         `json:"is_liked"`
	LikeCount     int64        `json:"like_count"`
	FavoriteCount int64        `json:"favorite_count"`
	CommentCount  int64        `json:"comment_count"`
	CreatedAt     time.Time    `json:"created_at"`
}
