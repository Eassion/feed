package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	usersvc "feed/internal/service/user"
	"feed/pkg/jwtutil"
)

type contextKey string

const (
	claimsContextKey contextKey = "jwtClaims"
	tokenContextKey  contextKey = "jwtToken"
)

func JWTAuth(authenticator *usersvc.Service, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, token, err := authenticate(r, authenticator)
			if err == errMissingToken {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			if err != nil {
				logger.Warn("jwt validation failed", "error", err)
				http.Error(w, "invalid bearer token", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			ctx = context.WithValue(ctx, tokenContextKey, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func OptionalJWTAuth(authenticator *usersvc.Service, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, token, err := authenticate(r, authenticator)
			if err == errMissingToken {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				logger.Warn("jwt validation failed", "error", err)
				http.Error(w, "invalid bearer token", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			ctx = context.WithValue(ctx, tokenContextKey, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ClaimsFromContext(ctx context.Context) (*jwtutil.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*jwtutil.Claims)
	return claims, ok
}

func UserIDFromContext(ctx context.Context) (int64, bool) {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return 0, false
	}

	return claims.UserID, true
}

func TokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(tokenContextKey).(string)
	return token, ok
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

var errMissingToken = http.ErrNoCookie

func authenticate(r *http.Request, authenticator *usersvc.Service) (*jwtutil.Claims, string, error) {
	if authenticator == nil {
		return nil, "", jwtutil.ErrManagerNotConfigured
	}

	rawToken := bearerToken(r.Header.Get("Authorization"))
	if rawToken == "" {
		return nil, "", errMissingToken
	}

	claims, err := authenticator.Authenticate(r.Context(), rawToken)
	if err != nil {
		if errors.Is(err, usersvc.ErrInvalidSession) {
			return nil, "", err
		}
		return nil, "", err
	}

	return claims, rawToken, nil
}
