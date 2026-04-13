package handler

import (
	"net/http"
	"strconv"

	"feed/internal/middleware"
	feedsvc "feed/internal/service/feed"
)

type FeedHandler struct {
	feedService *feedsvc.Service
}

func NewFeedHandler(feedService *feedsvc.Service) *FeedHandler {
	return &FeedHandler{feedService: feedService}
}

func (h *FeedHandler) Recommend(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}

	userID, _ := middleware.UserIDFromContext(r.Context())
	result, err := h.feedService.ListRecommended(r.Context(), feedsvc.RecommendRequest{
		UserID:     userID,
		Limit:      limit,
		Cursor:     r.URL.Query().Get("cursor"),
		SnapshotID: r.URL.Query().Get("snapshot_id"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":       result.Items,
		"limit":       limit,
		"next_cursor": result.NextCursor,
		"has_more":    result.HasMore,
		"snapshot_id": result.SnapshotID,
	})
}
