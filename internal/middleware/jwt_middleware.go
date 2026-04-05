package middleware

import (
	"context"
	"net/http"

	"ai-chat/internal/service"
)

// ContextKey is an unexported type for context keys to avoid collisions.
type ContextKey string

// UserIDKey is the context key used to store the authenticated user's ID.
const UserIDKey ContextKey = "userID"

type Middleware struct {
	authService service.AuthService
}

func NewMiddleware(authService service.AuthService) *Middleware {
	return &Middleware{
		authService: authService,
	}
}

func (m *Middleware) JWTMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("jwt")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userID, err := m.authService.VerifyToken(cookie.Value)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Add userID to context using typed key
		ctx := context.WithValue(r.Context(), UserIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
