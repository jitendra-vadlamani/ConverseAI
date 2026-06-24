package orchestrator

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"ai-chat/internal/mcp"
	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/manager"
	"ai-chat/internal/repository"
	"ai-chat/internal/events"
	"ai-chat/internal/storage"
	"ai-chat/internal/util"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	perTaskTimeout    = 60 * time.Second
	maxOutputSize     = 32 * 1024 // 32KB — cap on output passed via {{PREVIOUS_OUTPUT}}
)

type Executor interface {
	Execute(ctx context.Context, tasks []model.Task, convID, userID primitive.ObjectID) ([]model.Task, int, error)
	ExecuteStep(ctx context.Context, step *model.PlanStep, convID, userID primitive.ObjectID) (string, int, error)
}

type sequentialExecutor struct {
	client         ollama.Client
	modelManager   manager.ModelManager
	storageService storage.StorageService
	eventRepo      repository.EventRepository
	eventBroker    events.EventBroker
	systemRepo     repository.SystemLLMRepository
	mcpRegistry    mcp.Registry
}

func NewExecutor(client ollama.Client, modelManager manager.ModelManager, storageService storage.StorageService, eventRepo repository.EventRepository, eventBroker events.EventBroker, systemRepo repository.SystemLLMRepository, mcpRegistry mcp.Registry) Executor {
	return &sequentialExecutor{
		client:         client,
		modelManager:   modelManager,
		storageService: storageService,
		eventRepo:      eventRepo,
		eventBroker:    eventBroker,
		systemRepo:     systemRepo,
		mcpRegistry:    mcpRegistry,
	}
}

func (s *sequentialExecutor) emitEvent(ctx context.Context, conversationID, userID primitive.ObjectID, eventType model.EventType, payload map[string]interface{}) {
	event := model.ConversationEvent{
		ConversationID: conversationID,
		UserID:         userID,
		Type:           eventType,
		Payload:        payload,
		Timestamp:      time.Now(),
	}
	if err := s.eventRepo.StoreEvent(ctx, event); err != nil {
		log.Printf("[Executor] Warning: Failed to store event %s: %v", eventType, err)
	}
}

func (e *sequentialExecutor) Execute(ctx context.Context, tasks []model.Task, convID, userID primitive.ObjectID) ([]model.Task, int, error) {
	fmt.Printf("[Executor] Starting execution for %d tasks\n", len(tasks))
	
	totalTokens := 0
	var lastOutput string

	for i := range tasks {
		t := &tasks[i]
		t.Status = "running"
		t.CreatedAt = time.Now()

		// Substitute {{PREVIOUS_OUTPUT}} with capped output from the previous task
		if strings.Contains(t.Input, "{{PREVIOUS_OUTPUT}}") {
			t.Input = strings.ReplaceAll(t.Input, "{{PREVIOUS_OUTPUT}}", lastOutput)
		}

		fmt.Printf("[Executor] Running task %d (%s) with model %s\n", i+1, t.Type, t.Model)

		// Emit Task Started Event
		e.emitEvent(ctx, convID, userID, model.EventTaskStarted, map[string]interface{}{
			"task_index": i, "type": t.Type, "model": t.Model,
			"message": fmt.Sprintf("Starting task %d: %s using model %s", i+1, t.Type, t.Model),
		})

		// VRAM Management: Ensure current task model is loaded
		if err := e.modelManager.PrepareModel(ctx, t.Model); err != nil {
			fmt.Printf("[Executor] Warning: PrepareModel failed: %v\n", err)
		}

		// Per-task timeout
		taskCtx, taskCancel := context.WithTimeout(ctx, perTaskTimeout)

		var output *ollama.GenerateResponse
		var err error
 
		switch t.Type {
		case model.TaskSummarize:
			output, err = e.runSummarize(taskCtx, t)
		case model.TaskTranslate:
			output, err = e.runTranslate(taskCtx, t)
		case model.TaskAnalyze:
			output, err = e.runAnalyze(taskCtx, t)
		case model.TaskChat:
			output, err = e.runChat(taskCtx, t)
		case model.TaskSearch:
			output = &ollama.GenerateResponse{
				Response: fmt.Sprintf("[Search] Search is not yet implemented. Query was: %s", t.Input),
				Done: true,
			}
		default:
			err = fmt.Errorf("unsupported task type: %s", t.Type)
		}

		taskCancel()

		if err != nil {
			t.Status = "failed"
			t.Error = err.Error()
			e.emitEvent(ctx, convID, userID, model.EventTaskFinished, map[string]interface{}{
				"task_index": i, "success": false, "error": err.Error(),
				"message": fmt.Sprintf("Task %d (%s) failed: %s", i+1, t.Type, err.Error()),
			})
			return tasks, totalTokens, fmt.Errorf("task %d (%s) failed: %v", i+1, t.Type, err)
		}

		t.Output = output.Response
		totalTokens += output.EvalCount + output.PromptEvalCount
		t.Status = "completed"
		t.CompletedAt = time.Now()

		// Cap output size before passing as {{PREVIOUS_OUTPUT}} to the next task
		lastOutput = output.Response
		if len(lastOutput) > maxOutputSize {
			lastOutput = lastOutput[:maxOutputSize] + "\n... [output truncated]"
		}

		// Emit Task Finished (Success) Event
		e.emitEvent(ctx, convID, userID, model.EventTaskFinished, map[string]interface{}{
			"task_index": i, "success": true,
			"message": fmt.Sprintf("Task %d (%s) completed successfully.", i+1, t.Type),
		})
	}
	return tasks, totalTokens, nil
}

