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
	userHandler := NewUserHandler(serviceContext.UserService, serviceContext.UploadService)
	contentHandler := NewContentHandler(serviceContext.ContentService, serviceContext.FeedService, serviceContext.UploadService)
	interactionHandler := NewInteractionHandler(serviceContext.InteractionService, serviceContext.CommentService)
	feedHandler := NewFeedHandler(serviceContext.FeedService)
	countHandler := NewCountHandler(serviceContext.CountService)
	uploadHandler := NewUploadHandler(serviceContext.UploadService)

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
		"POST /api/v1/users/avatar/upload",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(userHandler.UploadAvatar)),
	)
	mux.Handle(
		"GET /api/v1/users/{userID}/profile",
		middleware.OptionalJWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(userHandler.Profile)),
	)
	mux.Handle(
		"GET /api/v1/feed/recommend",
		middleware.OptionalJWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(feedHandler.Recommend)),
	)
	mux.Handle(
		"GET /api/v1/feed/following",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(feedHandler.Following)),
	)
	mux.Handle(
		"GET /api/v1/users/{userID}/favorites",
		middleware.OptionalJWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(feedHandler.UserFavorites)),
	)
	mux.Handle(
		"POST /api/v1/contents/articles",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(contentHandler.PublishArticle)),
	)
	mux.Handle(
		"POST /api/v1/contents/videos",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(contentHandler.PublishVideo)),
	)
	mux.Handle(
		"POST /api/v1/contents/upload-credentials",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(contentHandler.UploadCredentials)),
	)
	mux.Handle(
		"GET /api/v1/contents/{id}",
		middleware.OptionalJWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(contentHandler.Detail)),
	)
	mux.HandleFunc("GET /api/v1/contents/{id}/comments", interactionHandler.ListContentComments)
	mux.Handle(
		"DELETE /api/v1/contents/{id}",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(contentHandler.Delete)),
	)
	mux.Handle(
		"GET /api/v1/users/{userID}/contents",
		middleware.OptionalJWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(contentHandler.ListByUser)),
	)
	mux.Handle(
		"POST /api/v1/interactions/likes",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(interactionHandler.Like)),
	)
	mux.Handle(
		"DELETE /api/v1/interactions/likes/{contentID}",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(interactionHandler.Unlike)),
	)
	mux.Handle(
		"POST /api/v1/interactions/favorites",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(interactionHandler.Favorite)),
	)
	mux.Handle(
		"DELETE /api/v1/interactions/favorites/{contentID}",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(interactionHandler.Unfavorite)),
	)
	mux.Handle(
		"POST /api/v1/comments",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(interactionHandler.Comment)),
	)
	mux.Handle(
		"DELETE /api/v1/comments/{commentID}",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(interactionHandler.DeleteComment)),
	)
	mux.HandleFunc("GET /api/v1/comments/{commentID}/replies", interactionHandler.ListReplies)
	mux.Handle(
		"POST /api/v1/follows",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(interactionHandler.Follow)),
	)
	mux.Handle(
		"DELETE /api/v1/follows/{followeeID}",
		middleware.JWTAuth(serviceContext.UserService, logger)(http.HandlerFunc(interactionHandler.Unfollow)),
	)
	mux.HandleFunc("GET /api/v1/counts/contents/{contentID}", countHandler.Content)
	mux.HandleFunc("GET /api/v1/counts/users/{userID}", countHandler.User)
	mux.HandleFunc("GET /api/v1/counts/contents", countHandler.BatchContent)
	mux.HandleFunc("POST /api/v1/uploads/objects", uploadHandler.UploadObject)
	mux.HandleFunc("GET /api/v1/assets/{objectKey...}", uploadHandler.ServeObject)

	return mux
}
