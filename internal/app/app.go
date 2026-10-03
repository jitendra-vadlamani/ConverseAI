// Package app wires the services together and builds the HTTP handler.
package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"ai-chat/internal/agent"
	"ai-chat/internal/config"
	"ai-chat/internal/database"
	"ai-chat/internal/events"
	"ai-chat/internal/handler"
	"ai-chat/internal/manager"
	"ai-chat/internal/middleware"
	"ai-chat/internal/ollama"
	"ai-chat/internal/rag"
	"ai-chat/internal/repository"
	"ai-chat/internal/search"
	"ai-chat/internal/service"
	"ai-chat/internal/storage"
	"ai-chat/internal/util"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// App holds the long-lived pieces the server needs to start and stop.
type App struct {
	Handler http.Handler
	Runs    service.RunService

	db     *database.Database
	broker events.Broker
}

// Repos are exposed for maintenance commands (key rotation, exports).
type Repos struct {
	Chats    repository.ChatRepository
	Events   repository.EventRepository
	Runs     repository.RunRepository
	Feedback repository.FeedbackRepository
}

// Overrides lets tests swap external services for fakes.
type Overrides struct {
	Ollama ollama.Client
	Search search.Service
}

func OpenRepos(ctx context.Context, cfg *config.Config) (*database.Database, *Repos, error) {
	keys, err := util.NewKeyRing(cfg.DBEncryptionKey, cfg.DBEncryptionKeysOld...)
	if err != nil {
		return nil, nil, err
	}
	db, err := database.NewDatabase(ctx, cfg.MongoURI, cfg.DBName)
	if err != nil {
		return nil, nil, err
	}
	return db, &Repos{
		Chats:    repository.NewChatRepository(db.DB, keys),
		Events:   repository.NewEventRepository(db.DB, keys),
		Runs:     repository.NewRunRepository(db.DB, keys),
		Feedback: repository.NewFeedbackRepository(db.DB, keys),
	}, nil
}

