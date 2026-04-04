package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"ai-chat/internal/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Database struct {
	Client *mongo.Client
	DB     *mongo.Database
}

func NewDatabase(cfg *config.Config) (*Database, error) {
	// Use context with timeout for connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI(cfg.MongoURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Use the same context with timeout for ping
	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB at %s: %w", cfg.MongoURI, err)
	}

	db := client.Database(cfg.DBName)

	// Create unique index on email
	userCollection := db.Collection("users")
	_, err = userCollection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.M{"email": 1},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create index on email: %w", err)
	}

	log.Printf("Connected to MongoDB successfully at %s", cfg.MongoURI)
	return &Database{Client: client, DB: db}, nil
}
