package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"ai-chat/internal/config"
	_ "github.com/lib/pq"
)

type PostgresDatabase struct {
	DB *sql.DB
}

func NewPostgresDatabase(cfg *config.Config) (*PostgresDatabase, error) {
	db, err := sql.Open("postgres", cfg.PostgresURI)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Ping to verify connection
	err = db.Ping()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping postgres at %s: %w", cfg.PostgresURI, err)
	}

	log.Printf("Connected to PostgreSQL successfully at %s", cfg.PostgresURI)
	return &PostgresDatabase{DB: db}, nil
}
