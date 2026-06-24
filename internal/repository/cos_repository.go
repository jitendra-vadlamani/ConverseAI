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

type ProjectRepository interface {
	Create(ctx context.Context, project *model.Project) (*model.Project, error)
	GetByID(ctx context.Context, id primitive.ObjectID) (*model.Project, error)
	ListByUserID(ctx context.Context, userID primitive.ObjectID) ([]model.Project, error)
	Update(ctx context.Context, project *model.Project) error
	Delete(ctx context.Context, id primitive.ObjectID) error
}

type MongoProjectRepository struct {
	collection *mongo.Collection
}

func NewProjectRepository(db *mongo.Database) ProjectRepository {
	return &MongoProjectRepository{
		collection: db.Collection("projects"),
	}
}

func (r *MongoProjectRepository) Create(ctx context.Context, project *model.Project) (*model.Project, error) {
	if project.ID.IsZero() {
		project.ID = primitive.NewObjectID()
	}
	now := time.Now()
	project.CreatedAt = now
	project.UpdatedAt = now
	if project.Status == "" {
		project.Status = "active"
	}
	if project.Tasks == nil {
		project.Tasks = []model.ProjectTask{}
	}
	if project.Competencies == nil {
		project.Competencies = []model.Competency{}
	}
	if project.MemoryItems == nil {
		project.MemoryItems = []model.MemoryItem{}
	}

	_, err := r.collection.InsertOne(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("failed to create project: %w", err)
	}
	return project, nil
}

func (r *MongoProjectRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*model.Project, error) {
	var project model.Project
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&project)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find project: %w", err)
	}
	return &project, nil
}

func (r *MongoProjectRepository) ListByUserID(ctx context.Context, userID primitive.ObjectID) ([]model.Project, error) {
	opts := options.Find().SetSort(bson.M{"updated_at": -1})
	cursor, err := r.collection.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query projects: %w", err)
	}
	defer cursor.Close(ctx)

	var projects []model.Project
	if err := cursor.All(ctx, &projects); err != nil {
		return nil, fmt.Errorf("failed to decode projects list: %w", err)
	}

	if projects == nil {
		projects = []model.Project{}
	}
	return projects, nil
}

func (r *MongoProjectRepository) Update(ctx context.Context, project *model.Project) error {
	project.UpdatedAt = time.Now()
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": project.ID}, project)
	if err != nil {
		return fmt.Errorf("failed to update project: %w", err)
	}
	return nil
}

func (r *MongoProjectRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	return nil
}
