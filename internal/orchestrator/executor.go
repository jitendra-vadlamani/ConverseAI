package orchestrator

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/manager"
	"ai-chat/internal/repository"
	"ai-chat/internal/events"
	"ai-chat/internal/storage"
	"github.com/ledongthuc/pdf"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Executor interface {
	Execute(ctx context.Context, tasks []model.Task, convID, userID primitive.ObjectID) ([]model.Task, error)
}

type sequentialExecutor struct {
	client         ollama.Client
	modelManager   manager.ModelManager
	storageService storage.StorageService
	eventRepo      repository.EventRepository
	eventBroker    events.EventBroker
}

func NewExecutor(client ollama.Client, modelManager manager.ModelManager, storageService storage.StorageService, eventRepo repository.EventRepository, eventBroker events.EventBroker) Executor {
	return &sequentialExecutor{
		client:         client,
		modelManager:   modelManager,
		storageService: storageService,
		eventRepo:      eventRepo,
		eventBroker:    eventBroker,
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

func (e *sequentialExecutor) Execute(ctx context.Context, tasks []model.Task, convID, userID primitive.ObjectID) ([]model.Task, error) {
	fmt.Printf("[Executor] Starting execution for %d tasks\n", len(tasks))

	var lastOutput string

	for i := range tasks {
		t := &tasks[i]
		t.Status = "running"
		t.CreatedAt = time.Now()

		if strings.Contains(t.Input, "{{PREVIOUS_OUTPUT}}") {
			t.Input = strings.ReplaceAll(t.Input, "{{PREVIOUS_OUTPUT}}", lastOutput)
		}

		fmt.Printf("[Executor] Running task %d (%s) with model %s\n", i+1, t.Type, t.Model)

		// Emit Task Started Event
		e.emitEvent(ctx, convID, userID, model.EventTaskStarted, map[string]interface{}{
			"task_index": i, "type": t.Type, "model": t.Model,
		})

		// VRAM Management: Ensure current task model is loaded
		if err := e.modelManager.PrepareModel(ctx, t.Model); err != nil {
			fmt.Printf("[Executor] Warning: PrepareModel failed: %v\n", err)
		}

		var output string
		var err error

		switch t.Type {
		case model.TaskSummarize:
			output, err = e.runSummarize(ctx, t)
		case model.TaskTranslate:
			output, err = e.runTranslate(ctx, t)
		case model.TaskSearch:
			output, err = e.runSearch(ctx, t)
		case model.TaskAnalyze:
			output, err = e.runAnalyze(ctx, t)
		default:
			err = fmt.Errorf("unsupported task type: %s", t.Type)
		}

		if err != nil {
			t.Status = "failed"
			t.Error = err.Error()
			// Emit Task Finished (Failed) Event
			e.emitEvent(ctx, convID, userID, model.EventTaskFinished, map[string]interface{}{
				"task_index": i, "success": false, "error": err.Error(),
			})
			return tasks, fmt.Errorf("task %d (%s) failed: %v", i+1, t.Type, err)
		}

		t.Output = output
		t.Status = "completed"
		t.CompletedAt = time.Now()
		lastOutput = output

		// Emit Task Finished (Success) Event
		e.emitEvent(ctx, convID, userID, model.EventTaskFinished, map[string]interface{}{
			"task_index": i, "success": true,
		})
	}

	fmt.Printf("[Executor] Workflow completed successfully\n")
	return tasks, nil
}

func (s *sequentialExecutor) runSummarize(ctx context.Context, t *model.Task) (string, error) {
	images, extraContext, _ := s.resolveAttachments(ctx, t.Attachments)
	prompt := fmt.Sprintf("Summarize the following input. Respond ONLY with the summary.\n\nINPUT:\n%s", t.Input)
	if extraContext != "" {
		prompt = fmt.Sprintf("%s\n\n### ADDITIONAL FILE CONTEXT:\n%s", prompt, extraContext)
	}

	resp, err := s.client.Generate(ctx, &ollama.GenerateRequest{
		Model: t.Model, Prompt: prompt, Images: images, Stream: false,
	})
	if err != nil { return "", err }
	return strings.TrimSpace(resp.Response), nil
}

func (s *sequentialExecutor) runTranslate(ctx context.Context, t *model.Task) (string, error) {
	resp, err := s.client.Generate(ctx, &ollama.GenerateRequest{
		Model: t.Model, Prompt: t.Input, Stream: false,
	})
	if err != nil { return "", err }
	return strings.TrimSpace(resp.Response), nil
}

func (s *sequentialExecutor) runSearch(ctx context.Context, t *model.Task) (string, error) {
	return fmt.Sprintf("Mock search results for: %s", t.Input), nil
}

func (s *sequentialExecutor) runAnalyze(ctx context.Context, t *model.Task) (string, error) {
	images, extraContext, _ := s.resolveAttachments(ctx, t.Attachments)
	prompt := fmt.Sprintf("Analyze the following input and provide a detailed report. Respond ONLY with the analysis.\n\nINPUT:\n%s", t.Input)
	if extraContext != "" {
		prompt = fmt.Sprintf("%s\n\n### ADDITIONAL FILE CONTEXT:\n%s", prompt, extraContext)
	}

	resp, err := s.client.Generate(ctx, &ollama.GenerateRequest{
		Model: t.Model, Prompt: prompt, Images: images, Stream: false,
	})
	if err != nil { return "", err }
	return strings.TrimSpace(resp.Response), nil
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
		if isImage(ext) {
			images = append(images, base64.StdEncoding.EncodeToString(data))
		} else if isText(ext) {
			extraContext.WriteString(fmt.Sprintf("\n--- FILE: %s ---\n%s\n", id, string(data)))
		} else if ext == ".pdf" {
			text, err := extractTextFromPDF(data)
			if err != nil {
				log.Printf("[Executor] Failed to extract text from PDF %s: %v", id, err)
				continue
			}
			extraContext.WriteString(fmt.Sprintf("\n--- PDF DOCUMENT: %s ---\n%s\n", id, text))
		}
	}

	return images, extraContext.String(), nil
}

func extractTextFromPDF(data []byte) (string, error) {
	r := bytes.NewReader(data)
	pdfReader, err := pdf.NewReader(r, int64(len(data)))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	for i := 1; i <= pdfReader.NumPage(); i++ {
		p := pdfReader.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
	}
	return buf.String(), nil
}

func isImage(ext string) bool {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp": return true
	}
	return false
}

func isText(ext string) bool {
	switch ext {
	case ".txt", ".csv", ".json", ".md", ".go", ".py", ".js", ".ts": return true
	}
	return false
}
