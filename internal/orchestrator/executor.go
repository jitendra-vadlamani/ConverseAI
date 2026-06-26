package orchestrator

import (
	"context"
	"strings"

	"ai-chat/internal/events"
	"ai-chat/internal/mcp"
	"ai-chat/internal/repository"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Executor interface {
	ExecuteStep(ctx context.Context, toolName string, arguments map[string]interface{}, convID, userID primitive.ObjectID) (string, error)
}

type mcpExecutor struct {
	mcpRegistry mcp.Registry
	eventRepo   repository.EventRepository
	eventBroker events.EventBroker
}

func NewExecutor(mcpRegistry mcp.Registry, eventRepo repository.EventRepository, eventBroker events.EventBroker) Executor {
	return &mcpExecutor{
		mcpRegistry: mcpRegistry,
		eventRepo:   eventRepo,
		eventBroker: eventBroker,
	}
}

func (e *mcpExecutor) ExecuteStep(ctx context.Context, toolName string, arguments map[string]interface{}, convID, userID primitive.ObjectID) (string, error) {
	// Inject User ID and Conversation ID into context
	ctx = context.WithValue(ctx, mcp.UserIDKey, userID)
	ctx = context.WithValue(ctx, mcp.ConversationIDKey, convID)

	result, err := e.mcpRegistry.CallTool(ctx, toolName, arguments)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, content := range result.Content {
		if content.Type == "text" {
			sb.WriteString(content.Text)
		}
	}
	return sb.String(), nil
}