func New(ctx context.Context, cfg *config.Config, static fs.FS, ov Overrides) (*App, error) {
	db, repos, err := OpenRepos(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := repository.EnsureIndexes(ctx, db.DB); err != nil {
		return nil, fmt.Errorf("create indexes: %w", err)
	}
	if n, err := repos.Events.PurgeLegacyPlaintext(ctx); err != nil {
		slog.Warn("purge legacy plaintext events", "err", err)
	} else if n > 0 {
		slog.Info("removed plaintext payloads from legacy events", "count", n)
	}

	catalog, err := repository.NewSystemLLMRepository()
	if err != nil {
		return nil, err
	}
	userRepo := repository.NewUserRepository(db.DB)

	store, err := storage.NewStorageService(ctx, cfg.MinioEndpoint, cfg.MinioUser, cfg.MinioPass, cfg.MinioBucket, cfg.MinioSSL)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}

	var broker events.Broker = events.NewMemoryBroker()
	if cfg.RedisURL != "" {
		if broker, err = events.NewRedisBroker(ctx, cfg.RedisURL); err != nil {
			return nil, fmt.Errorf("redis: %w", err)
		}
		slog.Info("using redis event broker")
	}

	llm := ov.Ollama
	if llm == nil {
		llm = ollama.NewClient(cfg.OllamaBaseURL)
	}
	webSearch := ov.Search
	if webSearch == nil {
		webSearch = search.NewService()
	}
	ragSvc := rag.NewService(cfg.ChromaURL, cfg.ChromaTenant, cfg.ChromaDatabase, llm, cfg.EmbeddingModel)
	models := manager.NewModelManager(llm, cfg.SingleModelMode)

	ag := agent.New(&agent.Deps{
		Ollama: llm, Models: models, RAG: ragSvc, Search: webSearch, Files: store, Catalog: catalog,
		MaxNumCtx: cfg.MaxNumCtx, OCRModel: modelIfKnown(catalog, cfg.DefaultOCRModel),
		TranslationModel: modelIfKnown(catalog, cfg.DefaultTranslationModel),
	})
	emitter := &service.Emitter{Events: repos.Events, Broker: broker}
	authSvc := service.NewAuthService(userRepo, cfg.JWTSecret, cfg.JWTTTL)
	chatSvc := service.NewChatService(repos.Chats, userRepo, repos.Runs, repos.Feedback, repos.Events, catalog, store, ragSvc, broker)
	runSvc := service.NewRunService(cfg, repos.Chats, repos.Runs, catalog, store, ragSvc, ag, broker, emitter)

	authH := handler.NewAuthHandler(authSvc, chatSvc, cfg.CookieSecure, cfg.JWTTTL)
	chatH := handler.NewChatHandler(chatSvc, runSvc, cfg.MaxUploadBytes)
	mw := middleware.NewMiddleware(authSvc)
	auth := mw.RequireAuth

	byIP := middleware.ByIP(cfg.TrustProxy)
	loginLimit := middleware.NewRateLimiter("login", 10, 5)
	registerLimit := middleware.NewRateLimiter("register", 5, 3)
	completionLimit := middleware.NewRateLimiter("completion", 20, 5)
	apiLimit := middleware.NewRateLimiter("api", 600, 120)
	api := func(h http.HandlerFunc) http.HandlerFunc { return auth(apiLimit.Limit(middleware.ByUser, h)) }

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/register", registerLimit.Limit(byIP, authH.Register))
	mux.HandleFunc("POST /api/auth/login", loginLimit.Limit(byIP, authH.Login))
	mux.HandleFunc("POST /api/auth/logout", authH.Logout)
	mux.HandleFunc("GET /api/auth/me", api(authH.Me))
	mux.HandleFunc("PUT /api/auth/password", auth(loginLimit.Limit(middleware.ByUser, authH.UpdatePassword)))
	mux.HandleFunc("DELETE /api/auth/account", auth(loginLimit.Limit(middleware.ByUser, authH.DeleteAccount)))

	mux.HandleFunc("GET /api/models", api(chatH.ListModels))
	mux.HandleFunc("GET /api/chat/conversations", api(chatH.ListConversations))
	mux.HandleFunc("GET /api/chat/conversations/get", api(chatH.GetConversation))
	mux.HandleFunc("POST /api/chat/conversations/create", api(chatH.CreateConversation))
	mux.HandleFunc("DELETE /api/chat/conversations/delete", api(chatH.DeleteConversation))
	mux.HandleFunc("PATCH /api/chat/conversations/title", api(chatH.UpdateConversationTitle))
	mux.HandleFunc("GET /api/chat/conversations/files", api(chatH.ListConversationFiles))
	mux.HandleFunc("DELETE /api/chat/conversations/files", api(chatH.DeleteConversationFile))
	mux.HandleFunc("GET /api/chat/files/download", api(chatH.DownloadFile))
	mux.HandleFunc("GET /api/chat/conversations/events", api(chatH.GetEvents))
	mux.HandleFunc("GET /api/chat/conversations/events/stream", api(chatH.StreamEvents))
	mux.HandleFunc("POST /api/chat/completions", auth(completionLimit.Limit(middleware.ByUser, chatH.StreamCompletion)))
	mux.HandleFunc("GET /api/chat/runs/stream", api(chatH.StreamRun))
	mux.HandleFunc("POST /api/chat/runs/cancel", api(chatH.CancelRun))
	mux.HandleFunc("POST /api/chat/feedback", api(chatH.SubmitFeedback))

	mux.HandleFunc("GET /healthz", handler.Healthz)
	mux.HandleFunc("GET /readyz", handler.Readyz([]handler.Check{
		{Name: "mongodb", Fn: db.Ping},
		{Name: "minio", Fn: store.Ping},
		{Name: "chroma", Fn: ragSvc.Ping},
		{Name: "broker", Fn: func(ctx context.Context) error { return events.Ping(ctx, broker) }},
		{Name: "ollama", Fn: llm.Ping, Optional: true},
	}))
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	if static != nil {
		mux.Handle("/", spaHandler(static))
	}

	var h http.Handler = mux
	h = middleware.SameOrigin(nil, cfg.TrustProxy)(h)
	h = middleware.SecurityHeaders(h)
	h = middleware.Logger(h)

	return &App{Handler: h, Runs: runSvc, db: db, broker: broker}, nil
}

// modelIfKnown returns name if it's in the catalog, else "" (tool disabled).
func modelIfKnown(catalog repository.SystemLLMRepository, name string) string {
	if catalog.GetMetadata(name) == nil {
		slog.Warn("model not in system_models.json; the tool that needs it is disabled", "model", name)
		return ""
	}
	return name
}

// spaHandler serves the built client, falling back to index.html for
// client-side routes. Hashed assets are cached; index.html never is.
func spaHandler(static fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if f, err := static.Open(p); err == nil {
				f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(static, "index.html")
		if err != nil {
			http.Error(w, "client not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

// Close releases connections after the HTTP server has stopped.
func (a *App) Close(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	a.Runs.Shutdown(runCtx)
	cancel()
	_ = a.broker.Close()
	if err := a.db.Client.Disconnect(ctx); err != nil {
		slog.Warn("mongodb disconnect", "err", err)
	}
}
