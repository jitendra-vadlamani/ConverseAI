package app

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"

	"ai-chat/internal/config"
	"ai-chat/internal/database"
	"ai-chat/internal/events"
	"ai-chat/internal/handler"
	"ai-chat/internal/manager"
	"ai-chat/internal/mcp"
	"ai-chat/internal/middleware"
	"ai-chat/internal/ollama"
	"ai-chat/internal/orchestrator"
	"ai-chat/internal/repository"
	"ai-chat/internal/service"
	"ai-chat/internal/storage"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type App struct {
	cfg            *config.Config
	db             *database.Database
	storageService storage.StorageService
	mcpRegistry    mcp.Registry
	router         *http.ServeMux
}

func NewApp(cfg *config.Config) *App {
	return &App{
		cfg:    cfg,
		router: http.NewServeMux(),
	}
}

func (a *App) Initialize(staticContent embed.FS) error {
	// Initialize Database
	db, err := database.NewDatabase(a.cfg)
	if err != nil {
		return err
	}
	a.db = db

	// Initialize Storage
	storageService, err := storage.NewStorageService(a.cfg.MinioEndpoint, a.cfg.MinioUser, a.cfg.MinioPass, a.cfg.MinioBucket, a.cfg.MinioSSL)
	if err != nil {
		return err
	}
	a.storageService = storageService

	// Set up Collections & Indexes (like TTL)
	_, _ = a.db.DB.Collection("events").Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.M{"timestamp": 1},
		Options: options.Index().SetExpireAfterSeconds(30 * 24 * 60 * 60),
	})

	// 1. Core Clients & Repositories
	ollamaClient := ollama.NewClient()
	modelManager := manager.NewModelManager(ollamaClient)

	userRepo := repository.NewUserRepository(a.db.DB)
	systemLLMRepo := repository.NewSystemLLMRepository()
	chatRepo := repository.NewChatRepository(a.db.DB, a.cfg.DBEncryptionKey)
	projectRepo := repository.NewProjectRepository(a.db.DB)
	eventRepo := repository.NewEventRepository(a.db.DB)
	eventBroker := events.NewEventBroker()

	// 2. Services
	authService := service.NewAuthService(userRepo, a.cfg)
	cosService := service.NewCosService(ollamaClient, a.cfg)

	// Initialize MCP Registry
	a.mcpRegistry = mcp.NewRegistry()

	// Load external servers configured in workspace
	if err := a.mcpRegistry.LoadExternalServers(context.Background(), "."); err != nil {
		log.Printf("Warning: Failed to load external MCP servers: %v", err)
	}

	// 3. Orchestration
	executor := orchestrator.NewExecutor(a.mcpRegistry, eventRepo, eventBroker)
	orch := orchestrator.NewOrchestrator(ollamaClient, a.mcpRegistry, executor, eventRepo, eventBroker)

	chatService := service.NewChatService(chatRepo, projectRepo, systemLLMRepo, ollamaClient, modelManager, orch, a.mcpRegistry, a.storageService, eventRepo, eventBroker, a.cfg)

	// 4. Handlers
	authHandler := handler.NewAuthHandler(authService)
	chatHandler := handler.NewChatHandler(chatService, a.storageService, eventBroker)
	orchHandler := handler.NewOrchestratorHandler(orch)
	projectHandler := handler.NewProjectHandler(projectRepo, cosService)

	// 5. Routing
	mw := middleware.NewMiddleware(authService)
	a.registerRoutes(authHandler, chatHandler, orchHandler, projectHandler, mw, staticContent)

	return nil
}

func (a *App) registerRoutes(
	auth *handler.AuthHandler,
	chat *handler.ChatHandler,
	orch *handler.OrchestratorHandler,
	proj *handler.ProjectHandler,
	mw *middleware.Middleware,
	staticContent embed.FS,
) {
	// Domain Routes (Decentralized)
	auth.RegisterRoutes(a.router, mw)
	chat.RegisterRoutes(a.router, mw)
	orch.RegisterRoutes(a.router, mw)
	proj.RegisterRoutes(a.router, mw)

	// Hello probe
	a.router.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Hello from Go Backend!"})
	})

	// Static Web Client Files
	dist, err := fs.Sub(staticContent, "dist")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(dist))

	a.router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Clean(r.URL.Path)
		if _, err := dist.Open(path[1:]); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		indexHTML, _ := fs.ReadFile(dist, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
}

func (a *App) GetRouter() http.Handler {
	return middleware.Logger(a.router)
}

func (a *App) Shutdown(ctx context.Context) error {
	var err error
	if a.mcpRegistry != nil {
		err = a.mcpRegistry.Close()
	}
	if a.db != nil && a.db.Client != nil {
		dbErr := a.db.Client.Disconnect(ctx)
		if err == nil {
			err = dbErr
		}
	}
	return err
}
