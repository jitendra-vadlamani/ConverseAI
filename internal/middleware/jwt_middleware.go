package middleware

import (
	"context"
	"net/http"

	"ai-chat/internal/service"
)

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

		// Add userID to context
		ctx := context.WithValue(r.Context(), "userID", userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
