package repository

import (
	"context"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/util"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type FeedbackRepository interface {
	// Upsert records one rating per user per message; rating again replaces it.
	Upsert(ctx context.Context, fb *model.Feedback) error
	// ListNegative returns thumbs-down feedback, newest first, decrypted.
	ListNegative(ctx context.Context, limit int64) ([]model.Feedback, error)
	DeleteByUser(ctx context.Context, userID primitive.ObjectID) error
	DeleteByConversation(ctx context.Context, conversationID primitive.ObjectID) error
	RotateKeys(ctx context.Context) (int, error)
}

type mongoFeedbackRepository struct {
	collection *mongo.Collection
	keys       *util.KeyRing
}

func NewFeedbackRepository(db *mongo.Database, keys *util.KeyRing) FeedbackRepository {
	return &mongoFeedbackRepository{collection: db.Collection("feedback"), keys: keys}
}

func (r *mongoFeedbackRepository) Upsert(ctx context.Context, fb *model.Feedback) error {
	enc, err := r.keys.Encrypt(fb.Correction)
	if err != nil {
		return err
	}
	_, err = r.collection.UpdateOne(ctx,
		bson.M{"message_id": fb.MessageID, "user_id": fb.UserID},
		bson.M{
			"$set": bson.M{
				"conversation_id": fb.ConversationID, "run_id": fb.RunID,
				"rating": fb.Rating, "correction": enc, "created_at": time.Now(),
			},
		},
		options.Update().SetUpsert(true))
	return err
}

func (r *mongoFeedbackRepository) ListNegative(ctx context.Context, limit int64) ([]model.Feedback, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"rating": -1},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	var out []model.Feedback
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Correction, err = r.keys.Decrypt(out[i].Correction); err != nil {
			out[i].Correction = undecryptable
		}
	}
	return out, nil
}

func (r *mongoFeedbackRepository) DeleteByUser(ctx context.Context, userID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}

func (r *mongoFeedbackRepository) DeleteByConversation(ctx context.Context, conversationID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"conversation_id": conversationID})
	return err
}

func (r *mongoFeedbackRepository) RotateKeys(ctx context.Context) (int, error) {
	return rotateField(ctx, r.collection, r.keys, "correction")
}
