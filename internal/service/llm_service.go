package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type LLMService interface {
	ListModels(ctx context.Context, userID string) ([]*model.LLMInfo, error)
	AddModel(ctx context.Context, cfg *model.LLMConfig) (*model.LLMConfig, error)
	DeleteModel(ctx context.Context, id, userID string) error
}

type llmService struct {
	repo       repository.LLMRepository
	httpClient *http.Client
}

func NewLLMService(repo repository.LLMRepository) LLMService {
	return &llmService{
		repo: repo,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

func (s *llmService) ListModels(ctx context.Context, userID string) ([]*model.LLMInfo, error) {
	// 1. Fetch Auto-Discovered Local Ollama Models
	autoModels := s.discoverLocalOllamaModels()

	// 2. Fetch User-Configured Models from DB
	uID, _ := primitive.ObjectIDFromHex(userID)
	configs, err := s.repo.GetByUserID(ctx, uID)
	if err != nil {
		// Log error but continue with auto-discovered models
		fmt.Printf("[LLMService] ERROR fetching DB models: %v\n", err)
	}

	var allModels []*model.LLMInfo
	allModels = append(allModels, autoModels...)

	for _, c := range configs {
		status := s.checkModelStatus(c)
		// If context window is missing in DB config, try to fetch it live
		if c.ContextWindow == 0 && c.Provider == model.ProviderOllama {
			c.ContextWindow = s.fetchOllamaModelDetails(c.BaseURL, c.ModelName)
		}
		
		allModels = append(allModels, &model.LLMInfo{
			Config:   *c,
			Status:   status,
			IsSystem: false,
		})
	}

	return allModels, nil
}

func (s *llmService) discoverLocalOllamaModels() []*model.LLMInfo {
	baseURL := "http://localhost:11434"
	url := fmt.Sprintf("%s/api/tags", baseURL)
	
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil
	}

	var infos []*model.LLMInfo
	for _, m := range tags.Models {
		ctxWindow := s.fetchOllamaModelDetails(baseURL, m.Name)
		infos = append(infos, &model.LLMInfo{
			Config: model.LLMConfig{
				Name:          m.Name,
				Provider:      model.ProviderOllama,
				ModelName:     m.Name,
				BaseURL:       baseURL,
				ContextWindow: ctxWindow,
			},
			Status:   "Online",
			IsSystem: true, // Marked as system because it's auto-discovered
		})
	}
	return infos
}

func (s *llmService) fetchOllamaModelDetails(baseURL, modelName string) int {
	url := fmt.Sprintf("%s/api/show", baseURL)
	body, _ := json.Marshal(map[string]string{"name": modelName})
	
	resp, err := s.httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return 2048 // Fallback to default
	}
	defer resp.Body.Close()

	var details struct {
		Parameters string `json:"parameters"`
		ModelInfo  map[string]interface{} `json:"model_info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return 2048
	}

	// 1. Try model_info (modern Ollama)
	if ctx, ok := details.ModelInfo["llama.context_length"].(float64); ok {
		return int(ctx)
	}

	// 2. Try parsing parameters string (older Ollama)
	if strings.Contains(details.Parameters, "num_ctx") {
		lines := strings.Split(details.Parameters, "\n")
		for _, line := range lines {
			if strings.Contains(line, "num_ctx") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					var val int
					fmt.Sscanf(fields[1], "%d", &val)
					if val > 0 {
						return val
					}
				}
			}
		}
	}

	return 2048 // Default
}

func (s *llmService) checkModelStatus(cfg *model.LLMConfig) string {
	switch cfg.Provider {
	case model.ProviderOllama:
		return s.checkOllamaModelStatus(cfg.BaseURL, cfg.ModelName)
	case model.ProviderOpenAI, model.ProviderClaude:
		return "Online" // Assume cloud is online
	default:
		if cfg.BaseURL != "" {
			return s.checkHTTPStatus(cfg.BaseURL)
		}
		return "Online"
	}
}

func (s *llmService) checkOllamaModelStatus(baseURL, modelName string) string {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	url := fmt.Sprintf("%s/api/tags", baseURL)
	
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return "Offline"
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "Offline"
	}

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "Offline"
	}

	for _, m := range tags.Models {
		if m.Name == modelName {
			return "Online"
		}
	}

	return "Missing"
}

func (s *llmService) checkHTTPStatus(url string) string {
	resp, err := s.httpClient.Get(url)
	if err != nil || resp.StatusCode >= 400 {
		return "Offline"
	}
	defer resp.Body.Close()
	return "Online"
}

func (s *llmService) AddModel(ctx context.Context, cfg *model.LLMConfig) (*model.LLMConfig, error) {
	return s.repo.Create(ctx, cfg)
}

func (s *llmService) DeleteModel(ctx context.Context, id, userID string) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid ID")
	}

	uID, _ := primitive.ObjectIDFromHex(userID)

	// 1. Verify existence and ownership
	existing, err := s.repo.GetByID(ctx, objID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("model not found")
	}

	if existing.UserID != uID {
		return fmt.Errorf("unauthorized to delete this model")
	}

	return s.repo.Delete(ctx, objID)
}
