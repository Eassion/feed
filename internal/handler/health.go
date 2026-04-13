package handler

import (
	"net/http"

	"feed/internal/svc"
)

type HealthHandler struct {
	svc *svc.ServiceContext
}

func NewHealthHandler(serviceContext *svc.ServiceContext) *HealthHandler {
	return &HealthHandler{svc: serviceContext}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"app":    h.svc.Config.App.Name,
		"deps": map[string]bool{
			"mysql": h.svc.MySQL != nil,
			"redis": h.svc.Redis != nil,
			"kafka": h.svc.Kafka.Enabled(),
		},
	})
}
