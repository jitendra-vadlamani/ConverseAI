package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-chat/internal/config"
	"ai-chat/internal/mcp"
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
	mcpRegistry  mcp.Registry
}

func NewPlanner(client ollama.Client, modelManager manager.ModelManager, systemRepo repository.SystemLLMRepository, cfg *config.Config, mcpRegistry mcp.Registry) Planner {
	return &ollamaPlanner{
		client:       client,
		modelManager: modelManager,
		systemRepo:   systemRepo,
		cfg:          cfg,
		mcpRegistry:  mcpRegistry,
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

	// Dynamic Tool Discovery from MCP Registry
	tools, err := p.mcpRegistry.ListTools(ctx)
	if err != nil {
		fmt.Printf("[Planner] Warning: Failed to list tools from MCP: %v\n", err)
	}

	var toolsDesc strings.Builder
	for i, t := range tools {
		schemaBytes, _ := json.Marshal(t.InputSchema)
		toolsDesc.WriteString(fmt.Sprintf("%d. %s\n   Description: %s\n   Input Schema: %s\n\n", i+1, t.Name, t.Description, string(schemaBytes)))
	}
	toolsDesc.WriteString(fmt.Sprintf("%d. none\n   Description: Use ONLY when all aspects of the query are covered with high confidence.\n   Input Schema: {}\n\n", len(tools)+1))

	systemPrompt := fmt.Sprintf(`You are a high-fidelity planning engine that decides the next action in a multi-step AI reasoning system.

Your job is to:
1. Understand the user query. 
2. If the query is complex or multi-part, decompose it into sub-questions (Step 0).
3. Decide whether to use a tool based on the current context.
4. Select the best tool from the available tools listed below.
5. Before answering ("none"), perform a Structured Sufficiency Check.

---

## Available Tools

%s
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
`, toolsDesc.String())

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
	return nil, 0, 0, fmt.Errorf("Plan() is deprecated, use PlanNext() in a loop")
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
