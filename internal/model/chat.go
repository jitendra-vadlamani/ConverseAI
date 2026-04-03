package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
)

type Message struct {
	Role      MessageRole `bson:"role" json:"role"`
	Content   string      `bson:"content" json:"content"`
	Reasoning string      `bson:"reasoning,omitempty" json:"reasoning,omitempty"`
	CreatedAt time.Time   `bson:"created_at" json:"created_at"`
}

type Conversation struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID       primitive.ObjectID `bson:"user_id" json:"user_id"`
	Title        string             `bson:"title" json:"title"`
	ModelConfigID primitive.ObjectID `bson:"model_config_id,omitempty" json:"model_config_id,omitempty"`
	ModelName     string             `bson:"model_name,omitempty" json:"model_name,omitempty"`
	Messages      []Message          `bson:"messages" json:"messages"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}
