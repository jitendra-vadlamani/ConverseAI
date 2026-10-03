package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type Database struct {
	Client *mongo.Client
	DB     *mongo.Database
}

func NewDatabase(ctx context.Context, uri, dbName string) (*Database, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetTimeout(30*time.Second))
	if err != nil {
		return nil, fmt.Errorf("connect to MongoDB: %w", err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		// Don't echo the URI: it contains the password.
		return nil, fmt.Errorf("ping MongoDB: %w", err)
	}
	slog.Info("connected to MongoDB", "database", dbName)
	return &Database{Client: client, DB: client.Database(dbName)}, nil
}

func (d *Database) Ping(ctx context.Context) error {
	return d.Client.Ping(ctx, readpref.Primary())
}
