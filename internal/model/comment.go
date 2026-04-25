package model

import "time"

type CommentItem struct {
	ID            int64        `json:"id"`
	ContentID     int64        `json:"content_id"`
	ContentUserID int64        `json:"content_user_id"`
	UserID        int64        `json:"user_id"`
	ReplyToUserID int64        `json:"reply_to_user_id"`
	ParentID      int64        `json:"parent_id"`
	RootID        int64        `json:"root_id"`
	Comment       string       `json:"comment"`
	IsDeleted     bool         `json:"is_deleted"`
	ReplyCount    int64        `json:"reply_count"`
	Author        *UserSummary `json:"author,omitempty"`
	ReplyToUser   *UserSummary `json:"reply_to_user,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type CommentPage struct {
	Items      []CommentItem `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more"`
}
