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
	"ai-chat/internal/handler"
	"ai-chat/internal/llm"
	"ai-chat/internal/middleware"
	"ai-chat/internal/repository"
	"ai-chat/internal/service"
)

type App struct {
	cfg    *config.Config
	pgDb   *database.PostgresDatabase
	router *http.ServeMux
}

func NewApp(cfg *config.Config) *App {
	return &App{
		cfg:    cfg,
		router: http.NewServeMux(),
	}
}

func (a *App) Initialize(staticContent embed.FS) error {
	// Initialize Postgres DB
	pgDb, err := database.NewPostgresDatabase(a.cfg)
	if err != nil {
		return err
	}
	a.pgDb = pgDb

	// LLM Provider Abstraction
	var llmProvider llm.Provider
	if a.cfg.LLMBackend == "ollama" {
		llmProvider = llm.NewOllamaProvider(a.cfg.LLMBaseURL, a.cfg.LLMModel)
	} else {
		// Fallback to Ollama
		llmProvider = llm.NewOllamaProvider(a.cfg.LLMBaseURL, a.cfg.LLMModel)
	}

	userRepo := repository.NewUserRepository(a.pgDb.DB)
	topicRepo := repository.NewTopicRepository(a.pgDb.DB)

	// Services
	authService := service.NewAuthService(userRepo, a.cfg)
	topicService := service.NewTopicService(topicRepo, llmProvider, a.cfg)

	// Handlers
	authHandler := handler.NewAuthHandler(authService)
	topicHandler := handler.NewTopicHandler(topicService)

	// Routing
	mw := middleware.NewMiddleware(authService)
	a.registerRoutes(authHandler, topicHandler, mw, staticContent)

	return nil
}

func (a *App) registerRoutes(
	auth *handler.AuthHandler,
	topic *handler.TopicHandler,
	mw *middleware.Middleware,
	staticContent embed.FS,
) {
	// Domain Routes
	auth.RegisterRoutes(a.router, mw)
	topic.RegisterRoutes(a.router, mw)

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
	if a.pgDb != nil && a.pgDb.DB != nil {
		pgErr := a.pgDb.DB.Close()
		if err == nil {
			err = pgErr
		}
	}
	return err
}
