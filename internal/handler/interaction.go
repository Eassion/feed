package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"feed/internal/middleware"
	commentsvc "feed/internal/service/comment"
	interactionsvc "feed/internal/service/interaction"
)

type InteractionHandler struct {
	interactionService *interactionsvc.Service
	commentService     *commentsvc.Service
}

type contentActionRequest struct {
	ContentID int64  `json:"content_id"`
	Scene     string `json:"scene,omitempty"`
}

type commentRequest struct {
	ContentID     int64  `json:"content_id"`
	ParentID      int64  `json:"parent_id,omitempty"`
	RootID        int64  `json:"root_id,omitempty"`
	ReplyToUserID int64  `json:"reply_to_user_id,omitempty"`
	Text          string `json:"text"`
}

type followRequest struct {
	FolloweeID int64 `json:"followee_id"`
}

func NewInteractionHandler(interactionService *interactionsvc.Service, commentService *commentsvc.Service) *InteractionHandler {
	return &InteractionHandler{interactionService: interactionService, commentService: commentService}
}

func (h *InteractionHandler) Like(w http.ResponseWriter, r *http.Request) {
	h.toggleContentAction(w, r, func(userID, contentID int64, scene string) (any, error) {
		return h.interactionService.LikeWithScene(r.Context(), userID, contentID, scene)
	})
}

func (h *InteractionHandler) Unlike(w http.ResponseWriter, r *http.Request) {
	contentID, err := strconv.ParseInt(r.PathValue("contentID"), 10, 64)
	if err != nil || contentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid content id")
		return
	}
	h.toggleContentActionByID(w, r, contentID, r.URL.Query().Get("scene"), func(userID, contentID int64, scene string) (any, error) {
		return h.interactionService.UnlikeWithScene(r.Context(), userID, contentID, scene)
	})
}

func (h *InteractionHandler) Favorite(w http.ResponseWriter, r *http.Request) {
	h.toggleContentAction(w, r, func(userID, contentID int64, _ string) (any, error) {
		return h.interactionService.Favorite(r.Context(), userID, contentID)
	})
}

func (h *InteractionHandler) Unfavorite(w http.ResponseWriter, r *http.Request) {
	contentID, err := strconv.ParseInt(r.PathValue("contentID"), 10, 64)
	if err != nil || contentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid content id")
		return
	}
	h.toggleContentActionByID(w, r, contentID, "", func(userID, contentID int64, _ string) (any, error) {
		return h.interactionService.Unfavorite(r.Context(), userID, contentID)
	})
}

func (h *InteractionHandler) Comment(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	var req commentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	comment, err := h.interactionService.Comment(r.Context(), userID, interactionsvc.CommentInput{
		ContentID:     req.ContentID,
		ParentID:      req.ParentID,
		RootID:        req.RootID,
		ReplyToUserID: req.ReplyToUserID,
		Text:          req.Text,
	})
	if err != nil {
		h.writeInteractionError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, comment)
}

func (h *InteractionHandler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	commentID, err := strconv.ParseInt(r.PathValue("commentID"), 10, 64)
	if err != nil || commentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	if err := h.interactionService.DeleteComment(r.Context(), userID, commentID); err != nil {
		h.writeInteractionError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"message": "comment deleted"})
}

func (h *InteractionHandler) ListContentComments(w http.ResponseWriter, r *http.Request) {
	contentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || contentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid content id")
		return
	}

	limit, ok := parseFeedLimit(w, r)
	if !ok {
		return
	}

	page, err := h.commentService.ListContentComments(r.Context(), contentID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		if errors.Is(err, commentsvc.ErrInvalidCommentQuery) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":       page.Items,
		"limit":       limit,
		"next_cursor": page.NextCursor,
		"has_more":    page.HasMore,
	})
}

func (h *InteractionHandler) ListReplies(w http.ResponseWriter, r *http.Request) {
	rootID, err := strconv.ParseInt(r.PathValue("commentID"), 10, 64)
	if err != nil || rootID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	limit, ok := parseFeedLimit(w, r)
	if !ok {
		return
	}

	page, err := h.commentService.ListReplies(r.Context(), rootID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		if errors.Is(err, commentsvc.ErrInvalidCommentQuery) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":       page.Items,
		"limit":       limit,
		"next_cursor": page.NextCursor,
		"has_more":    page.HasMore,
	})
}

func (h *InteractionHandler) Follow(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	var req followRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.interactionService.Follow(r.Context(), userID, req.FolloweeID)
	if err != nil {
		h.writeInteractionError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *InteractionHandler) Unfollow(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	followeeID, err := strconv.ParseInt(r.PathValue("followeeID"), 10, 64)
	if err != nil || followeeID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid followee id")
		return
	}

	result, err := h.interactionService.Unfollow(r.Context(), userID, followeeID)
	if err != nil {
		h.writeInteractionError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *InteractionHandler) toggleContentAction(w http.ResponseWriter, r *http.Request, action func(userID, contentID int64, scene string) (any, error)) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	var req contentActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.toggleContentActionByID(w, r, req.ContentID, req.Scene, actionWithUser(userID, action))
}

func (h *InteractionHandler) toggleContentActionByID(w http.ResponseWriter, r *http.Request, contentID int64, scene string, action func(userID, contentID int64, scene string) (any, error)) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}
	if contentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid content id")
		return
	}

	result, err := action(userID, contentID, scene)
	if err != nil {
		h.writeInteractionError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func actionWithUser(userID int64, action func(userID, contentID int64, scene string) (any, error)) func(int64, int64, string) (any, error) {
	return func(_ int64, contentID int64, scene string) (any, error) {
		return action(userID, contentID, scene)
	}
}

func (h *InteractionHandler) writeInteractionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, interactionsvc.ErrInvalidInteraction):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, interactionsvc.ErrContentNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, interactionsvc.ErrCommentNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
