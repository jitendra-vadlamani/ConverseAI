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
	GetMetadata(modelName string) *model.LLMConfig
	GetAllSystemModels() []model.LLMConfig
}

type StaticSystemLLMRepository struct {
	models map[string]model.LLMConfig
}

func NewSystemLLMRepository() SystemLLMRepository {
	repo := &StaticSystemLLMRepository{
		models: make(map[string]model.LLMConfig),
	}

	var models []struct {
		ModelName       string   `json:"model_name"`
		Name            string   `json:"name"`
		ContextWindow   int      `json:"context_window"`
		Description     string   `json:"description"`
		Architecture    string   `json:"architecture,omitempty"`
		ParametersCount string   `json:"parameters_count,omitempty"`
		EmbeddingLength int      `json:"embedding_length,omitempty"`
		Quantization    string   `json:"quantization,omitempty"`
		Capabilities    []string `json:"capabilities,omitempty"`
		Temperature     float64  `json:"temperature,omitempty"`
		TopK            int      `json:"top_k,omitempty"`
		TopP            float64  `json:"top_p,omitempty"`
		RepeatPenalty   float64  `json:"repeat_penalty,omitempty"`
		StopSequences   []string `json:"stop_sequences,omitempty"`
	}

	if err := json.Unmarshal(systemModelsData, &models); err != nil {
		fmt.Printf("[SystemLLMRepository] ERROR: Failed to load system models: %v\n", err)
		return repo
	}

	for _, m := range models {
		repo.models[m.ModelName] = model.LLMConfig{
			ModelName:       m.ModelName,
			Name:            m.Name,
			Provider:        model.ProviderOllama,
			ContextWindow:   m.ContextWindow,
			Description:     m.Description,
			Architecture:    m.Architecture,
			ParametersCount: m.ParametersCount,
			EmbeddingLength: m.EmbeddingLength,
			Quantization:    m.Quantization,
			Capabilities:    m.Capabilities,
			Temperature:     m.Temperature,
			TopK:            m.TopK,
			TopP:            m.TopP,
			RepeatPenalty:   m.RepeatPenalty,
			StopSequences:   m.StopSequences,
		}
	}

	return repo
}

func (r *StaticSystemLLMRepository) GetMetadata(modelName string) *model.LLMConfig {
	if cfg, ok := r.models[modelName]; ok {
		return &cfg
	}
	return nil
}

func (r *StaticSystemLLMRepository) GetAllSystemModels() []model.LLMConfig {
	var all []model.LLMConfig
	for _, m := range r.models {
		all = append(all, m)
	}
	return all
}
