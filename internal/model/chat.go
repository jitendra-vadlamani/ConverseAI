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

// Message is one turn in a conversation. Content is exactly what the user
// typed (or the model answered); file text and retrieved context are rebuilt
// from Attachments at request time and never stored here.
type Message struct {
	ID           primitive.ObjectID `bson:"id,omitempty" json:"id,omitempty"`
	Role         MessageRole        `bson:"role" json:"role"`
	Content      string             `bson:"content" json:"content"`
	Reasoning    string             `bson:"reasoning,omitempty" json:"reasoning,omitempty"`
	ModelName    string             `bson:"model_name" json:"model_name"`
	Attachments  []string           `bson:"attachments,omitempty" json:"attachments,omitempty"` // storage object keys
	RunID        primitive.ObjectID `bson:"run_id,omitempty" json:"run_id,omitempty"`
	TokenCount   int                `bson:"token_count" json:"token_count"`
	IsSummarized bool               `bson:"is_summarized" json:"is_summarized"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
}

type Conversation struct {
	ID                primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	UserID            primitive.ObjectID  `bson:"user_id" json:"user_id"`
	Title             string              `bson:"title" json:"title"`
	Messages          []Message           `bson:"messages" json:"messages"`
	Summary           string              `bson:"summary,omitempty" json:"summary,omitempty"`
	SummaryTokenCount int                 `bson:"summary_token_count" json:"summary_token_count"`
	TotalTokens       int                 `bson:"total_tokens" json:"total_tokens"`
	ActiveRunID       *primitive.ObjectID `bson:"active_run_id,omitempty" json:"active_run_id,omitempty"`
	CreatedAt         time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt         time.Time           `bson:"updated_at" json:"updated_at"`
}

// ConversationSummary is what the sidebar needs; listing never loads messages.
type ConversationSummary struct {
	ID        primitive.ObjectID `bson:"_id" json:"id"`
	Title     string             `bson:"title" json:"title"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

// AttachmentIDs returns every distinct attachment in the conversation, in
// first-seen order.
func (c *Conversation) AttachmentIDs() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range c.Messages {
		for _, a := range m.Attachments {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	return out
}

type Feedback struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID         primitive.ObjectID `bson:"user_id" json:"-"`
	ConversationID primitive.ObjectID `bson:"conversation_id" json:"conversation_id"`
	MessageID      primitive.ObjectID `bson:"message_id" json:"message_id"`
	RunID          primitive.ObjectID `bson:"run_id,omitempty" json:"run_id,omitempty"`
	Rating         int                `bson:"rating" json:"rating"` // +1 or -1
	Correction     string             `bson:"correction,omitempty" json:"correction,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
}
