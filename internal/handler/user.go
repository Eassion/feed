package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"feed/internal/middleware"
	uploadsvc "feed/internal/service/upload"
	usersvc "feed/internal/service/user"
)

type UserHandler struct {
	userService   *usersvc.Service
	uploadService *uploadsvc.Service
}

func NewUserHandler(userService *usersvc.Service, uploadService *uploadsvc.Service) *UserHandler {
	return &UserHandler{userService: userService, uploadService: uploadService}
}

type updateProfileRequest struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
	Mobile   string `json:"mobile"`
	Email    string `json:"email"`
	Profile  string `json:"profile"`
}

func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	user, err := h.userService.GetProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, usersvc.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *UserHandler) Profile(w http.ResponseWriter, r *http.Request) {
	targetUserID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil || targetUserID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	viewerID, _ := middleware.UserIDFromContext(r.Context())
	profile, err := h.userService.GetHomepage(r.Context(), targetUserID, viewerID)
	if err != nil {
		switch {
		case errors.Is(err, usersvc.ErrUserNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, usersvc.ErrHomepageUnavailable):
			writeError(w, http.StatusInternalServerError, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, profile)
}

func (h *UserHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	var req updateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.userService.UpdateProfile(r.Context(), userID, usersvc.UpdateProfileRequest{
		Username: req.Username,
		Nickname: req.Nickname,
		Avatar:   req.Avatar,
		Bio:      firstNonEmpty(req.Bio, req.Profile),
		Mobile:   req.Mobile,
		Email:    req.Email,
	})
	if err != nil {
		switch {
		case errors.Is(err, usersvc.ErrInvalidProfile):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, usersvc.ErrUsernameTaken):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, usersvc.ErrUserNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *UserHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	if err := r.ParseMultipartForm(6 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, header, err := r.FormFile(uploadsvc.LocalUploadFormField)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing upload file")
		return
	}
	defer file.Close()

	result, err := h.uploadService.UploadAvatar(r.Context(), header.Filename, file)
	if err != nil {
		writeUploadError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, result)
}