func (e *sequentialExecutor) ExecuteStep(ctx context.Context, step *model.PlanStep, convID, userID primitive.ObjectID) (string, int, error) {
	fmt.Printf("[Executor] Executing step: %s (Reason: %s)\n", step.Tool, step.Reason)

	// Emit Tool Started Event
	e.emitEvent(ctx, convID, userID, model.EventTaskStarted, map[string]interface{}{
		"tool": step.Tool, "reason": step.Reason,
		"message": fmt.Sprintf("Starting tool: %s", step.Tool),
	})

	// Inject User ID and Conversation ID into context for tools to retrieve
	ctx = context.WithValue(ctx, mcp.UserIDKey, userID)
	ctx = context.WithValue(ctx, mcp.ConversationIDKey, convID)

	var output string
	var err error

	if step.Tool == model.ToolNone || step.Tool == "none" {
		output = "Done."
	} else {
		var result *mcp.CallToolResult
		result, err = e.mcpRegistry.CallTool(ctx, step.Tool, step.Input)
		if err == nil {
			var sb strings.Builder
			for _, content := range result.Content {
				if content.Type == "text" {
					sb.WriteString(content.Text)
				}
			}
			output = sb.String()
		}
	}

	if err != nil {
		e.emitEvent(ctx, convID, userID, model.EventTaskFinished, map[string]interface{}{
			"tool": step.Tool, "success": false, "error": err.Error(),
		})
		return "", 0, err
	}

	e.emitEvent(ctx, convID, userID, model.EventTaskFinished, map[string]interface{}{
		"tool": step.Tool, "success": true,
	})

	return output, 0, nil
}


func (s *sequentialExecutor) getNumCtx(modelName string) int {
	if meta := s.systemRepo.GetMetadata(modelName); meta != nil && meta.ContextWindow > 0 {
		return meta.ContextWindow
	}
	return 8192 // safe default
}

func (s *sequentialExecutor) runSummarize(ctx context.Context, t *model.Task) (*ollama.GenerateResponse, error) {
	images, extraContext, _ := s.resolveAttachments(ctx, t.Attachments)
	prompt := fmt.Sprintf("Summarize the following input. Respond ONLY with the summary.\n\nINPUT:\n%s", t.Input)
	if extraContext != "" {
		prompt = fmt.Sprintf("%s\n\n### ADDITIONAL FILE CONTEXT:\n%s", prompt, extraContext)
	}

	return s.client.Generate(ctx, &ollama.GenerateRequest{
		Model: t.Model, Prompt: prompt, Images: images, Stream: false,
		Options: map[string]interface{}{"num_ctx": s.getNumCtx(t.Model)},
	})
}

func (s *sequentialExecutor) runTranslate(ctx context.Context, t *model.Task) (*ollama.GenerateResponse, error) {
	return s.client.Generate(ctx, &ollama.GenerateRequest{
		Model: t.Model, Prompt: t.Input, Stream: false,
		Options: map[string]interface{}{"num_ctx": s.getNumCtx(t.Model)},
	})
}

func (s *sequentialExecutor) runAnalyze(ctx context.Context, t *model.Task) (*ollama.GenerateResponse, error) {
	images, extraContext, _ := s.resolveAttachments(ctx, t.Attachments)
	prompt := fmt.Sprintf("Analyze the following input and provide a detailed report. Respond ONLY with the analysis.\n\nINPUT:\n%s", t.Input)
	if extraContext != "" {
		prompt = fmt.Sprintf("%s\n\n### ADDITIONAL FILE CONTEXT:\n%s", prompt, extraContext)
	}

	return s.client.Generate(ctx, &ollama.GenerateRequest{
		Model: t.Model, Prompt: prompt, Images: images, Stream: false,
		Options: map[string]interface{}{"num_ctx": s.getNumCtx(t.Model)},
	})
}

func (s *sequentialExecutor) runChat(ctx context.Context, t *model.Task) (*ollama.GenerateResponse, error) {
	return s.client.Generate(ctx, &ollama.GenerateRequest{
		Model: t.Model, Prompt: t.Input, Stream: false,
		Options: map[string]interface{}{"num_ctx": s.getNumCtx(t.Model)},
	})
}

func (s *sequentialExecutor) resolveAttachments(ctx context.Context, attachmentIDs []string) ([]string, string, error) {
	var images []string
	var extraContext strings.Builder

	for _, id := range attachmentIDs {
		data, err := s.storageService.Get(ctx, id)
		if err != nil {
			log.Printf("[Executor] Failed to get attachment %s: %v", id, err)
			continue
		}

		ext := strings.ToLower(filepath.Ext(id))
		if util.IsImage(ext) {
			images = append(images, base64.StdEncoding.EncodeToString(data))
		} else if util.IsText(ext) {
			extraContext.WriteString(fmt.Sprintf("\n--- FILE: %s ---\n%s\n", id, string(data)))
		} else if ext == ".pdf" {
			text, err := util.ExtractTextFromPDF(data)
			if err != nil {
				log.Printf("[Executor] Failed to extract text from PDF %s: %v", id, err)
				continue
			}
			extraContext.WriteString(fmt.Sprintf("\n--- PDF DOCUMENT: %s ---\n%s\n", id, text))
		}
	}

	return images, extraContext.String(), nil
}
