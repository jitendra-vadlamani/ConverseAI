package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/util"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const undecryptable = "[this content was encrypted with a key that is no longer configured]"

type ChatRepository interface {
	CreateConversation(ctx context.Context, userID primitive.ObjectID, title string) (*model.Conversation, error)
	// GetConversation loads a conversation by id only. Handlers must use
	// GetOwnedConversation; this is for background jobs that already know
	// the owner.
	GetConversation(ctx context.Context, id primitive.ObjectID) (*model.Conversation, error)
	GetOwnedConversation(ctx context.Context, id, userID primitive.ObjectID) (*model.Conversation, error)
	ListConversations(ctx context.Context, userID primitive.ObjectID) ([]model.ConversationSummary, error)
	AddMessage(ctx context.Context, conversationID primitive.ObjectID, msg model.Message) (primitive.ObjectID, error)
	SetMessageTokenCount(ctx context.Context, conversationID, messageID primitive.ObjectID, tokens int) error
	SetTotalTokens(ctx context.Context, conversationID primitive.ObjectID, tokens int) error
	// SetSummary stores the summary and marks every message created at or
	// before cutoff as summarized.
	SetSummary(ctx context.Context, conversationID primitive.ObjectID, summary string, tokens int, cutoff time.Time) error
	UpdateTitle(ctx context.Context, id, userID primitive.ObjectID, title string) error
	DeleteConversation(ctx context.Context, id primitive.ObjectID) error
	DeleteByUser(ctx context.Context, userID primitive.ObjectID) error
	RemoveFileFromConversation(ctx context.Context, conversationID primitive.ObjectID, fileID string) error
	CountFileReferences(ctx context.Context, userID primitive.ObjectID, fileID string) (int64, error)
	// ClaimActiveRun sets the conversation's active run if none is set.
	// It returns ErrConflict if another run is already active.
	ClaimActiveRun(ctx context.Context, conversationID, userID, runID primitive.ObjectID) error
	ClearActiveRun(ctx context.Context, conversationID, runID primitive.ObjectID) error
	// RotateKeys re-encrypts every field not encrypted with the primary key.
	RotateKeys(ctx context.Context) (int, error)
}

type MongoChatRepository struct {
	collection *mongo.Collection
	keys       *util.KeyRing
}

func NewChatRepository(db *mongo.Database, keys *util.KeyRing) ChatRepository {
	return &MongoChatRepository{collection: db.Collection("conversations"), keys: keys}
}

func (r *MongoChatRepository) encrypt(s string) (string, error) {
	enc, err := r.keys.Encrypt(s)
	if err != nil {
		return "", fmt.Errorf("encrypt field: %w", err)
	}
	return enc, nil
}

func (r *MongoChatRepository) decrypt(s string) string {
	pt, err := r.keys.Decrypt(s)
	if err != nil {
		slog.Error("decrypt conversation field", "err", err)
		return undecryptable
	}
	return pt
}

func (r *MongoChatRepository) decryptConversation(c *model.Conversation) {
	c.Title = r.decrypt(c.Title)
	c.Summary = r.decrypt(c.Summary)
	for i := range c.Messages {
		c.Messages[i].Content = r.decrypt(c.Messages[i].Content)
		c.Messages[i].Reasoning = r.decrypt(c.Messages[i].Reasoning)
	}
}

func (r *MongoChatRepository) CreateConversation(ctx context.Context, userID primitive.ObjectID, title string) (*model.Conversation, error) {
	encTitle, err := r.encrypt(title)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	conv := &model.Conversation{
		ID: primitive.NewObjectID(), UserID: userID, Title: encTitle,
		Messages: []model.Message{}, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := r.collection.InsertOne(ctx, conv); err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	conv.Title = title
	return conv, nil
}

func (r *MongoChatRepository) GetConversation(ctx context.Context, id primitive.ObjectID) (*model.Conversation, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

func (r *MongoChatRepository) GetOwnedConversation(ctx context.Context, id, userID primitive.ObjectID) (*model.Conversation, error) {
	return r.findOne(ctx, bson.M{"_id": id, "user_id": userID})
}

func (r *MongoChatRepository) findOne(ctx context.Context, filter bson.M) (*model.Conversation, error) {
	var conv model.Conversation
	if err := r.collection.FindOne(ctx, filter).Decode(&conv); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	r.decryptConversation(&conv)
	return &conv, nil
}

func (r *MongoChatRepository) ListConversations(ctx context.Context, userID primitive.ObjectID) ([]model.ConversationSummary, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}}).
		SetProjection(bson.M{"_id": 1, "title": 1, "created_at": 1, "updated_at": 1})
	cursor, err := r.collection.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	out := []model.ConversationSummary{}
	if err := cursor.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode conversations: %w", err)
	}
	for i := range out {
		out[i].Title = r.decrypt(out[i].Title)
	}
	return out, nil
}

