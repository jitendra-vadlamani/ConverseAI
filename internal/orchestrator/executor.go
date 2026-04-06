package orchestrator

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/manager"
	"ai-chat/internal/repository"
	"ai-chat/internal/events"
	"ai-chat/internal/storage"
	"ai-chat/internal/util"
	"ai-chat/internal/service"
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
	ragService     service.RagService
	searchService  service.SearchService
}

func NewExecutor(client ollama.Client, modelManager manager.ModelManager, storageService storage.StorageService, eventRepo repository.EventRepository, eventBroker events.EventBroker, systemRepo repository.SystemLLMRepository, ragService service.RagService, searchService service.SearchService) Executor {
	return &sequentialExecutor{
		client:         client,
		modelManager:   modelManager,
		storageService: storageService,
		eventRepo:      eventRepo,
		eventBroker:    eventBroker,
		systemRepo:     systemRepo,
		ragService:     ragService,
		searchService:  searchService,
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

func (e *sequentialExecutor) ExecuteStep(ctx context.Context, step *model.PlanStep, convID, userID primitive.ObjectID) (string, int, error) {
	fmt.Printf("[Executor] Executing step: %s (Reason: %s)\n", step.Tool, step.Reason)

	// Emit Tool Started Event
	e.emitEvent(ctx, convID, userID, model.EventTaskStarted, map[string]interface{}{
		"tool": step.Tool, "reason": step.Reason,
		"message": fmt.Sprintf("Starting tool: %s", step.Tool),
	})

	var output string
	var tokens int
	var err error

	switch step.Tool {
	case model.ToolWebSearch:
		output, err = e.runWebSearch(ctx, step, convID, userID)
		tokens = 0
	case model.ToolRetrieveDocs:
		output, err = e.runRetrieveDocuments(ctx, step, userID)
		tokens = 0
	case model.ToolSummarize:
		text, _ := step.Input["text"].(string)
		resp, err2 := e.runSummarize(ctx, &model.Task{Input: text, Model: e.systemRepo.GetDefaultModel()})
		if err2 == nil {
			output = resp.Response
			tokens = resp.EvalCount + resp.PromptEvalCount
		}
		err = err2
	case model.ToolOCRExtract:
		fileID, _ := step.Input["file_id"].(string)
		resp, err2 := e.runAnalyze(ctx, &model.Task{Attachments: []string{fileID}, Input: "Extract all text from this image.", Model: e.systemRepo.GetDefaultModel()})
		if err2 == nil {
			output = resp.Response
			tokens = resp.EvalCount + resp.PromptEvalCount
		}
		err = err2
	case model.ToolNone:
		output = "Done."
		tokens = 0
	default:
		err = fmt.Errorf("unsupported tool: %s", step.Tool)
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

	return output, tokens, nil
}

func (e *sequentialExecutor) runRetrieveDocuments(ctx context.Context, step *model.PlanStep, userID primitive.ObjectID) (string, error) {
	query, _ := step.Input["query"].(string)
	if query == "" {
		return "No query provided for document retrieval", nil
	}

	collectionName := fmt.Sprintf("user-knowledge-%s", userID.Hex())
	evidences, err := e.ragService.Search(ctx, collectionName, query, 5, nil)
	if err != nil {
		return "", err
	}

	if len(evidences) == 0 {
		return "No relevant documents found in local knowledge.", nil
	}

	var sb strings.Builder
	for _, ev := range evidences {
		sb.WriteString(fmt.Sprintf("[Source: %s | Relevance: %.2f]\n%s\n---\n", ev.Source, ev.RelevanceScore, ev.Content))
	}
	return sb.String(), nil
}

func (e *sequentialExecutor) runWebSearch(ctx context.Context, step *model.PlanStep, convID, userID primitive.ObjectID) (string, error) {
	query, _ := step.Input["query"].(string)
	if query == "" {
		return "No search query provided.", nil
	}

	e.emitEvent(ctx, convID, userID, model.EventSearchStarted, map[string]interface{}{
		"query": query, "message": fmt.Sprintf("Searching the web for '%s'...", query),
	})

	// 1. Initial Search (DDG with Wikipedia Fallback)
	evidences, err := e.searchService.SearchDuckDuckGo(ctx, query)
	if err != nil || len(evidences) == 0 {
		fmt.Printf("[Executor] DDG failed or empty, trying Wikipedia for query: %s\n", query)
		evidences, err = e.searchService.SearchWikipedia(ctx, query)
	}

	if err != nil {
		e.emitEvent(ctx, convID, userID, model.EventSearchFinished, map[string]interface{}{
			"success": false, "error": err.Error(), "message": "Global search failed.",
		})
		return "", fmt.Errorf("search failed: %w", err)
	}
	
	e.emitEvent(ctx, convID, userID, model.EventSearchFinished, map[string]interface{}{
		"success": true, "count": len(evidences), "message": fmt.Sprintf("Found %d search results.", len(evidences)),
	})

	if len(evidences) == 0 {
		return "No search results found.", nil
	}

	// 2. Parallel Page Extraction (Top 3)
	limit := 3
	if len(evidences) < limit {
		limit = len(evidences)
	}

	e.emitEvent(ctx, convID, userID, model.EventExtractionStarted, map[string]interface{}{
		"limit": limit, "message": fmt.Sprintf("Extracting content from top %d sources...", limit),
	})

	tempCollection := fmt.Sprintf("temp-search-%s", convID.Hex())
	_ = e.ragService.DeleteCollection(ctx, tempCollection)

	type pageResult struct {
		id      string
		content string
		source  string
		err     error
	}
	resChan := make(chan pageResult, limit)

	for i := 0; i < limit; i++ {
		go func(ev model.Evidence, idx int) {
			content, err := e.searchService.FetchPageContent(ctx, ev.URL)
			resChan <- pageResult{id: fmt.Sprintf("web-%d", idx), content: content, source: ev.URL, err: err}
		}(evidences[i], i)
	}

	extractedCount := 0
	for i := 0; i < limit; i++ {
		res := <-resChan
		if res.err == nil && res.content != "" {
			_ = e.ragService.Ingest(ctx, tempCollection, res.id, res.source, res.content)
			extractedCount++
		}
	}
	
	e.emitEvent(ctx, convID, userID, model.EventExtractionFinished, map[string]interface{}{
		"count": extractedCount, "message": fmt.Sprintf("Successfully extracted content from %d pages.", extractedCount),
	})

	// 3. RAG Ranking from extracted content
	rankedResults, err := e.ragService.Search(ctx, tempCollection, query, 5, nil)
	if err != nil {
		return "Search completed but failed to rank results.", nil
	}

	if len(rankedResults) == 0 {
		// Fallback to snippets if extraction/ranking failed
		var sb strings.Builder
		sb.WriteString("Retrieved snippets (Full page extraction failed):\n")
		for _, ev := range evidences {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", ev.Source, ev.Content))
		}
		return sb.String(), nil
	}

	// 5. Cluster and Detect Contradictions (Phase 2 - Non-blocking)
	// We use a shorter timeout for fact-checking to ensure it doesn't block the system
	checkCtx, checkCancel := context.WithTimeout(ctx, 15*time.Second)
	defer checkCancel()

	clusters, _ := e.ragService.ClusterEvidence(checkCtx, rankedResults)
	for _, cluster := range clusters {
		if len(cluster) > 1 {
			e.detectContradiction(checkCtx, cluster)
		}
	}

	// 6. Calculate Final Scores and Sort
	for i := range rankedResults {
		ev := &rankedResults[i]
		// Formula: Relevance(60%) + Authority(20%) + Freshness(20%)
		ev.FinalScore = (ev.RelevanceScore * 0.6) + (ev.AuthorityScore * 0.2) + (ev.FreshnessScore * 0.2)
		if ev.IsConflicting {
			ev.FinalScore *= 0.5 // Heavy penalty for conflicts
		}
	}

	sort.Slice(rankedResults, func(i, j int) bool {
		return rankedResults[i].FinalScore > rankedResults[j].FinalScore
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Top verified information for '%s':\n", query))
	for _, ev := range rankedResults {
		if ev.IsConflicting {
			sb.WriteString(fmt.Sprintf("[Source: %s | Final Score: %.2f | !! CONFLICT !!: %s]\n%s\n---\n", ev.Source, ev.FinalScore, ev.ConflictReason, ev.Content))
		} else {
			sb.WriteString(fmt.Sprintf("[Source: %s | Final Score: %.2f | Authority: %.1f | Freshness: %.1f]\n%s\n---\n", ev.Source, ev.FinalScore, ev.AuthorityScore, ev.FreshnessScore, ev.Content))
		}
	}

	return sb.String(), nil
}

func (e *sequentialExecutor) detectContradiction(ctx context.Context, cluster []model.Evidence) {
	if len(cluster) < 2 {
		return
	}

	// Prepare text for analysis
	var sb strings.Builder
	for i, ev := range cluster {
		fmt.Fprintf(&sb, "Source [%d]: %s\nContent: %s\n\n", i, ev.Source, ev.Content)
	}

	prompt := fmt.Sprintf(`Analyze these %d pieces of evidence. Do they agree on the facts, or is there a contradiction?
Reply with EXACTLY "AGREE" or "CONTRADICT" on the first line, followed by a one-sentence reason on the second line.

Evidence:
%s`, len(cluster), sb.String())

	resp, err := e.client.Generate(ctx, &ollama.GenerateRequest{
		Model:  e.systemRepo.GetDefaultModel(),
		Prompt: prompt,
		System: "You are a professional fact-checker. Be strict. Only flag as CONTRADICT if they actively disagree on a quantifiable or qualitative claim.",
	})
	if err != nil {
		return
	}

	lines := strings.Split(strings.TrimSpace(resp.Response), "\n")
	if len(lines) > 0 && strings.ToUpper(lines[0]) == "CONTRADICT" {
		reason := "Known conflict between sources."
		if len(lines) > 1 {
			reason = lines[1]
		}
		for i := range cluster {
			cluster[i].IsConflicting = true
			cluster[i].ConflictReason = reason
		}
	}
}

func (s *sequentialExecutor) getNumCtx(modelName string) int {
	if meta := s.systemRepo.GetMetadata(modelName); meta != nil && meta.ContextWindow > 0 {
		return meta.ContextWindow
	}
	return 8192 // safe default
}

func (e *sequentialExecutor) emitEvent(ctx context.Context, conversationID, userID primitive.ObjectID, eventType model.EventType, payload map[string]interface{}) {
	event := model.ConversationEvent{
		ConversationID: conversationID,
		UserID:         userID,
		Type:           eventType,
		Payload:        payload,
		Timestamp:      time.Now(),
	}
	e.eventBroker.Publish(event)
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
