package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

func (s RunStatus) Terminal() bool {
	return s == RunSucceeded || s == RunFailed || s == RunCancelled
}

// Run is one assistant turn executing in the background. It is persisted
// after every agent step so a restart resumes it instead of losing it.
type Run struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ConversationID primitive.ObjectID `bson:"conversation_id" json:"conversation_id"`
	UserID         primitive.ObjectID `bson:"user_id" json:"-"`
	UserMessageID  primitive.ObjectID `bson:"user_message_id" json:"user_message_id"`
	Model          string             `bson:"model" json:"model"`
	Status         RunStatus          `bson:"status" json:"status"`
	Error          string             `bson:"error,omitempty" json:"error,omitempty"`

	// Record of how the answer was produced, for reproducibility.
	PromptVersion string         `bson:"prompt_version,omitempty" json:"prompt_version,omitempty"`
	ModelParams   map[string]any `bson:"model_params,omitempty" json:"model_params,omitempty"`
	ModelsUsed    []string       `bson:"models_used,omitempty" json:"models_used,omitempty"`

	// State is the encrypted, JSON-encoded agent state (steps taken so far).
	State string `bson:"state,omitempty" json:"-"`
	// Partial is the encrypted answer text streamed so far; Seq counts chunks.
	Partial string `bson:"partial,omitempty" json:"-"`
	Seq     int64  `bson:"seq" json:"seq"`

	Owner           string    `bson:"owner,omitempty" json:"-"`
	HeartbeatAt     time.Time `bson:"heartbeat_at" json:"-"`
	CancelRequested bool      `bson:"cancel_requested,omitempty" json:"-"`
	Attempts        int       `bson:"attempts" json:"attempts"`

	PromptTokens     int `bson:"prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int `bson:"completion_tokens" json:"completion_tokens"`

	CreatedAt  time.Time  `bson:"created_at" json:"created_at"`
	StartedAt  *time.Time `bson:"started_at,omitempty" json:"started_at,omitempty"`
	FinishedAt *time.Time `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
}
