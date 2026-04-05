package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"ai-chat/internal/model"
	"ai-chat/internal/repository"
)

const (
	maxTaskCount    = 10
	maxInputLength  = 50000 // 50KB
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

	if len(tasks) > maxTaskCount {
		return fmt.Errorf("task plan has %d tasks, maximum allowed is %d", len(tasks), maxTaskCount)
	}

	allowedTypes := map[model.TaskType]bool{
		model.TaskSummarize: true,
		model.TaskTranslate: true,
		model.TaskSearch:    true,
		model.TaskAnalyze:   true,
		model.TaskChat:      true,
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

		// 3. Validate Model — must be non-empty and safe
		if strings.TrimSpace(t.Model) == "" {
			return fmt.Errorf("task %d: model name is empty", i+1)
		}
		if !isSafeModelName(t.Model) {
			return fmt.Errorf("task %d: model name '%s' contains invalid characters", i+1, t.Model)
		}

		// Extract model name (handles :latest suffix if provided)
		baseModel := t.Model
		if !modelNames[baseModel] && !modelNames[baseModel+":latest"] {
			return fmt.Errorf("task %d: unknown or unsupported model '%s'", i+1, t.Model)
		}

		// 4. Validate Input length
		if len(t.Input) > maxInputLength {
			return fmt.Errorf("task %d: input is too long (%d chars, max %d)", i+1, len(t.Input), maxInputLength)
		}

		// 5. Validate Chain Consistency
		// If input is {{PREVIOUS_OUTPUT}}, ensure there WAS a previous task
		if strings.Contains(t.Input, "{{PREVIOUS_OUTPUT}}") {
			if i == 0 {
				return fmt.Errorf("task 1: cannot use {{PREVIOUS_OUTPUT}} in the first task")
			}
			if !previousOutputProduced {
				return fmt.Errorf("task %d: depends on output that was not produced", i+1)
			}
		}

		// Currently, every task produces output in our system
		previousOutputProduced = true
	}

	return nil
}

// isSafeModelName rejects model names with path traversal, shell injection, or other suspicious characters.
func isSafeModelName(name string) bool {
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' && r != ':' {
			return false
		}
	}
	return true
}
