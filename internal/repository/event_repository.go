package repository

import (
	"context"
	"encoding/json"
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

type EventRepository interface {
	StoreEvent(ctx context.Context, event *model.ConversationEvent) error
	GetEventsByConversationID(ctx context.Context, conversationID primitive.ObjectID) ([]model.ConversationEvent, error)
	DeleteEventsByConversationID(ctx context.Context, conversationID primitive.ObjectID) error
	DeleteByUser(ctx context.Context, userID primitive.ObjectID) error
	RotateKeys(ctx context.Context) (int, error)
	// PurgeLegacyPlaintext removes the unencrypted `payload` field written by
	// versions before event encryption.
	PurgeLegacyPlaintext(ctx context.Context) (int64, error)
}

// storedEvent is the on-disk shape: the payload (prompts, tool arguments,
// tool output) is JSON-encoded and encrypted with the database key.
type storedEvent struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	ConversationID primitive.ObjectID `bson:"conversation_id"`
	UserID         primitive.ObjectID `bson:"user_id"`
	RunID          primitive.ObjectID `bson:"run_id,omitempty"`
	Type           model.EventType    `bson:"type"`
	PayloadEnc     string             `bson:"payload_enc,omitempty"`
	Timestamp      time.Time          `bson:"timestamp"`
}

type mongoEventRepository struct {
	collection *mongo.Collection
	keys       *util.KeyRing
}

func NewEventRepository(db *mongo.Database, keys *util.KeyRing) EventRepository {
	return &mongoEventRepository{collection: db.Collection("events"), keys: keys}
}

func (r *mongoEventRepository) StoreEvent(ctx context.Context, e *model.ConversationEvent) error {
	if e.ID.IsZero() {
		e.ID = primitive.NewObjectID()
	}
	raw, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("encode event payload: %w", err)
	}
	enc, err := r.keys.Encrypt(string(raw))
	if err != nil {
		return err
	}
	_, err = r.collection.InsertOne(ctx, storedEvent{
		ID: e.ID, ConversationID: e.ConversationID, UserID: e.UserID, RunID: e.RunID,
		Type: e.Type, PayloadEnc: enc, Timestamp: e.Timestamp,
	})
	return err
}

func (r *mongoEventRepository) GetEventsByConversationID(ctx context.Context, conversationID primitive.ObjectID) ([]model.ConversationEvent, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"conversation_id": conversationID},
		options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}}).SetLimit(2000))
	if err != nil {
		return nil, err
	}
	var stored []storedEvent
	if err := cursor.All(ctx, &stored); err != nil {
		return nil, err
	}
	out := make([]model.ConversationEvent, 0, len(stored))
	for _, s := range stored {
		e := model.ConversationEvent{
			ID: s.ID, ConversationID: s.ConversationID, UserID: s.UserID, RunID: s.RunID,
			Type: s.Type, Timestamp: s.Timestamp,
		}
		if s.PayloadEnc != "" {
			pt, err := r.keys.Decrypt(s.PayloadEnc)
			if err != nil {
				slog.Error("decrypt event payload", "event", s.ID.Hex(), "err", err)
				e.Payload = map[string]any{"message": undecryptable}
			} else if err := json.Unmarshal([]byte(pt), &e.Payload); err != nil {
				e.Payload = map[string]any{"message": "unreadable event payload"}
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func (r *mongoEventRepository) DeleteEventsByConversationID(ctx context.Context, conversationID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"conversation_id": conversationID})
	return err
}

func (r *mongoEventRepository) DeleteByUser(ctx context.Context, userID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}

func (r *mongoEventRepository) RotateKeys(ctx context.Context) (int, error) {
	return rotateField(ctx, r.collection, r.keys, "payload_enc")
}

func (r *mongoEventRepository) PurgeLegacyPlaintext(ctx context.Context) (int64, error) {
	res, err := r.collection.UpdateMany(ctx, bson.M{"payload": bson.M{"$exists": true}}, bson.M{"$unset": bson.M{"payload": ""}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// rotateField re-encrypts a single top-level string field in every document
// of the collection that isn't already on the primary key (plaintext values
// included).
func rotateField(ctx context.Context, coll *mongo.Collection, keys *util.KeyRing, field string) (int, error) {
	cursor, err := coll.Find(ctx, bson.M{field: bson.M{"$type": "string", "$ne": ""}},
		options.Find().SetProjection(bson.M{field: 1}))
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	n := 0
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return n, err
		}
		val, _ := doc[field].(string)
		if !keys.NeedsRotation(val) {
			continue
		}
		pt, err := keys.Decrypt(val)
		if err != nil {
			return n, fmt.Errorf("%s %v: %w", coll.Name(), doc["_id"], err)
		}
		enc, err := keys.Encrypt(pt)
		if err != nil {
			return n, err
		}
		if _, err := coll.UpdateOne(ctx, bson.M{"_id": doc["_id"], field: val}, bson.M{"$set": bson.M{field: enc}}); err != nil {
			return n, err
		}
		n++
	}
	return n, cursor.Err()
}
