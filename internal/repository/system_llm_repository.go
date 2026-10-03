package repository

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"ai-chat/internal/model"
)

//go:embed system_models.json
var systemModelsData []byte

type SystemLLMRepository interface {
	// GetMetadata returns nil for models that aren't in system_models.json.
	GetMetadata(modelName string) *model.LLMConfig
	GetAllSystemModels() []model.LLMConfig
}

type StaticSystemLLMRepository struct {
	models map[string]model.LLMConfig
	order  []string // file order, so the default model stays first
}

func NewSystemLLMRepository() (SystemLLMRepository, error) {
	var models []model.LLMConfig
	if err := json.Unmarshal(systemModelsData, &models); err != nil {
		return nil, fmt.Errorf("parse system_models.json: %w", err)
	}
	repo := &StaticSystemLLMRepository{models: make(map[string]model.LLMConfig, len(models))}
	for _, m := range models {
		repo.models[m.ModelName] = m
		repo.order = append(repo.order, m.ModelName)
	}
	return repo, nil
}

func (r *StaticSystemLLMRepository) GetMetadata(modelName string) *model.LLMConfig {
	if cfg, ok := r.models[modelName]; ok {
		return &cfg
	}
	return nil
}

// GetAllSystemModels lists models in file order, skipping embedding-only
// models since they can't answer messages.
func (r *StaticSystemLLMRepository) GetAllSystemModels() []model.LLMConfig {
	all := make([]model.LLMConfig, 0, len(r.models))
	for _, name := range r.order {
		m := r.models[name]
		if m.Has("embedding") && !m.Has("chat") {
			continue
		}
		all = append(all, m)
	}
	return all
}
