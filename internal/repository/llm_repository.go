package repository

import (
	"context"
	"fmt"
	"time"

	"ai-chat/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type LLMRepository interface {
	Create(ctx context.Context, config *model.LLMConfig) (*model.LLMConfig, error)
	GetByUserID(ctx context.Context, userID primitive.ObjectID) ([]*model.LLMConfig, error)
	GetByID(ctx context.Context, id primitive.ObjectID) (*model.LLMConfig, error)
	Delete(ctx context.Context, id primitive.ObjectID) error
}

type MongoLLMRepository struct {
	collection *mongo.Collection
}

func NewLLMRepository(db *mongo.Database) LLMRepository {
	return &MongoLLMRepository{
		collection: db.Collection("llms"),
	}
}

func (r *MongoLLMRepository) Create(ctx context.Context, config *model.LLMConfig) (*model.LLMConfig, error) {
	newID := primitive.NewObjectID()
	config.ID = &newID
	config.CreatedAt = time.Now()
	config.UpdatedAt = time.Now()

	_, err := r.collection.InsertOne(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to insert llm config: %w", err)
	}

	return config, nil
}

func (r *MongoLLMRepository) GetByUserID(ctx context.Context, userID primitive.ObjectID) ([]*model.LLMConfig, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("failed to list llm configs: %w", err)
	}
	defer cursor.Close(ctx)

	var configs []*model.LLMConfig
	if err := cursor.All(ctx, &configs); err != nil {
		return nil, fmt.Errorf("failed to decode llm configs: %w", err)
	}

	return configs, nil
}

func (r *MongoLLMRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*model.LLMConfig, error) {
	var config model.LLMConfig
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&config)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find llm config by ID: %w", err)
	}
	return &config, nil
}

func (r *MongoLLMRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete llm config: %w", err)
	}
	return nil
}
