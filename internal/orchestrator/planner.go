package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/manager"
)

type Planner interface {
	Plan(ctx context.Context, query string, modelName string, images []string) ([]model.Task, error)
}

type ollamaPlanner struct {
	client       ollama.Client
	modelManager manager.ModelManager
}

func NewPlanner(client ollama.Client, modelManager manager.ModelManager) Planner {
	return &ollamaPlanner{
		client:       client,
		modelManager: modelManager,
	}
}

func (p *ollamaPlanner) Plan(ctx context.Context, query string, modelName string, images []string) ([]model.Task, error) {
	fmt.Printf("[Planner] Generating plan for query: %s using model: %s (Images: %d)\n", query, modelName, len(images))
	
	if err := p.modelManager.PrepareModel(ctx, modelName); err != nil {
		fmt.Printf("[Planner] Warning: PrepareModel failed: %v\n", err)
	}

	systemPrompt := `You are a Task Planner. Convert user queries into a JSON array of tasks.
Available Task Types: "summarize", "translate", "search", "analyze", "chat"

Vision Capability:
If images are provided, you should prioritize vision-capable models (like "qwen3-vl:8b" or "deepseek-ocr:3b") for the first task to describe or extract text from the images.

Rules:
1. Return ONLY a JSON array of tasks.
2. If the user is just having a conversation, return [{"type": "chat"}].
3. Each task must have: "type", "model", "input".
4. For translation tasks, explicitly state the target language in the "input" like: "Translate to Spanish. TEXT: {{PREVIOUS_OUTPUT}}".
5. Use "{{PREVIOUS_OUTPUT}}" as input if it depends on the previous task.

Example Result:
[{"type": "analyze", "model": "qwen3-vl:8b", "input": "Describe this image"}, {"type": "translate", "model": "llama3", "input": "Translate the translation below to Spanish. TEXT: {{PREVIOUS_OUTPUT}}"}]`

	var tasks []model.Task
	var lastErr error

	for i := 0; i < 3; i++ {
		if i > 0 { fmt.Printf("[Planner] Retry %d/2\n", i) }

		resp, err := p.client.Generate(ctx, &ollama.GenerateRequest{
			Model: modelName, Prompt: query, System: systemPrompt, Stream: false, Format: "json",
		})
		if err != nil {
			lastErr = err
			continue
		}

		cleanJSON := sCleanJSON(resp.Response)
		if err := json.Unmarshal([]byte(cleanJSON), &tasks); err != nil {
			lastErr = fmt.Errorf("failed to parse JSON: %v", err)
			continue
		}
		
		// If vision models are needed, ensure we pass attachments to the first task
		if len(images) > 0 && len(tasks) > 0 {
			tasks[0].Attachments = images
		}

		if len(tasks) > 0 { return tasks, nil }
	}
	return nil, fmt.Errorf("failed after 2 retries: %v", lastErr)
}

func sCleanJSON(raw string) string {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	return strings.TrimSpace(clean)
}
