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
	PlanNext(ctx context.Context, query string, context []model.PlanStep, modelName string, images []string) (*model.PlanStep, int, int, error)
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

func (p *ollamaPlanner) PlanNext(ctx context.Context, query string, history []model.PlanStep, modelName string, images []string) (*model.PlanStep, int, int, error) {
	fmt.Printf("[Planner] Deciding NEXT step for query: %s (History: %d)\n", query, len(history))

	if err := p.modelManager.PrepareModel(ctx, modelName); err != nil {
		fmt.Printf("[Planner] Warning: PrepareModel failed: %v\n", err)
	}

	// Build context from history
	contextStr := ""
	if len(history) > 0 {
		contextStr = "\n## Previous Tool Results\n"
		for i, h := range history {
			contextStr += fmt.Sprintf("[%d] Tool: %s, Result: %s\n", i+1, h.Tool, h.Output)
		}
	}

	systemPrompt := `You are a high-fidelity planning engine that decides the next action in a multi-step AI reasoning system.

Your job is to:
1. Understand the user query. 
2. If the query is complex or multi-part, decompose it into sub-questions (Step 0).
3. Decide whether to use a tool based on the current context.
4. Select the best tool (web_search, retrieve_documents, summarize, ocr_extract).
5. Before answering ("none"), perform a Structured Sufficiency Check.

---

## Available Tools

1. web_search
   Input: { "query": "string" }
   Use when you need current or external information. If previous RAG results were low relevance, use this.

2. retrieve_documents
   Input: { "query": "string" }
   Use to search local user-uploaded files.

3. summarize
   Input: { "text": "string" }

4. ocr_extract
   Input: { "file_id": "string" }

5. none
   Use ONLY when all aspects of the query are covered with high confidence.

---

## Decision Rules (Confidence & Robustness)

* **Adaptive Thresholding**: If previous tool results show [Relevance: < 0.7], treat the context as insufficient and trigger a web_search or Wikipedia fallback.
* **Step 0 Decomposition**: For "Compare X and Y" or "What is A and why is B...", generate sub-queries first.

---

## Structured Sufficiency Check (MANDATORY)

Your JSON response must follow this format:

{
  "tool": "tool_name",
  "input": { ... },
  "reason": "short explanation",
  "confidence": 0.0 to 1.0,
  "evaluation": {
    "covered_aspects": ["list of what we know"],
    "missing_aspects": ["list of what we still need to find"],
    "sufficiency_score": 0.0 to 1.0
  }
}

If "missing_aspects" is NOT empty, DO NOT choose "none". Continue searching.
`

	combinedPrompt := fmt.Sprintf("%s\n\nUser Query: %s\n%s", systemPrompt, query, contextStr)

	// Look up context window for this model
	numCtx := 8192 // safe default
	if meta := p.systemRepo.GetMetadata(modelName); meta != nil && meta.ContextWindow > 0 {
		numCtx = meta.ContextWindow
	}

	resp, err := p.client.Generate(ctx, &ollama.GenerateRequest{
		Model: modelName, Prompt: combinedPrompt, Stream: false, Format: "json",
		Options: map[string]interface{}{"num_ctx": numCtx},
	})
	if err != nil {
		return nil, 0, 0, err
	}

	cleanJSON := sCleanJSON(resp.Response)
	var step model.PlanStep
	if err := json.Unmarshal([]byte(cleanJSON), &step); err != nil {
		return nil, resp.PromptEvalCount, resp.EvalCount, fmt.Errorf("failed to parse JSON as PlanStep: %v (Response: %s)", err, resp.Response)
	}

	return &step, resp.PromptEvalCount, resp.EvalCount, nil
}

func (p *ollamaPlanner) Plan(ctx context.Context, query string, modelName string, images []string) ([]model.Task, int, int, error) {
	// Refactor to use the new Tool-Based approach but return a Tasks array for legacy compatibility
	// However, the user said "completely replace", so we might just return a single Task that triggers the loop
	// For now, I'll return a special 'tool-loop' task if other components expect Plan() to return tasks.
	// Actually, I will update Orchestrator to NOT call Plan() anymore.
	
	// Temporarily keep Plan() signature but it won't be used by the new Orchestrator.
	return nil, 0, 0, fmt.Errorf("Plan() is deprecated, use PlanNext() in a loop")
}

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
