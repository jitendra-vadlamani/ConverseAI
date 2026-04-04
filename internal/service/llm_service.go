package service

import (
	"context"
	"fmt"
	"strings"

	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type LLMService interface {
	ListModels(ctx context.Context, userID string) ([]*model.LLMInfo, error)
	AddModel(ctx context.Context, cfg *model.LLMConfig) (*model.LLMConfig, error)
	DeleteModel(ctx context.Context, id, userID string) error
}

type llmService struct {
	repo         repository.LLMRepository
	systemRepo   repository.SystemLLMRepository
	ollamaClient ollama.Client
}

func NewLLMService(repo repository.LLMRepository, systemRepo repository.SystemLLMRepository, ollamaClient ollama.Client) LLMService {
	return &llmService{
		repo:         repo,
		systemRepo:   systemRepo,
		ollamaClient: ollamaClient,
	}
}

func (s *llmService) ListModels(ctx context.Context, userID string) ([]*model.LLMInfo, error) {
	// 1. Fetch Auto-Discovered Local Ollama Models
	autoModels := s.discoverLocalOllamaModels(ctx)

	// 2. Fetch "Known" System Models from JSON
	systemModels := s.systemRepo.GetAllSystemModels()

	// 3. Fetch User-Configured Models from DB
	uID, _ := primitive.ObjectIDFromHex(userID)
	configs, err := s.repo.GetByUserID(ctx, uID)
	if err != nil {
		fmt.Printf("[LLMService] ERROR fetching DB models: %v\n", err)
	}

	var allModels []*model.LLMInfo
	matchedAuto := make(map[string]bool)

	// Process System Models (JSON)
	for _, sm := range systemModels {
		status := "Offline"
		for _, am := range autoModels {
			if am.Config.ModelName == sm.ModelName {
				status = "Online"
				matchedAuto[am.Config.ModelName] = true
				break
			}
		}
		allModels = append(allModels, &model.LLMInfo{
			Config:   sm,
			Status:   status,
			IsSystem: true,
		})
	}

	// Add any Auto-Discovered models that weren't in our system list
	for _, am := range autoModels {
		if !matchedAuto[am.Config.ModelName] {
			allModels = append(allModels, am)
		}
	}

	// Process User Models from DB
	for _, c := range configs {
		status := s.checkModelStatus(ctx, c)
		if c.ContextWindow == 0 && c.Provider == model.ProviderOllama {
			c.ContextWindow = s.fetchOllamaModelContext(ctx, c.ModelName)
		}
		allModels = append(allModels, &model.LLMInfo{
			Config:   *c,
			Status:   status,
			IsSystem: false,
		})
	}

	return allModels, nil
}

func (s *llmService) discoverLocalOllamaModels(ctx context.Context) []*model.LLMInfo {
	names, err := s.ollamaClient.Tags(ctx)
	if err != nil {
		return nil
	}

	var infos []*model.LLMInfo
	for _, name := range names {
		ctxWindow := s.fetchOllamaModelContext(ctx, name)
		infos = append(infos, &model.LLMInfo{
			Config: model.LLMConfig{
				Name:          name,
				Provider:      model.ProviderOllama,
				ModelName:     name,
				BaseURL:       s.ollamaClient.GetBaseURL(),
				ContextWindow: ctxWindow,
			},
			Status:   "Online",
			IsSystem: true,
		})
	}
	return infos
}

func (s *llmService) fetchOllamaModelContext(ctx context.Context, modelName string) int {
	details, err := s.ollamaClient.Show(ctx, modelName)
	if err != nil {
		return 2048 // Fallback
	}

	// Try modern model_info
	if infoField, ok := details["model_info"].(map[string]interface{}); ok {
		if ctxLen, ok := infoField["llama.context_length"].(float64); ok {
			return int(ctxLen)
		}
	}

	// Try legacy parameters string parsing
	if params, ok := details["parameters"].(string); ok && strings.Contains(params, "num_ctx") {
		lines := strings.Split(params, "\n")
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

	return 2048
}

func (s *llmService) checkModelStatus(ctx context.Context, cfg *model.LLMConfig) string {
	if cfg.Provider == model.ProviderOllama {
		names, err := s.ollamaClient.Tags(ctx)
		if err != nil {
			return "Offline"
		}
		for _, name := range names {
			if name == cfg.ModelName {
				return "Online"
			}
		}
		return "Missing"
	}
	return "Online" // Cloud providers assumed online
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
	existing, err := s.repo.GetByID(ctx, objID)
	if err != nil || existing == nil || existing.UserID != uID {
		return fmt.Errorf("model not found or unauthorized")
	}
	return s.repo.Delete(ctx, objID)
}
