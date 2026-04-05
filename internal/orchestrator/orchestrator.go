package orchestrator

import (
	"context"
	"fmt"
	"time"

	"ai-chat/internal/model"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Orchestrator interface {
	Run(ctx context.Context, query string, modelName string, images []string, convID, userID primitive.ObjectID) (*model.OrchestrationResult, int, int, error)
}

type systemOrchestrator struct {
	planner   Planner
	validator Validator
	executor  Executor
}

func NewOrchestrator(planner Planner, validator Validator, executor Executor) Orchestrator {
	return &systemOrchestrator{
		planner:   planner,
		validator: validator,
		executor:  executor,
	}
}

func (o *systemOrchestrator) Run(ctx context.Context, query string, modelName string, images []string, convID, userID primitive.ObjectID) (*model.OrchestrationResult, int, int, error) {
	fmt.Printf("[Orchestrator] Starting multi-modal workflow for: %s\n", query)
	
	result := &model.OrchestrationResult{
		Query:     query,
		StartTime: time.Now(),
	}

	totalInputTokens, totalOutputTokens := 0, 0

	// 1. Plan using the dynamic model and images
	tasks, inT, outT, err := o.planner.Plan(ctx, query, modelName, images)
	totalInputTokens += inT
	totalOutputTokens += outT

	if err != nil {
		fmt.Printf("[Orchestrator] Planning failed: %v\n", err)
		result.Error = fmt.Sprintf("Planning failed: %v", err)
		result.EndTime = time.Now()
		return result, totalInputTokens, totalOutputTokens, err
	}
	result.Plan = tasks

	// 2. Validate
	if err := o.validator.Validate(ctx, tasks); err != nil {
		fmt.Printf("[Orchestrator] Validation failed: %v\n", err)
		result.Error = fmt.Sprintf("Validation failed: %v", err)
		result.EndTime = time.Now()
		return result, totalInputTokens, totalOutputTokens, err
	}

	// 3. Execute
	updatedTasks, execTokens, err := o.executor.Execute(ctx, tasks, convID, userID)
	totalOutputTokens += execTokens // Executor tokens are mostly output generation
	result.Plan = updatedTasks
	if err != nil {
		fmt.Printf("[Orchestrator] Execution failed: %v\n", err)
		result.Error = fmt.Sprintf("Execution failed: %v", err)
		result.EndTime = time.Now()
		return result, totalInputTokens, totalOutputTokens, err
	}

	// 4. Finalize
	result.Success = true
	result.EndTime = time.Now()
	fmt.Printf("[Orchestrator] Workflow completed successfully for query: %s. Total tokens: %d\n", query, totalInputTokens+totalOutputTokens)

	return result, totalInputTokens, totalOutputTokens, nil
}
