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
	Provider      LLMProvider         `bson:"provider" json:"provider"`
	Name          string              `bson:"name" json:"name"`
	ModelName     string              `bson:"model_name" json:"model_name"`
	BaseURL       string              `bson:"base_url,omitempty" json:"base_url,omitempty"`
	Description   string              `bson:"description,omitempty" json:"description,omitempty"`
	ContextWindow int                 `bson:"context_window" json:"context_window"`
	
	// Advanced Metadata
	Architecture    string   `bson:"architecture,omitempty" json:"architecture,omitempty"`
	ParametersCount string   `bson:"parameters_count,omitempty" json:"parameters_count,omitempty"`
	EmbeddingLength int      `bson:"embedding_length,omitempty" json:"embedding_length,omitempty"`
	Quantization    string   `bson:"quantization,omitempty" json:"quantization,omitempty"`
	Capabilities    []string `bson:"capabilities,omitempty" json:"capabilities,omitempty"`
	
	// Default Parameters
	Temperature   float64  `bson:"temperature,omitempty" json:"temperature,omitempty"`
	TopK          int      `bson:"top_k,omitempty" json:"top_k,omitempty"`
	TopP          float64  `bson:"top_p,omitempty" json:"top_p,omitempty"`
	RepeatPenalty float64  `bson:"repeat_penalty,omitempty" json:"repeat_penalty,omitempty"`
	StopSequences []string `bson:"stop_sequences,omitempty" json:"stop_sequences,omitempty"`

	CreatedAt     time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time           `bson:"updated_at" json:"updated_at"`
}

type LLMInfo struct {
	Config   LLMConfig `json:"config"`
	Status   string    `json:"status"` // "online", "offline"
	IsSystem bool      `json:"is_system"`
}
