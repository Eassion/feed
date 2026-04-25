package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	countsvc "feed/internal/service/count"
)

type CountHandler struct {
	countService *countsvc.Service
}

func NewCountHandler(countService *countsvc.Service) *CountHandler {
	return &CountHandler{countService: countService}
}

func (h *CountHandler) Content(w http.ResponseWriter, r *http.Request) {
	contentID, err := strconv.ParseInt(r.PathValue("contentID"), 10, 64)
	if err != nil || contentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid content id")
		return
	}

	counter, err := h.countService.GetContentCounter(r.Context(), contentID)
	if err != nil {
		h.writeCountError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, counter)
}

func (h *CountHandler) User(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	counter, err := h.countService.GetUserCounter(r.Context(), userID)
	if err != nil {
		h.writeCountError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, counter)
}

func (h *CountHandler) BatchContent(w http.ResponseWriter, r *http.Request) {
	rawIDs := strings.Split(strings.TrimSpace(r.URL.Query().Get("ids")), ",")
	contentIDs := make([]int64, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		if rawID == "" {
			continue
		}
		contentID, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
		if err != nil || contentID <= 0 {
			writeError(w, http.StatusBadRequest, "ids must be a comma-separated list of positive integers")
			return
		}
		contentIDs = append(contentIDs, contentID)
	}
	if len(contentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}

	countMap, err := h.countService.BatchGetContentCountMap(r.Context(), contentIDs)
	if err != nil {
		h.writeCountError(w, err)
		return
	}

	items := make([]any, 0, len(contentIDs))
	for _, contentID := range contentIDs {
		counter, ok := countMap[contentID]
		if !ok {
			counter = countsvc.ZeroContentCount(contentID)
		}
		items = append(items, counter)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (h *CountHandler) writeCountError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, countsvc.ErrInvalidCountQuery):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
