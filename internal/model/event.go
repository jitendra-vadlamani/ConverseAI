package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type EventType string

const (
	EventUserMessageReceived       EventType = "user_message_received"
	EventRunQueued                 EventType = "run_queued"
	EventRunStarted                EventType = "run_started"
	EventRunResumed                EventType = "run_resumed"
	EventRunFinished               EventType = "run_finished"
	EventModelRouted               EventType = "model_routed"
	EventToolDecision              EventType = "tool_decision"
	EventTaskStarted               EventType = "task_started"
	EventTaskFinished              EventType = "task_finished"
	EventAssistantMessageGenerated EventType = "assistant_message_generated"
	EventRAGIngested               EventType = "rag_ingested"
	EventRAGSearchFinished         EventType = "rag_search_finished"
	EventAttachmentResolved        EventType = "attachment_resolved"
	EventSummarizationStarted      EventType = "summarization_started"
	EventSummarizationFinished     EventType = "summarization_finished"
	EventSearchStarted             EventType = "search_started"
	EventSearchFinished            EventType = "search_finished"
	EventExtractionStarted         EventType = "extraction_started"
	EventExtractionFinished        EventType = "extraction_finished"
)

// ConversationEvent is one entry in a conversation's System Logs. The payload
// can contain prompts, tool arguments and tool output, so it is encrypted at
// rest (see repository.EventRepository).
type ConversationEvent struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ConversationID primitive.ObjectID `bson:"conversation_id" json:"conversation_id"`
	UserID         primitive.ObjectID `bson:"user_id" json:"user_id"`
	RunID          primitive.ObjectID `bson:"run_id,omitempty" json:"run_id,omitempty"`
	Type           EventType          `bson:"type" json:"type"`
	Payload        map[string]any     `bson:"-" json:"payload"`
	Timestamp      time.Time          `bson:"timestamp" json:"timestamp"`
}
