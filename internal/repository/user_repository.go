package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ai-chat/internal/model"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type UserRepository interface {
	Create(ctx context.Context, email, passwordHash string) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	GetByID(ctx context.Context, id primitive.ObjectID) (*model.User, error)
	UpdatePassword(ctx context.Context, id primitive.ObjectID, passwordHash string) error
	Delete(ctx context.Context, id primitive.ObjectID) error
}

type MongoUserRepository struct {
	collection *mongo.Collection
}

func NewUserRepository(db *mongo.Database) UserRepository {
	return &MongoUserRepository{collection: db.Collection("users")}
}

func (r *MongoUserRepository) Create(ctx context.Context, email, passwordHash string) (*model.User, error) {
	user := &model.User{
		ID:           primitive.NewObjectID(),
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}
	if _, err := r.collection.InsertOne(ctx, user); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return user, nil
}

func (r *MongoUserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	return r.findOne(ctx, bson.M{"email": email})
}

func (r *MongoUserRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*model.User, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

func (r *MongoUserRepository) findOne(ctx context.Context, filter bson.M) (*model.User, error) {
	var user model.User
	if err := r.collection.FindOne(ctx, filter).Decode(&user); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find user: %w", err)
	}
	return &user, nil
}

func (r *MongoUserRepository) UpdatePassword(ctx context.Context, id primitive.ObjectID, passwordHash string) error {
	res, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"password_hash": passwordHash}})
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MongoUserRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}
