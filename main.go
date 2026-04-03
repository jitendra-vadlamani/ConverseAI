package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"

	"ai-chat/internal/config"
	"ai-chat/internal/database"
	"ai-chat/internal/handler"
	"ai-chat/internal/middleware"
	"ai-chat/internal/repository"
	"ai-chat/internal/service"
)

//go:embed all:client/dist
var staticContent embed.FS

func main() {
	// Initialize Config
	cfg := config.LoadConfig()

	// Initialize Database
	db, err := database.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize Repository
	userRepo := repository.NewUserRepository(db.DB)
	llmRepo := repository.NewLLMRepository(db.DB)
	chatRepo := repository.NewChatRepository(db.DB)

	// Initialize Service
	authService := service.NewAuthService(userRepo, cfg)
	llmService := service.NewLLMService(llmRepo)
	chatService := service.NewChatService(chatRepo, llmRepo)

	// Initialize Handler
	authHandler := handler.NewAuthHandler(authService)
	llmHandler := handler.NewLLMHandler(llmService)
	chatHandler := handler.NewChatHandler(chatService)

	// Initialize Middleware
	mw := middleware.NewMiddleware(authService)

	mux := http.NewServeMux()

	// Auth routes
	mux.HandleFunc("/api/auth/register", authHandler.Register)
	mux.HandleFunc("/api/auth/login", authHandler.Login)
	mux.HandleFunc("/api/auth/me", mw.JWTMiddleware(authHandler.Me))
	mux.HandleFunc("/api/auth/logout", authHandler.Logout)
	mux.HandleFunc("/api/auth/password", mw.JWTMiddleware(authHandler.UpdatePassword))

	// LLM routes
	mux.HandleFunc("/api/llms", mw.JWTMiddleware(llmHandler.List))
	mux.HandleFunc("/api/llms/add", mw.JWTMiddleware(llmHandler.Add))
	mux.HandleFunc("/api/llms/delete", mw.JWTMiddleware(llmHandler.Delete))

	// Chat routes
	mux.HandleFunc("/api/chat/conversations", mw.JWTMiddleware(chatHandler.ListConversations))
	mux.HandleFunc("/api/chat/conversations/get", mw.JWTMiddleware(chatHandler.GetConversation))
	mux.HandleFunc("/api/chat/conversations/create", mw.JWTMiddleware(chatHandler.CreateConversation))
	mux.HandleFunc("/api/chat/conversations/delete", mw.JWTMiddleware(chatHandler.DeleteConversation))
	mux.HandleFunc("/api/chat/completions", mw.JWTMiddleware(chatHandler.StreamCompletion))

	// API routes
	mux.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Hello from Go Backend!",
		})
	})

	// Static file server with SPA routing
	dist, err := fs.Sub(staticContent, "client/dist")
	if err != nil {
		log.Fatal(err)
	}

	fileServer := http.FileServer(http.FS(dist))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If the request path is an actual file in the dist folder, serve it
		path := filepath.Clean(r.URL.Path)
		if _, err := dist.Open(path[1:]); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Otherwise, serve index.html for SPA routing
		indexHTML, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	log.Printf("Server starting on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, middleware.Logger(mux)); err != nil {
		log.Fatal(err)
	}
}
