package repository

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrNotFound is returned instead of (nil, nil) so callers can't dereference
// a missing document by accident.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a unique constraint or precondition fails.
var ErrConflict = errors.New("conflict")

// EnsureIndexes creates every index the repositories rely on. It is
// idempotent and runs at startup.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	specs := map[string][]mongo.IndexModel{
		"users": {
			{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
		"conversations": {
			{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "updated_at", Value: -1}}},
			{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "messages.attachments", Value: 1}}},
		},
		"events": {
			{Keys: bson.D{{Key: "conversation_id", Value: 1}, {Key: "timestamp", Value: 1}}},
			{Keys: bson.D{{Key: "user_id", Value: 1}}},
			{Keys: bson.D{{Key: "timestamp", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(30 * 24 * 60 * 60)},
		},
		"runs": {
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "heartbeat_at", Value: 1}}},
			{Keys: bson.D{{Key: "conversation_id", Value: 1}}},
			{Keys: bson.D{{Key: "user_id", Value: 1}}},
			{Keys: bson.D{{Key: "finished_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(30 * 24 * 60 * 60)},
		},
		"feedback": {
			{Keys: bson.D{{Key: "user_id", Value: 1}}},
			{Keys: bson.D{{Key: "message_id", Value: 1}, {Key: "user_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
	}
	for coll, models := range specs {
		if _, err := db.Collection(coll).Indexes().CreateMany(ctx, models); err != nil {
			return err
		}
	}
	return nil
}
