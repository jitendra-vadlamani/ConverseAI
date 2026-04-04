package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"ai-chat/internal/model"
	"ai-chat/internal/repository"
)

type Validator interface {
	Validate(ctx context.Context, tasks []model.Task) error
}

type systemValidator struct {
	systemRepo repository.SystemLLMRepository
}

func NewValidator(repo repository.SystemLLMRepository) Validator {
	return &systemValidator{
		systemRepo: repo,
	}
}

func (v *systemValidator) Validate(ctx context.Context, tasks []model.Task) error {
	if len(tasks) == 0 {
		return fmt.Errorf("task plan is empty")
	}

	allowedTypes := map[model.TaskType]bool{
		model.TaskSummarize: true,
		model.TaskTranslate: true,
		model.TaskSearch:    true,
		model.TaskAnalyze:   true,
	}

	// 1. Fetch available models from system repository for reference
	availableModels := v.systemRepo.GetAllSystemModels()
	modelNames := make(map[string]bool)
	for _, m := range availableModels {
		modelNames[m.ModelName] = true
	}

	previousOutputProduced := false

	for i, t := range tasks {
		// 2. Validate Task Type
		if !allowedTypes[t.Type] {
			return fmt.Errorf("task %d: unknown task type '%s'", i+1, t.Type)
		}

		// 3. Validate Model
		// Extract model name (handles :latest suffix if provided)
		baseModel := t.Model
		if !modelNames[baseModel] && !modelNames[baseModel+":latest"] {
			return fmt.Errorf("task %d: unknown or unsupported model '%s'", i+1, t.Model)
		}

		// 4. Validate Chain Consistency
		// If input is {{PREVIOUS_OUTPUT}}, ensure there WAS a previous task
		if strings.Contains(t.Input, "{{PREVIOUS_OUTPUT}}") {
			if i == 0 {
				return fmt.Errorf("task 1: cannot use {{PREVIOUS_OUTPUT}} in the first task")
			}
			if !previousOutputProduced {
				// This shouldn't happen in a linear chain but good for future DAG support
				return fmt.Errorf("task %d: depends on output that was not produced", i+1)
			}
		}

		// Currently, every task produces output in our system
		previousOutputProduced = true
	}

	return nil
}
