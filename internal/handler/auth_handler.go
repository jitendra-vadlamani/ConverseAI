package handler

import (
	"net/http"
	"time"

	"ai-chat/internal/middleware"
	"ai-chat/internal/service"
)

type AuthHandler struct {
	auth         service.AuthService
	chats        service.ChatService
	cookieSecure bool
	ttl          time.Duration
}

func NewAuthHandler(auth service.AuthService, chats service.ChatService, cookieSecure bool, ttl time.Duration) *AuthHandler {
	return &AuthHandler{auth: auth, chats: chats, cookieSecure: cookieSecure, ttl: ttl}
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) setSession(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	user, err := h.auth.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	user, token, err := h.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	h.setSession(w, token, h.ttl)
	writeJSON(w, http.StatusOK, map[string]any{"message": "Login successful", "user": user})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.setSession(w, "", -time.Second)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user, err := h.auth.GetUserByID(r.Context(), middleware.UserID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h *AuthHandler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.auth.UpdatePassword(r.Context(), middleware.UserID(r), req.OldPassword, req.NewPassword); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Password updated successfully"})
}

// DeleteAccount permanently removes the user and all of their data. The
// current password is required so a stolen session alone can't do it.
func (h *AuthHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	userID := middleware.UserID(r)
	if err := h.auth.CheckPassword(r.Context(), userID, req.Password); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.chats.DeleteAccount(r.Context(), userID); err != nil {
		writeError(w, r, err)
		return
	}
	h.setSession(w, "", -time.Second)
	w.WriteHeader(http.StatusNoContent)
}
