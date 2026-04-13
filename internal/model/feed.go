package model

import "time"

type FeedItem struct {
	ID          int64     `json:"id"`
	AuthorID    int64     `json:"author_id"`
	ContentType string    `json:"content_type"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	Score       float64   `json:"score"`
	CreatedAt   time.Time `json:"created_at"`
}
