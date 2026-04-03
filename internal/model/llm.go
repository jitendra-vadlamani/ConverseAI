package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type LLMProvider string

const (
	ProviderOllama LLMProvider = "ollama"
	ProviderOpenAI LLMProvider = "openai"
	ProviderClaude LLMProvider = "claude"
	ProviderCustom LLMProvider = "custom"
)

type LLMConfig struct {
	ID            *primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	UserID        primitive.ObjectID  `bson:"user_id" json:"user_id"`
	Provider      LLMProvider         `bson:"provider" json:"provider"`
	Name          string              `bson:"name" json:"name"`
	ModelName     string              `bson:"model_name" json:"model_name"`
	BaseURL       string              `bson:"base_url,omitempty" json:"base_url,omitempty"`
	APIKey        string              `bson:"api_key,omitempty" json:"api_key,omitempty"`
	Description   string              `bson:"description,omitempty" json:"description,omitempty"`
	ContextWindow int                 `bson:"context_window" json:"context_window"`
	CreatedAt     time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time           `bson:"updated_at" json:"updated_at"`
}

type LLMInfo struct {
	Config   LLMConfig `json:"config"`
	Status   string    `json:"status"` // "online", "offline"
	IsSystem bool      `json:"is_system"`
}
