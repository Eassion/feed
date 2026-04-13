package handler

import (
	"log/slog"
	"net/http"

	"feed/internal/middleware"
	"feed/internal/svc"
)

func NewRouter(serviceContext *svc.ServiceContext, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	healthHandler := NewHealthHandler(serviceContext)
	authHandler := NewAuthHandler(serviceContext.UserService)
	userHandler := NewUserHandler(serviceContext.UserService)
	feedHandler := NewFeedHandler(serviceContext.FeedService)

	mux.Handle("/healthz", healthHandler)
	mux.HandleFunc("POST /api/v1/auth/register", authHandler.Register)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login)
	mux.Handle(
		"POST /api/v1/auth/logout",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(authHandler.Logout)),
	)
	mux.Handle(
		"GET /api/v1/users/me",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(userHandler.Me)),
	)
	mux.Handle(
		"PUT /api/v1/users/me",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(userHandler.UpdateMe)),
	)
	mux.Handle(
		"GET /api/v1/feed/recommend",
		middleware.OptionalJWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(feedHandler.Recommend)),
	)

	return mux
}
