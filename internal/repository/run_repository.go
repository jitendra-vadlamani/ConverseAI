package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/util"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrLeaseLost means another instance has taken over the run.
var ErrLeaseLost = errors.New("run lease lost")

// RunRepository persists runs. Every write made while executing a run is
// guarded by the owner field, so two instances can never both drive a run.
type RunRepository interface {
	Create(ctx context.Context, run *model.Run) error
	Get(ctx context.Context, id primitive.ObjectID) (*model.Run, error)
	GetOwned(ctx context.Context, id, userID primitive.ObjectID) (*model.Run, error)
	MarkRunning(ctx context.Context, id primitive.ObjectID, owner string, promptVersion string, params map[string]any) error
	SaveState(ctx context.Context, id primitive.ObjectID, owner, state string, modelsUsed []string) error
	SavePartial(ctx context.Context, id primitive.ObjectID, owner, partial string, seq int64) error
	// Heartbeat extends the lease and reports whether cancel was requested.
	Heartbeat(ctx context.Context, id primitive.ObjectID, owner string) (cancelRequested bool, err error)
	Finish(ctx context.Context, id primitive.ObjectID, owner string, status model.RunStatus, errMsg string, promptTokens, completionTokens int) error
	RequestCancel(ctx context.Context, id, userID primitive.ObjectID) error
	// Release gives up the lease without finishing the run, so another
	// instance (or this one after a restart) resumes it right away.
	Release(ctx context.Context, id primitive.ObjectID, owner string) error
	// ClaimStale takes over one unfinished run whose owner stopped
	// heartbeating before staleBefore and increments its attempt counter.
	// It returns ErrNotFound if there is none.
	ClaimStale(ctx context.Context, owner string, staleBefore time.Time) (*model.Run, error)
	DeleteByUser(ctx context.Context, userID primitive.ObjectID) error
	RotateKeys(ctx context.Context) (int, error)
}

type mongoRunRepository struct {
	collection *mongo.Collection
	keys       *util.KeyRing
}

func NewRunRepository(db *mongo.Database, keys *util.KeyRing) RunRepository {
	return &mongoRunRepository{collection: db.Collection("runs"), keys: keys}
}

func (r *mongoRunRepository) Create(ctx context.Context, run *model.Run) error {
	if run.ID.IsZero() {
		run.ID = primitive.NewObjectID()
	}
	now := time.Now()
	run.CreatedAt, run.HeartbeatAt = now, now
	_, err := r.collection.InsertOne(ctx, run)
	return err
}

func (r *mongoRunRepository) Get(ctx context.Context, id primitive.ObjectID) (*model.Run, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

func (r *mongoRunRepository) GetOwned(ctx context.Context, id, userID primitive.ObjectID) (*model.Run, error) {
	return r.findOne(ctx, bson.M{"_id": id, "user_id": userID})
}

func (r *mongoRunRepository) findOne(ctx context.Context, filter bson.M) (*model.Run, error) {
	var run model.Run
	if err := r.collection.FindOne(ctx, filter).Decode(&run); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var err error
	if run.State, err = r.keys.Decrypt(run.State); err != nil {
		return nil, fmt.Errorf("decrypt run state: %w", err)
	}
	if run.Partial, err = r.keys.Decrypt(run.Partial); err != nil {
		return nil, fmt.Errorf("decrypt run partial: %w", err)
	}
	return &run, nil
}

func (r *mongoRunRepository) ownedUpdate(ctx context.Context, id primitive.ObjectID, owner string, update bson.M) error {
	res, err := r.collection.UpdateOne(ctx, bson.M{"_id": id, "owner": owner}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (r *mongoRunRepository) MarkRunning(ctx context.Context, id primitive.ObjectID, owner, promptVersion string, params map[string]any) error {
	now := time.Now()
	return r.ownedUpdate(ctx, id, owner, bson.M{"$set": bson.M{
		"status": model.RunRunning, "started_at": now, "heartbeat_at": now,
		"prompt_version": promptVersion, "model_params": params,
	}})
}

func (r *mongoRunRepository) SaveState(ctx context.Context, id primitive.ObjectID, owner, state string, modelsUsed []string) error {
	enc, err := r.keys.Encrypt(state)
	if err != nil {
		return err
	}
	return r.ownedUpdate(ctx, id, owner, bson.M{"$set": bson.M{"state": enc, "models_used": modelsUsed, "heartbeat_at": time.Now()}})
}

func (r *mongoRunRepository) SavePartial(ctx context.Context, id primitive.ObjectID, owner, partial string, seq int64) error {
	enc, err := r.keys.Encrypt(partial)
	if err != nil {
		return err
	}
	return r.ownedUpdate(ctx, id, owner, bson.M{"$set": bson.M{"partial": enc, "seq": seq}})
}

func (r *mongoRunRepository) Heartbeat(ctx context.Context, id primitive.ObjectID, owner string) (bool, error) {
	var run struct {
		CancelRequested bool `bson:"cancel_requested"`
	}
	err := r.collection.FindOneAndUpdate(ctx, bson.M{"_id": id, "owner": owner},
		bson.M{"$set": bson.M{"heartbeat_at": time.Now()}},
		options.FindOneAndUpdate().SetProjection(bson.M{"cancel_requested": 1})).Decode(&run)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, ErrLeaseLost
	}
	return run.CancelRequested, err
}

func (r *mongoRunRepository) Finish(ctx context.Context, id primitive.ObjectID, owner string, status model.RunStatus, errMsg string, promptTokens, completionTokens int) error {
	return r.ownedUpdate(ctx, id, owner, bson.M{
		"$set": bson.M{
			"status": status, "error": errMsg, "finished_at": time.Now(),
			"prompt_tokens": promptTokens, "completion_tokens": completionTokens,
		},
		"$unset": bson.M{"state": "", "partial": ""},
	})
}

func (r *mongoRunRepository) RequestCancel(ctx context.Context, id, userID primitive.ObjectID) error {
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "user_id": userID, "status": bson.M{"$in": []model.RunStatus{model.RunQueued, model.RunRunning}}},
		bson.M{"$set": bson.M{"cancel_requested": true}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *mongoRunRepository) Release(ctx context.Context, id primitive.ObjectID, owner string) error {
	return r.ownedUpdate(ctx, id, owner, bson.M{"$set": bson.M{"owner": "", "heartbeat_at": time.Unix(0, 0)}})
}

func (r *mongoRunRepository) ClaimStale(ctx context.Context, owner string, staleBefore time.Time) (*model.Run, error) {
	var run model.Run
	err := r.collection.FindOneAndUpdate(ctx,
		bson.M{
			"status":       bson.M{"$in": []model.RunStatus{model.RunQueued, model.RunRunning}},
			"heartbeat_at": bson.M{"$lt": staleBefore},
		},
		bson.M{"$set": bson.M{"owner": owner, "heartbeat_at": time.Now()}, "$inc": bson.M{"attempts": 1}},
		options.FindOneAndUpdate().SetReturnDocument(options.After).SetSort(bson.D{{Key: "created_at", Value: 1}}),
	).Decode(&run)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if run.State, err = r.keys.Decrypt(run.State); err != nil {
		return nil, err
	}
	if run.Partial, err = r.keys.Decrypt(run.Partial); err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *mongoRunRepository) DeleteByUser(ctx context.Context, userID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}

func (r *mongoRunRepository) RotateKeys(ctx context.Context) (int, error) {
	a, err := rotateField(ctx, r.collection, r.keys, "state")
	if err != nil {
		return a, err
	}
	b, err := rotateField(ctx, r.collection, r.keys, "partial")
	return a + b, err
}
