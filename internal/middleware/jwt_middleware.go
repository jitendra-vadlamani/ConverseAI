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

// CookieName holds the session JWT.
const CookieName = "jwt"

// UserID returns the authenticated user's id, or "" outside RequireAuth.
func UserID(r *http.Request) string {
	id, _ := r.Context().Value(UserIDKey).(string)
	return id
}

type Middleware struct {
	authService service.AuthService
}

func NewMiddleware(authService service.AuthService) *Middleware {
	return &Middleware{authService: authService}
}

// RequireAuth rejects requests without a valid session cookie.
func (m *Middleware) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieName)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID, err := m.authService.VerifyToken(cookie.Value)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), UserIDKey, userID)
		if lw, ok := w.(*responseWriter); ok {
			lw.userID = userID
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
