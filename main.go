package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ai-chat/internal/app"
	"ai-chat/internal/config"
	"ai-chat/internal/model"
	"ai-chat/internal/tracing"
)

//go:embed all:client/dist
var staticContent embed.FS

var version = "dev"

const usage = `usage: converseai [command]

commands:
  serve            run the HTTP server (default)
  rotate-keys      re-encrypt stored data with DB_ENCRYPTION_KEY; old keys go in DB_ENCRYPTION_KEYS_OLD
  export-feedback  print thumbs-down answers (with corrections) as JSONL eval candidates
`

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error:\n%v\n", err)
		os.Exit(2)
	}
	setupLogging(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "serve":
		err = serve(ctx, cfg)
	case "rotate-keys":
		err = rotateKeys(ctx, cfg)
	case "export-feedback":
		err = exportFeedback(ctx, cfg)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error(cmd+" failed", "err", err)
		os.Exit(1)
	}
}

func setupLogging(cfg *config.Config) {
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	if cfg.IsDevelopment() {
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	slog.SetDefault(slog.New(h))
}

func serve(ctx context.Context, cfg *config.Config) error {
	shutdownTracing, err := tracing.Setup(ctx, cfg.OTLPEndpoint, version)
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	dist, err := fs.Sub(staticContent, "client/dist")
	if err != nil {
		return err
	}
	a, err := app.New(ctx, cfg, dist, app.Overrides{})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           a.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute, // uploads
		WriteTimeout:      2 * time.Minute, // SSE handlers lift this per request
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())
	go a.Runs.Background(bgCtx)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server starting", "port", cfg.Port, "version", version)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		bgCancel()
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	bgCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Stop runs first so open streams end with their saved state, then
	// drain HTTP.
	a.Runs.Shutdown(shutdownCtx)
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("http shutdown", "err", err)
	}
	a.Close(shutdownCtx)
	_ = shutdownTracing(shutdownCtx)
	slog.Info("server stopped")
	return nil
}

func rotateKeys(ctx context.Context, cfg *config.Config) error {
	db, repos, err := app.OpenRepos(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Client.Disconnect(context.Background())
	for name, rotate := range map[string]func(context.Context) (int, error){
		"conversations": repos.Chats.RotateKeys,
		"events":        repos.Events.RotateKeys,
		"runs":          repos.Runs.RotateKeys,
		"feedback":      repos.Feedback.RotateKeys,
	} {
		n, err := rotate(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		slog.Info("re-encrypted", "collection", name, "documents", n)
	}
	slog.Info("key rotation complete; DB_ENCRYPTION_KEYS_OLD can now be removed")
	return nil
}

// exportFeedback writes each thumbs-down answer as a candidate golden-set
// case. A human reviews them before adding to evals/golden.jsonl.
func exportFeedback(ctx context.Context, cfg *config.Config) error {
	db, repos, err := app.OpenRepos(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Client.Disconnect(context.Background())
	items, err := repos.Feedback.ListNegative(ctx, 500)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	for _, fb := range items {
		conv, err := repos.Chats.GetConversation(ctx, fb.ConversationID)
		if err != nil {
			continue
		}
		var question, answer string
		for i, m := range conv.Messages {
			if m.ID == fb.MessageID {
				answer = m.Content
				for j := i - 1; j >= 0; j-- {
					if conv.Messages[j].Role == model.RoleUser {
						question = conv.Messages[j].Content
						break
					}
				}
			}
		}
		if question == "" {
			continue
		}
		_ = enc.Encode(map[string]any{
			"id": "feedback-" + fb.MessageID.Hex(), "category": "feedback", "question": question,
			"bad_answer": answer, "correction": fb.Correction, "run_id": fb.RunID.Hex(),
		})
	}
	return nil
}
