package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"feed/internal/middleware"
	usersvc "feed/internal/service/user"
)

type AuthHandler struct {
	userService *usersvc.Service
}

func NewAuthHandler(userService *usersvc.Service) *AuthHandler {
	return &AuthHandler{userService: userService}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
	Mobile   string `json:"mobile"`
	Email    string `json:"email"`
	Profile  string `json:"profile"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.userService.Register(r.Context(), usersvc.RegisterRequest{
		Username: req.Username,
		Password: req.Password,
		Nickname: req.Nickname,
		Avatar:   req.Avatar,
		Bio:      firstNonEmpty(req.Bio, req.Profile),
		Mobile:   req.Mobile,
		Email:    req.Email,
	})
	if err != nil {
		switch {
		case errors.Is(err, usersvc.ErrInvalidCredentials):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, usersvc.ErrUsernameTaken):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      result.Token,
		"expires_at": result.ExpiresAt,
		"user":       result.User,
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.userService.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, usersvc.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}

		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      result.Token,
		"expires_at": result.ExpiresAt,
		"user":       result.User,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	token, ok := middleware.TokenFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated token")
		return
	}

	if err := h.userService.Logout(r.Context(), token, userID); err != nil {
		if errors.Is(err, usersvc.ErrInvalidSession) {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "logout success",
	})
}
