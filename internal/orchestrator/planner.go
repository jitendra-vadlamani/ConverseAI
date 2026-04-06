package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-chat/internal/config"
	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/manager"
	"ai-chat/internal/repository"
)

type Planner interface {
	Plan(ctx context.Context, query string, modelName string, images []string) ([]model.Task, int, int, error)
}

type ollamaPlanner struct {
	client       ollama.Client
	modelManager manager.ModelManager
	systemRepo   repository.SystemLLMRepository
	cfg          *config.Config
}

func NewPlanner(client ollama.Client, modelManager manager.ModelManager, systemRepo repository.SystemLLMRepository, cfg *config.Config) Planner {
	return &ollamaPlanner{
		client:       client,
		modelManager: modelManager,
		systemRepo:   systemRepo,
		cfg:          cfg,
	}
}

func (p *ollamaPlanner) Plan(ctx context.Context, query string, modelName string, images []string) ([]model.Task, int, int, error) {
	fmt.Printf("[Planner] Generating plan for query: %s using model: %s (Images: %d)\n", query, modelName, len(images))
	
	if err := p.modelManager.PrepareModel(ctx, modelName); err != nil {
		fmt.Printf("[Planner] Warning: PrepareModel failed: %v\n", err)
	}

	systemPrompt := fmt.Sprintf(`You are a Task Planner. Your job is to decompose user queries into a sequence of atomic tasks.
Available Task Types: "summarize", "translate", "search", "analyze", "chat"

Preferred Models for Tasks:
- OCR: "%s"
- Vision/Analysis: "%s"
- Coding: "%s"
- Translation: "%s"
- General Chat: "%s"

Rules:
1. Return ONLY a JSON array. DO NOT wrap it in an object. No preamble or markdown.
2. If the user mentions "this", "it", or "the file", prioritize using the context provided in the "ADDITIONAL CONTEXT" section.
3. For simple conversation (greetings, off-topic), use [{"type": "chat", "model": "%s", "input": "USER_PROMPT"}].
4. If images are provided, start with an "analyze" task.
5. Use "{{PREVIOUS_OUTPUT}}" to pass results between tasks in a sequence.

Format:
[
  {"type": "task_type", "model": "model_name", "input": "specific instruction"}
]`, 
	p.cfg.DefaultOCRModel, p.cfg.DefaultVisionModel, p.cfg.DefaultCodingModel, p.cfg.DefaultTranslationModel, p.cfg.DefaultChatModel,
	p.cfg.DefaultChatModel)

	var tasks []model.Task
	var lastErr error

	for i := 0; i < 3; i++ {
		if i > 0 { fmt.Printf("[Planner] Retry %d/2\n", i) }

		// Look up context window for this model
		numCtx := 8192 // safe default
		if meta := p.systemRepo.GetMetadata(modelName); meta != nil && meta.ContextWindow > 0 {
			numCtx = meta.ContextWindow
		}

		resp, err := p.client.Generate(ctx, &ollama.GenerateRequest{
			Model: modelName, Prompt: query, System: systemPrompt, Stream: false, Format: "json",
			Options: map[string]interface{}{"num_ctx": numCtx},
		})
		if err != nil {
			lastErr = err
			continue
		}

		cleanJSON := sCleanJSON(resp.Response)
		if err := json.Unmarshal([]byte(cleanJSON), &tasks); err != nil {
			// Second attempt: parse as object and look for "tasks", "plan", "steps", etc.
			var obj map[string]interface{}
			if err2 := json.Unmarshal([]byte(cleanJSON), &obj); err2 == nil {
				// Case A: The object IS the task (no wrapper)
				if _, ok := obj["type"]; ok {
					var singleTask model.Task
					if err3 := json.Unmarshal([]byte(cleanJSON), &singleTask); err3 == nil {
						tasks = []model.Task{singleTask}
					}
				}

				// Case B: The object has a wrapper key ("tasks", "plan", etc.)
				if len(tasks) == 0 {
					possibleKeys := []string{"tasks", "plan", "steps", "items"}
					for _, k := range possibleKeys {
						if t, ok := obj[k].([]interface{}); ok {
							b, _ := json.Marshal(t)
							_ = json.Unmarshal(b, &tasks)
							if len(tasks) > 0 { break }
						}
					}
				}
			}
			
			if len(tasks) == 0 {
				lastErr = fmt.Errorf("failed to parse JSON as tasks array: %v (Response: %s)", err, resp.Response)
				continue
			}
		}

		// --- Post-parse sanitization ---

		// Cap task count to prevent resource exhaustion
		if len(tasks) > 10 {
			fmt.Printf("[Planner] Warning: LLM produced %d tasks, capping to 10\n", len(tasks))
			tasks = tasks[:10]
		}

		// Validate and sanitize each task
		sanitized := make([]model.Task, 0, len(tasks))
		for _, t := range tasks {
			// Skip tasks with empty type or model
			if strings.TrimSpace(string(t.Type)) == "" || strings.TrimSpace(t.Model) == "" {
				fmt.Printf("[Planner] Warning: skipping task with empty type='%s' or model='%s'\n", t.Type, t.Model)
				continue
			}
			sanitized = append(sanitized, t)
		}
		tasks = sanitized

		// Propagate images to ALL tasks (not just the first) — the executor's
		// resolveAttachments handles the appropriate extraction per file type
		if len(images) > 0 {
			for j := range tasks {
				tasks[j].Attachments = images
			}
		}

		if len(tasks) > 0 { return tasks, resp.PromptEvalCount, resp.EvalCount, nil }
	}
	return nil, 0, 0, fmt.Errorf("planning failed after 3 retries: %v", lastErr)
}

func sCleanJSON(raw string) string {
	clean := strings.TrimSpace(raw)
	
	// Basic Markdown Stripping
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	clean = strings.TrimSpace(clean)

	// Robust Extraction: find first '[' or '{' and last ']' or '}'
	startIdx := strings.IndexAny(clean, "[{")
	endIdx := strings.LastIndexAny(clean, "]}")
	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		return clean[startIdx : endIdx+1]
	}

	return clean
}
