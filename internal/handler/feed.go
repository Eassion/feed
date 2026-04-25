package handler

import (
	"errors"
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

//推荐流入口
func (h *FeedHandler) Recommend(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseFeedLimit(w, r)
	if !ok {
		return
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

//关注流入口
func (h *FeedHandler) Following(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseFeedLimit(w, r)
	if !ok {
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	result, err := h.feedService.ListFollowing(r.Context(), feedsvc.FollowRequest{
		UserID: userID,
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
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
	})
}

func (h *FeedHandler) UserFavorites(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseFeedLimit(w, r)
	if !ok {
		return
	}

	targetUserID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil || targetUserID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	viewerID, _ := middleware.UserIDFromContext(r.Context())
	result, err := h.feedService.ListUserFavorited(r.Context(), feedsvc.UserFavoriteRequest{
		ViewerID:       viewerID,
		FavoriteUserID: targetUserID,
		Limit:          limit,
		Cursor:         r.URL.Query().Get("cursor"),
	})
	if err != nil {
		if errors.Is(err, feedsvc.ErrInvalidFeedRequest) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":       result.Items,
		"limit":       limit,
		"next_cursor": result.NextCursor,
		"has_more":    result.HasMore,
	})
}

func parseFeedLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return 0, false
		}
		limit = parsed
	}

	return limit, true
}
