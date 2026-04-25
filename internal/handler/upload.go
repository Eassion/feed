package handler

import (
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	uploadsvc "feed/internal/service/upload"
)

type UploadHandler struct {
	uploadService *uploadsvc.Service
}

func NewUploadHandler(uploadService *uploadsvc.Service) *UploadHandler {
	return &UploadHandler{uploadService: uploadService}
}

func (h *UploadHandler) UploadObject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, header, err := r.FormFile(uploadsvc.LocalUploadFormField)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing upload file")
		return
	}
	defer file.Close()

	expiresAt, err := time.Parse(time.RFC3339, r.FormValue("expires_at"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid expires_at")
		return
	}

	result, err := h.uploadService.UploadObject(r.Context(), uploadsvc.ObjectUploadRequest{
		ObjectKey: r.FormValue("key"),
		Scene:     r.FormValue("scene"),
		MimeType:  r.FormValue("mime_type"),
		ExpiresAt: expiresAt,
		Signature: r.FormValue("signature"),
	}, header.Filename, file)
	if err != nil {
		writeUploadError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

func (h *UploadHandler) ServeObject(w http.ResponseWriter, r *http.Request) {
	objectKey := r.PathValue("objectKey")
	if objectKey == "" {
		http.NotFound(w, r)
		return
	}

	file, err := h.uploadService.Open(objectKey)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	if contentType := mime.TypeByExtension(filepath.Ext(objectKey)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, filepath.Base(objectKey), time.Time{}, file)
}

func writeUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, uploadsvc.ErrInvalidUploadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, uploadsvc.ErrUnsupportedUpload):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, uploadsvc.ErrExpiredCredential):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, uploadsvc.ErrInvalidCredential):
		writeError(w, http.StatusUnauthorized, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
