package model

import (
	"time"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type EventType string

const (
	EventUserMessageReceived      EventType = "user_message_received"
	EventPlannerOutput            EventType = "planner_output"
	EventOrchestrationStarted     EventType = "orchestration_started"
	EventOrchestrationFinished    EventType = "orchestration_finished"
	EventTaskStarted              EventType = "task_started"
	EventTaskFinished             EventType = "task_finished"
	EventModelLoadStarted         EventType = "model_load_started"
	EventModelLoadFinished        EventType = "model_load_finished"
	EventAssistantMessageGenerated EventType = "assistant_message_generated"
	EventRAGSearchStarted         EventType = "rag_search_started"
	EventRAGSearchFinished        EventType = "rag_search_finished"
	EventAttachmentResolved       EventType = "attachment_resolved"
	EventSummarizationStarted      EventType = "summarization_started"
	EventSummarizationFinished     EventType = "summarization_finished"
	EventToolDecision              EventType = "tool_decision"
	EventSearchStarted             EventType = "search_started"
	EventSearchFinished            EventType = "search_finished"
	EventExtractionStarted         EventType = "extraction_started"
	EventExtractionFinished        EventType = "extraction_finished"
	EventSufficiencyChecked        EventType = "sufficiency_checked"
	EventGroundedGenerationStarted EventType = "grounded_generation_started"
)

type ConversationEvent struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ConversationID primitive.ObjectID `bson:"conversation_id" json:"conversation_id"`
	UserID         primitive.ObjectID     `bson:"user_id" json:"user_id"`
	Type           EventType              `bson:"type" json:"type"`
	Payload        map[string]interface{} `bson:"payload" json:"payload"`
	Timestamp      time.Time              `bson:"timestamp" json:"timestamp"`
}
