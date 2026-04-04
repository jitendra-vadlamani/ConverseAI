package repository

import (
	"context"

	"ai-chat/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type EventRepository interface {
	StoreEvent(ctx context.Context, event model.ConversationEvent) error
	GetEventsByConversationID(ctx context.Context, conversationID primitive.ObjectID) ([]model.ConversationEvent, error)
	GetEventsByUserID(ctx context.Context, userID primitive.ObjectID) ([]model.ConversationEvent, error)
}

type mongoEventRepository struct {
	collection *mongo.Collection
}

func NewEventRepository(db *mongo.Database) EventRepository {
	return &mongoEventRepository{
		collection: db.Collection("events"),
	}
}

func (r *mongoEventRepository) StoreEvent(ctx context.Context, event model.ConversationEvent) error {
	_, err := r.collection.InsertOne(ctx, event)
	return err
}

func (r *mongoEventRepository) GetEventsByConversationID(ctx context.Context, conversationID primitive.ObjectID) ([]model.ConversationEvent, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"conversation_id": conversationID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var events []model.ConversationEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *mongoEventRepository) GetEventsByUserID(ctx context.Context, userID primitive.ObjectID) ([]model.ConversationEvent, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var events []model.ConversationEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	return events, nil
}