func (r *MongoChatRepository) AddMessage(ctx context.Context, conversationID primitive.ObjectID, msg model.Message) (primitive.ObjectID, error) {
	if msg.ID.IsZero() {
		msg.ID = primitive.NewObjectID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	var err error
	if msg.Content, err = r.encrypt(msg.Content); err != nil {
		return primitive.NilObjectID, err
	}
	if msg.Reasoning, err = r.encrypt(msg.Reasoning); err != nil {
		return primitive.NilObjectID, err
	}
	res, err := r.collection.UpdateOne(ctx, bson.M{"_id": conversationID}, bson.M{
		"$push": bson.M{"messages": msg},
		"$set":  bson.M{"updated_at": time.Now()},
	})
	if err != nil {
		return primitive.NilObjectID, fmt.Errorf("add message: %w", err)
	}
	if res.MatchedCount == 0 {
		return primitive.NilObjectID, ErrNotFound
	}
	return msg.ID, nil
}

func (r *MongoChatRepository) SetMessageTokenCount(ctx context.Context, conversationID, messageID primitive.ObjectID, tokens int) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": conversationID, "messages.id": messageID},
		bson.M{"$set": bson.M{"messages.$.token_count": tokens}})
	return err
}

func (r *MongoChatRepository) SetTotalTokens(ctx context.Context, id primitive.ObjectID, tokens int) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"total_tokens": tokens}})
	return err
}

func (r *MongoChatRepository) SetSummary(ctx context.Context, id primitive.ObjectID, summary string, tokens int, cutoff time.Time) error {
	enc, err := r.encrypt(summary)
	if err != nil {
		return err
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": id},
		bson.M{"$set": bson.M{
			"summary":                       enc,
			"summary_token_count":           tokens,
			"messages.$[old].is_summarized": true,
			"updated_at":                    time.Now(),
		}},
		options.Update().SetArrayFilters(options.ArrayFilters{
			Filters: []any{bson.M{"old.created_at": bson.M{"$lte": cutoff}}},
		}))
	if err != nil {
		return fmt.Errorf("set summary: %w", err)
	}
	return nil
}

func (r *MongoChatRepository) UpdateTitle(ctx context.Context, id, userID primitive.ObjectID, title string) error {
	enc, err := r.encrypt(title)
	if err != nil {
		return err
	}
	res, err := r.collection.UpdateOne(ctx, bson.M{"_id": id, "user_id": userID},
		bson.M{"$set": bson.M{"title": enc, "updated_at": time.Now()}})
	if err != nil {
		return fmt.Errorf("update title: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MongoChatRepository) DeleteConversation(ctx context.Context, id primitive.ObjectID) error {
	if _, err := r.collection.DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	return nil
}

func (r *MongoChatRepository) DeleteByUser(ctx context.Context, userID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}

func (r *MongoChatRepository) RemoveFileFromConversation(ctx context.Context, id primitive.ObjectID, fileID string) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$pull": bson.M{"messages.$[].attachments": fileID},
		"$set":  bson.M{"updated_at": time.Now()},
	})
	if err != nil {
		return fmt.Errorf("remove file from conversation: %w", err)
	}
	return nil
}

func (r *MongoChatRepository) CountFileReferences(ctx context.Context, userID primitive.ObjectID, fileID string) (int64, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{"user_id": userID, "messages.attachments": fileID})
	if err != nil {
		return 0, fmt.Errorf("count file references: %w", err)
	}
	return count, nil
}

func (r *MongoChatRepository) ClaimActiveRun(ctx context.Context, conversationID, userID, runID primitive.ObjectID) error {
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": conversationID, "user_id": userID, "active_run_id": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"active_run_id": runID, "updated_at": time.Now()}})
	if err != nil {
		return fmt.Errorf("claim active run: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrConflict
	}
	return nil
}

func (r *MongoChatRepository) ClearActiveRun(ctx context.Context, conversationID, runID primitive.ObjectID) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": conversationID, "active_run_id": runID},
		bson.M{"$unset": bson.M{"active_run_id": ""}})
	return err
}

func (r *MongoChatRepository) RotateKeys(ctx context.Context) (int, error) {
	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	updated := 0
	for cursor.Next(ctx) {
		var conv model.Conversation
		if err := cursor.Decode(&conv); err != nil {
			return updated, err
		}
		set := bson.M{}
		rotate := func(field, value string) error {
			if !r.keys.NeedsRotation(value) {
				return nil
			}
			pt, err := r.keys.Decrypt(value)
			if err != nil {
				return fmt.Errorf("conversation %s %s: %w", conv.ID.Hex(), field, err)
			}
			enc, err := r.encrypt(pt)
			if err != nil {
				return err
			}
			set[field] = enc
			return nil
		}
		if err := rotate("title", conv.Title); err != nil {
			return updated, err
		}
		if err := rotate("summary", conv.Summary); err != nil {
			return updated, err
		}
		for i, m := range conv.Messages {
			if err := rotate(fmt.Sprintf("messages.%d.content", i), m.Content); err != nil {
				return updated, err
			}
			if err := rotate(fmt.Sprintf("messages.%d.reasoning", i), m.Reasoning); err != nil {
				return updated, err
			}
		}
		if len(set) == 0 {
			continue
		}
		// Guard on message count so a message appended concurrently can't be
		// overwritten by a stale positional index.
		if _, err := r.collection.UpdateOne(ctx,
			bson.M{"_id": conv.ID, "messages": bson.M{"$size": len(conv.Messages)}},
			bson.M{"$set": set}); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, cursor.Err()
}
