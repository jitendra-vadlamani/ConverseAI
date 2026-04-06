package orchestrator

import (
	"context"
	"fmt"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/repository"
	"ai-chat/internal/events"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Orchestrator interface {
	Run(ctx context.Context, query string, modelName string, images []string, convID, userID primitive.ObjectID) (*model.OrchestrationResult, int, int, error)
}

type systemOrchestrator struct {
	planner     Planner
	validator   Validator
	executor    Executor
	eventRepo   repository.EventRepository
	eventBroker events.EventBroker
}

func NewOrchestrator(planner Planner, validator Validator, executor Executor, eventRepo repository.EventRepository, eventBroker events.EventBroker) Orchestrator {
	return &systemOrchestrator{
		planner:     planner,
		validator:   validator,
		executor:    executor,
		eventRepo:   eventRepo,
		eventBroker: eventBroker,
	}
}

func (o *systemOrchestrator) Run(ctx context.Context, query string, modelName string, images []string, convID, userID primitive.ObjectID) (*model.OrchestrationResult, int, int, error) {
	fmt.Printf("[Orchestrator] Starting tool-based workflow for: %s\n", query)
	
	result := &model.OrchestrationResult{
		Query:     query,
		StartTime: time.Now(),
	}

	totalInputTokens, totalOutputTokens := 0, 0
	var history []model.PlanStep
	maxSteps := 5 // Practical default
	
	// Initial Event
	o.emitEvent(ctx, convID, userID, model.EventOrchestrationStarted, map[string]interface{}{
		"query": query, "model": modelName,
	})

	for i := 0; i < maxSteps; i++ {
		fmt.Printf("[Orchestrator] Step %d\n", i+1)

		// 1. Plan NEXT step
		step, inT, outT, err := o.planner.PlanNext(ctx, query, history, modelName, images)
		totalInputTokens += inT
		totalOutputTokens += outT

		if err != nil {
			fmt.Printf("[Orchestrator] Planning failed: %v\n", err)
			result.Error = fmt.Sprintf("Planning failed at step %d: %v", i+1, err)
			o.finalize(result, false)
			return result, totalInputTokens, totalOutputTokens, err
		}

		o.emitEvent(ctx, convID, userID, model.EventToolDecision, map[string]interface{}{
			"step": i + 1, "tool": step.Tool, "reason": step.Reason, "confidence": step.Confidence,
		})

		// Emit Sufficiency Evaluation Event if present
		if eval, ok := step.Input["evaluation"].(map[string]interface{}); ok {
			o.emitEvent(ctx, convID, userID, model.EventSufficiencyChecked, map[string]interface{}{
				"step": i + 1,
				"covered": eval["covered_aspects"],
				"missing": eval["missing_aspects"],
				"score": eval["sufficiency_score"],
			})
		}

		// 2. Execute
		output, inT, outT, err := o.executor.Execute(ctx, step.Tool, step.Reason, images)
		totalInputTokens += inT
		totalOutputTokens += outT
		if err != nil {
			fmt.Printf("[Orchestrator] Execution failed: %v\n", err)
			result.Error = fmt.Sprintf("Execution failed at step %d: %v", i+1, err)
			o.finalize(result, false)
			return result, totalInputTokens, totalOutputTokens, err
		}

		// Finalize
		step.Output = output
		history = append(history, *step)

		if step.Tool == model.ToolNone {
			fmt.Printf("[Orchestrator] Step %d: AI finalized. Generating grounded answer...\n", i+1)
			break
		}

		// Hard cap at maxSteps
		if i == maxSteps-1 {
			fmt.Printf("[Orchestrator] Warning: Reached max steps (%d). Generating best-effort answer...\n", maxSteps)
			break
		}
	}

	// 3. Final Grounded Generation
	o.emitEvent(ctx, convID, userID, model.EventGroundedGenerationStarted, map[string]interface{}{
		"query": query, "message": "Synthesizing evidence into the final grounded response...",
	})
	finalAnswer, inT, outT, err := o.generateGroundedAnswer(ctx, query, history, modelName)
	totalInputTokens += inT
	totalOutputTokens += outT
	if err != nil {
		fmt.Printf("[Orchestrator] Final generation failed: %v\n", err)
		result.Error = fmt.Sprintf("Final generation failed: %v", err)
		o.finalize(result, false)
		return result, totalInputTokens, totalOutputTokens, err
	}

	// For legacy support, translate history to Tasks in result
	result.Plan = make([]model.Task, len(history)+1)
	for i, h := range history {
		result.Plan[i] = model.Task{
			Type:   model.TaskType(h.Tool),
			Input:  h.Reason,
			Output: h.Output,
			Status: "completed",
		}
	}
	// Add the final grounded answer as the last task
	result.Plan[len(history)] = model.Task{
		Type:   model.TaskChat,
		Input:  "Generate final grounded response",
		Output: finalAnswer,
		Status: "completed",
	}

	o.finalize(result, true)
	o.emitEvent(ctx, convID, userID, model.EventOrchestrationFinished, map[string]interface{}{
		"success": true, "steps": len(history), "total_tokens": totalInputTokens + totalOutputTokens,
	})

	return result, totalInputTokens, totalOutputTokens, nil
}

func (o *systemOrchestrator) generateGroundedAnswer(ctx context.Context, query string, history []model.PlanStep, modelName string) (string, int, int, error) {
	var evidence strings.Builder
	for i, h := range history {
		if h.Tool != model.ToolNone && h.Output != "" {
			evidence.WriteString(fmt.Sprintf("\n--- Evidence %d (Source: %s) ---\n%s\n", i+1, h.Tool, h.Output))
		}
	}

	prompt := fmt.Sprintf(`You are a grounded assistant. Based on the following evidence collected for the query: "%s", provide a final, comprehensive answer.

## Evidence Collected:
%s

## Instructions:
1. Every claim you make MUST be followed by a citation marker like [1] or [Source: name].
2. **Prioritize high-confidence evidence**: Use "Final Score" (0.0-1.0) as a guide for reliability.
3. **Handle Conflicts**: If evidence is marked as "!! CONFLICT !!", explicitly mention the disagreement and try to synthesize the most likely truth or state why it is debated.
4. If sources are marked as "Outdated" (low Freshness), acknowledge that information might have changed.
5. Your response should be professional, clear, and strictly grounded in the provided Evidence.

Final Grounded Answer:`, query, evidence.String())

	resp, err := o.planner.(*ollamaPlanner).client.Chat(ctx, &api.ChatRequest{
		Model: modelName,
		Messages: []api.Message{
			{Role: "system", Content: "You are a professional assistant that provides grounded answers based on evidence."},
			{Role: "user", Content: prompt},
		},
		Stream: false,
	})
	if err != nil {
		return "", 0, 0, err
	}

	// Wait, ollama.Chat returns a stream if Stream: true, or a scanner if Stream: false?
	// Actually, ollama.Chat in this codebase seems to return a response body if stream:true
	// Let's use ollama.Generate for simplicity if available.
	
	genResp, err := o.planner.(*ollamaPlanner).client.Generate(ctx, &ollama.GenerateRequest{
		Model: modelName, Prompt: prompt, Stream: false,
	})
	if err != nil {
		return "", 0, 0, err
	}

	return genResp.Response, genResp.PromptEvalCount, genResp.EvalCount, nil
}

func (o *systemOrchestrator) finalize(result *model.OrchestrationResult, success bool) {
	result.Success = success
	result.EndTime = time.Now()
}

func (o *systemOrchestrator) emitEvent(ctx context.Context, conversationID, userID primitive.ObjectID, eventType model.EventType, payload map[string]interface{}) {
	event := model.ConversationEvent{
		ConversationID: conversationID,
		UserID:         userID,
		Type:           eventType,
		Payload:        payload,
		Timestamp:      time.Now(),
	}
	if err := o.eventRepo.StoreEvent(ctx, event); err != nil {
		fmt.Printf("[Orchestrator] Warning: Failed to store event %s: %v\n", eventType, err)
	}
	o.eventBroker.Publish(event)
}
