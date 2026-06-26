package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ai-chat/client"
	"ai-chat/internal/app"
	"ai-chat/internal/config"
)

func main() {
	// Initialize Config
	cfg := config.LoadConfig()

	// Instantiate the app container
	application := app.NewApp(cfg)

	// Initialize the application dependencies and router
	if err := application.Initialize(client.StaticContent); err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}

	// Setup Server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: application.GetRouter(),
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server starting on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to listen: %v", err)
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

	// Shutdown HTTP Server
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	// Disconnect MongoDB and clean up container resources
	if err := application.Shutdown(shutdownCtx); err != nil {
		log.Printf("Application shutdown error: %v", err)
	}

	log.Println("Server exited gracefully")
}
