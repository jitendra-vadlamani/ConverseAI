package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ai-chat/internal/config"
	"ai-chat/internal/database"
	"ai-chat/internal/handler"
	"ai-chat/internal/middleware"
	"ai-chat/internal/ollama"
	"ai-chat/internal/orchestrator"
	"ai-chat/internal/manager"
	"ai-chat/internal/repository"
	"ai-chat/internal/service"
	"ai-chat/internal/storage"
	"ai-chat/internal/events"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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

	// 1. Initialize Base Clients & Repositories
	ollamaClient := ollama.NewClient()
	modelManager := manager.NewModelManager(ollamaClient)

	// Initialize Storage
	storageService, err := storage.NewStorageService(cfg.MinioEndpoint, cfg.MinioUser, cfg.MinioPass, cfg.MinioBucket, cfg.MinioSSL)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	userRepo := repository.NewUserRepository(db.DB)
	systemLLMRepo := repository.NewSystemLLMRepository()
	chatRepo := repository.NewChatRepository(db.DB, cfg.DBEncryptionKey)
	eventRepo := repository.NewEventRepository(db.DB)
	eventBroker := events.NewEventBroker()

	// One-time Setup: TTL Index for Events (30 days)
	_, _ = db.DB.Collection("events").Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.M{"timestamp": 1},
		Options: options.Index().SetExpireAfterSeconds(30 * 24 * 60 * 60),
	})

	// 2. Initialize Orchestrator
	planner := orchestrator.NewPlanner(ollamaClient, modelManager, systemLLMRepo, cfg)
	validator := orchestrator.NewValidator(systemLLMRepo)
	executor := orchestrator.NewExecutor(ollamaClient, modelManager, storageService, eventRepo, eventBroker, systemLLMRepo)
	orch := orchestrator.NewOrchestrator(planner, validator, executor)

	// 3. Initialize Services
	authService := service.NewAuthService(userRepo, cfg)
	ragService := service.NewRagService(cfg, ollamaClient)
	chatService := service.NewChatService(chatRepo, systemLLMRepo, ollamaClient, modelManager, orch, planner, storageService, eventRepo, eventBroker, ragService, cfg)

	// 4. Initialize Handlers
	authHandler := handler.NewAuthHandler(authService)
	chatHandler := handler.NewChatHandler(chatService, storageService, eventBroker)
	orchHandler := handler.NewOrchestratorHandler(orch)

	// 5. Setup Routes
	mw := middleware.NewMiddleware(authService)
	mux := http.NewServeMux()

	// API Routes
	mux.HandleFunc("/api/auth/register", authHandler.Register)
	mux.HandleFunc("/api/auth/login", authHandler.Login)
	mux.HandleFunc("/api/auth/me", mw.JWTMiddleware(authHandler.Me))
	mux.HandleFunc("/api/auth/logout", authHandler.Logout)
	mux.HandleFunc("/api/auth/password", mw.JWTMiddleware(authHandler.UpdatePassword))

	mux.HandleFunc("/api/chat/conversations", mw.JWTMiddleware(chatHandler.ListConversations))
	mux.HandleFunc("/api/models", mw.JWTMiddleware(chatHandler.ListModels))
	mux.HandleFunc("/api/chat/conversations/get", mw.JWTMiddleware(chatHandler.GetConversation))
	mux.HandleFunc("/api/chat/conversations/create", mw.JWTMiddleware(chatHandler.CreateConversation))
	mux.HandleFunc("/api/chat/conversations/delete", mw.JWTMiddleware(chatHandler.DeleteConversation))
	mux.HandleFunc("/api/chat/conversations/title", mw.JWTMiddleware(chatHandler.UpdateConversationTitle))
	mux.HandleFunc("/api/chat/conversations/files", mw.JWTMiddleware(chatHandler.DeleteConversationFile)) // DELETE method handled in handler
	mux.HandleFunc("/api/chat/files/presign", mw.JWTMiddleware(chatHandler.GetFilePresignedURL))
	mux.HandleFunc("/api/chat/conversations/events", mw.JWTMiddleware(chatHandler.GetEvents))
	mux.HandleFunc("/api/chat/conversations/events/stream", mw.JWTMiddleware(chatHandler.StreamEvents))
	mux.HandleFunc("/api/chat/completions", mw.JWTMiddleware(chatHandler.StreamCompletion))

	mux.HandleFunc("/api/orchestrate", mw.JWTMiddleware(orchHandler.Orchestrate))

	mux.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Hello from Go Backend!"})
	})

	// Static File Serving
	dist, err := fs.Sub(staticContent, "client/dist")
	if err != nil { log.Fatal(err) }
	fileServer := http.FileServer(http.FS(dist))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Clean(r.URL.Path)
		if _, err := dist.Open(path[1:]); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		indexHTML, _ := fs.ReadFile(dist, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	// Graceful Shutdown
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: middleware.Logger(mux),
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server starting on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Give active connections 10 seconds to drain
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	// Disconnect MongoDB
	if err := db.Client.Disconnect(shutdownCtx); err != nil {
		log.Printf("MongoDB disconnect error: %v", err)
	}

	log.Println("Server exited gracefully")
}
