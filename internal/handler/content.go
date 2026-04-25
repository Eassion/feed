package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"feed/internal/middleware"
	contentsvc "feed/internal/service/content"
	feedsvc "feed/internal/service/feed"
	uploadsvc "feed/internal/service/upload"
)

type ContentHandler struct {
	contentService *contentsvc.Service
	feedService    *feedsvc.Service
	uploadService  *uploadsvc.Service
}

type publishArticleRequest struct {
	Title          string `json:"title"`
	CoverURL       string `json:"cover_url"`
	ArticleContent string `json:"article_content"`
	Visibility     string `json:"visibility"`
}

type publishVideoRequest struct {
	Title      string `json:"title"`
	CoverURL   string `json:"cover_url"`
	VideoURL   string `json:"video_url"`
	Duration   int64  `json:"duration"`
	Visibility string `json:"visibility"`
}

type uploadCredentialRequest struct {
	Scene    string `json:"scene"`
	FileName string `json:"file_name"`
	Ext      string `json:"ext"`
	MimeType string `json:"mime_type"`
}

func NewContentHandler(contentService *contentsvc.Service, feedService *feedsvc.Service, uploadService *uploadsvc.Service) *ContentHandler {
	return &ContentHandler{contentService: contentService, feedService: feedService, uploadService: uploadService}
}

func (h *ContentHandler) PublishArticle(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	var req publishArticleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	detail, err := h.contentService.PublishArticle(r.Context(), contentsvc.PublishArticleRequest{
		AuthorID:       userID,
		Title:          req.Title,
		CoverURL:       req.CoverURL,
		ArticleContent: req.ArticleContent,
		Visibility:     req.Visibility,
	})
	if err != nil {
		if errors.Is(err, contentsvc.ErrInvalidContent) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, detail)
}

func (h *ContentHandler) PublishVideo(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	var req publishVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	detail, err := h.contentService.PublishVideo(r.Context(), contentsvc.PublishVideoRequest{
		AuthorID:   userID,
		Title:      req.Title,
		CoverURL:   req.CoverURL,
		VideoURL:   req.VideoURL,
		Duration:   req.Duration,
		Visibility: req.Visibility,
	})
	if err != nil {
		if errors.Is(err, contentsvc.ErrInvalidContent) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, detail)
}

func (h *ContentHandler) UploadCredentials(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	var req uploadCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	credential, err := h.uploadService.CreateContentCredential(r.Context(), uploadsvc.UploadCredentialRequest{
		Scene:    req.Scene,
		FileName: req.FileName,
		Ext:      req.Ext,
		MimeType: req.MimeType,
	})
	if err != nil {
		writeUploadError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, credential)
}

func (h *ContentHandler) Detail(w http.ResponseWriter, r *http.Request) {
	contentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || contentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid content id")
		return
	}

	viewerID, _ := middleware.UserIDFromContext(r.Context())
	detail, err := h.contentService.GetDetail(r.Context(), contentID, viewerID)
	if err != nil {
		switch {
		case errors.Is(err, contentsvc.ErrInvalidContent):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, contentsvc.ErrContentNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (h *ContentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	contentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || contentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid content id")
		return
	}

	if err := h.contentService.Delete(r.Context(), contentID, userID); err != nil {
		switch {
		case errors.Is(err, contentsvc.ErrInvalidContent):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, contentsvc.ErrContentNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, contentsvc.ErrContentForbidden):
			writeError(w, http.StatusForbidden, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "content deleted",
	})
}

func (h *ContentHandler) ListByUser(w http.ResponseWriter, r *http.Request) {
	authorID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil || authorID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}

	viewerID, _ := middleware.UserIDFromContext(r.Context())
	page, err := h.feedService.ListUserPublished(r.Context(), feedsvc.UserPublishRequest{
		UserID:   viewerID,
		AuthorID: authorID,
		Cursor:   r.URL.Query().Get("cursor"),
		Limit:    limit,
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
		"items":       page.Items,
		"limit":       limit,
		"next_cursor": page.NextCursor,
		"has_more":    page.HasMore,
	})
}
