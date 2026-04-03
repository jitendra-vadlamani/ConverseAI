package database

import (
	"context"
	"fmt"
	"log"

	"ai-chat/internal/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ctx = context.Background()
)

type Database struct {
	Client *mongo.Client
	DB     *mongo.Database
}

func NewDatabase(cfg *config.Config) (*Database, error) {
	clientOptions := options.Client().ApplyURI(cfg.MongoURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
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

	log.Println("Connected to MongoDB successfully.")
	return &Database{Client: client, DB: db}, nil
}
