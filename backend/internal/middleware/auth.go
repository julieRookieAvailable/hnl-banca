package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/config"
	"github.com/julieRookieAvailable/hnl-banca/backend/internal/respond"
)

type ctxKey string

const userIDKey ctxKey = "userID"
const requestIDKey ctxKey = "requestID"

func UserID(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey).(string); ok {
		return v
	}
	return ""
}

func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// RequireAuth valida el Bearer token JWT y lo desenmascara en los logs.
func RequireAuth(cfg *config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			respond.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "no autorizado")
			return
		}
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims := &jwt.RegisteredClaims{}
		parsed, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			return []byte(cfg.JWTSecret), nil
		})
		if err != nil || !parsed.Valid || claims.Subject == "" {
			respond.Error(w, http.StatusUnauthorized, "INVALID_TOKEN", "token inválido")
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, claims.Subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
