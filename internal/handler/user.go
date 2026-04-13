package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"feed/internal/middleware"
	usersvc "feed/internal/service/user"
)

type UserHandler struct {
	userService *usersvc.Service
}

func NewUserHandler(userService *usersvc.Service) *UserHandler {
	return &UserHandler{userService: userService}
}

type updateProfileRequest struct {
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
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
		Avatar:   req.Avatar,
		Profile:  req.Profile,
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
