package repository

import (
	"context"
	"fmt"
	"time"

	"ai-chat/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ChatRepository interface {
	CreateConversation(ctx context.Context, conversation *model.Conversation) (*model.Conversation, error)
	GetConversationByID(ctx context.Context, id primitive.ObjectID) (*model.Conversation, error)
	GetConversationsByUserID(ctx context.Context, userID primitive.ObjectID) ([]*model.Conversation, error)
	AddMessage(ctx context.Context, conversationID primitive.ObjectID, message model.Message) error
	UpdateTotalTokens(ctx context.Context, conversationID primitive.ObjectID, tokens int) error
	UpdateSummary(ctx context.Context, conversationID primitive.ObjectID, summary string, tokens int) error
	MarkMessagesAsSummarized(ctx context.Context, conversationID primitive.ObjectID) error
	DeleteConversation(ctx context.Context, id primitive.ObjectID) error
}

type MongoChatRepository struct {
	collection *mongo.Collection
}

func NewChatRepository(db *mongo.Database) ChatRepository {
	return &MongoChatRepository{
		collection: db.Collection("conversations"),
	}
}

func (r *MongoChatRepository) CreateConversation(ctx context.Context, conv *model.Conversation) (*model.Conversation, error) {
	conv.ID = primitive.NewObjectID()
	conv.CreatedAt = time.Now()
	conv.UpdatedAt = time.Now()
	if conv.Messages == nil {
		conv.Messages = []model.Message{}
	}

	_, err := r.collection.InsertOne(ctx, conv)
	if err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}
	return conv, nil
}

func (r *MongoChatRepository) GetConversationByID(ctx context.Context, id primitive.ObjectID) (*model.Conversation, error) {
	var conv model.Conversation
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&conv)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}
	return &conv, nil
}

func (r *MongoChatRepository) GetConversationsByUserID(ctx context.Context, userID primitive.ObjectID) ([]*model.Conversation, error) {
	opts := options.Find().SetSort(bson.M{"updated_at": -1})
	cursor, err := r.collection.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	defer cursor.Close(ctx)

	conversations := []*model.Conversation{}
	if err := cursor.All(ctx, &conversations); err != nil {
		return nil, fmt.Errorf("failed to decode conversations: %w", err)
	}
	return conversations, nil
}

func (r *MongoChatRepository) AddMessage(ctx context.Context, conversationID primitive.ObjectID, msg model.Message) error {
	msg.CreatedAt = time.Now()
	update := bson.M{
		"$push": bson.M{"messages": msg},
		"$set":  bson.M{"updated_at": time.Now()},
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": conversationID}, update)
	if err != nil {
		return fmt.Errorf("failed to add message: %w", err)
	}
	return nil
}

func (r *MongoChatRepository) UpdateTotalTokens(ctx context.Context, id primitive.ObjectID, tokens int) error {
	update := bson.M{
		"$inc": bson.M{"total_tokens": tokens},
		"$set": bson.M{"updated_at": time.Now()},
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to update total tokens: %w", err)
	}
	return nil
}

func (r *MongoChatRepository) UpdateSummary(ctx context.Context, id primitive.ObjectID, summary string, tokens int) error {
	update := bson.M{
		"$set": bson.M{
			"summary":             summary,
			"summary_token_count": tokens,
			"updated_at":          time.Now(),
		},
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to update summary: %w", err)
	}
	return nil
}

func (r *MongoChatRepository) MarkMessagesAsSummarized(ctx context.Context, id primitive.ObjectID) error {
	update := bson.M{
		"$set": bson.M{
			"messages.$[].is_summarized": true,
			"updated_at":                 time.Now(),
		},
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to mark messages as summarized: %w", err)
	}
	return nil
}

func (r *MongoChatRepository) DeleteConversation(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}
	return nil
}
