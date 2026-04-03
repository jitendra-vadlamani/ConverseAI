package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ChatService interface {
	CreateConversation(ctx context.Context, userID, modelConfigID, modelName, title string) (*model.Conversation, error)
	GetConversation(ctx context.Context, id string) (*model.Conversation, error)
	ListConversations(ctx context.Context, userID string) ([]*model.Conversation, error)
	StreamCompletion(ctx context.Context, conversationID, prompt string, onThought func(string), onDelta func(string)) error
	DeleteConversation(ctx context.Context, id string) error
}

type chatService struct {
	repo       repository.ChatRepository
	llmRepo    repository.LLMRepository
	httpClient *http.Client
}

func NewChatService(repo repository.ChatRepository, llmRepo repository.LLMRepository) ChatService {
	return &chatService{
		repo:       repo,
		llmRepo:    llmRepo,
		httpClient: &http.Client{},
	}
}

func (s *chatService) CreateConversation(ctx context.Context, userID, modelConfigID, modelName, title string) (*model.Conversation, error) {
	uID, _ := primitive.ObjectIDFromHex(userID)
	var mID primitive.ObjectID
	if modelConfigID != "" {
		mID, _ = primitive.ObjectIDFromHex(modelConfigID)
	}

	conv := &model.Conversation{
		UserID:        uID,
		ModelConfigID: mID,
		ModelName:     modelName,
		Title:         title,
	}
	return s.repo.CreateConversation(ctx, conv)
}

func (s *chatService) GetConversation(ctx context.Context, id string) (*model.Conversation, error) {
	objID, _ := primitive.ObjectIDFromHex(id)
	return s.repo.GetConversationByID(ctx, objID)
}

func (s *chatService) ListConversations(ctx context.Context, userID string) ([]*model.Conversation, error) {
	uID, _ := primitive.ObjectIDFromHex(userID)
	return s.repo.GetConversationsByUserID(ctx, uID)
}

func (s *chatService) DeleteConversation(ctx context.Context, id string) error {
	objID, _ := primitive.ObjectIDFromHex(id)
	return s.repo.DeleteConversation(ctx, objID)
}

func (s *chatService) StreamCompletion(ctx context.Context, conversationID, prompt string, onThought func(string), onDelta func(string)) error {
	// Add 5m default timeout (increased from 60s for local Ollama reliability)
	tCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	cID, _ := primitive.ObjectIDFromHex(conversationID)
	conv, err := s.repo.GetConversationByID(tCtx, cID)
	if err != nil {
		return err
	}

	// Resolve LLM Config
	var llmConfig *model.LLMConfig
	if !conv.ModelConfigID.IsZero() {
		llmConfig, _ = s.llmRepo.GetByID(tCtx, conv.ModelConfigID)
	}

	if llmConfig == nil && conv.ModelName != "" {
		llmConfig = &model.LLMConfig{
			Provider:  model.ProviderOllama,
			ModelName: conv.ModelName,
			BaseURL:   "http://localhost:11434",
		}
	}

	if llmConfig == nil {
		log.Printf("[ChatService] ERROR: No model configuration found for conversation %s", conversationID)
		return fmt.Errorf("no model configuration found for this conversation")
	}

	// 1. Save User Message
	userMsg := model.Message{Role: model.RoleUser, Content: prompt, CreatedAt: time.Now()}
	if err := s.repo.AddMessage(tCtx, conv.ID, userMsg); err != nil {
		return err
	}

	isThinking := false
	fullReasoning := ""
	fullAIResponse := ""
	
	// A simple streaming parser for <think> and </think>
	err = s.callLLMStream(tCtx, llmConfig, conv, prompt, func(chunk string) {
		// Detect thinking tags if they appear in chunks
		// This is a naive parser but works for most models emitting these tags
		cleanChunk := chunk
		
		if strings.Contains(chunk, "<think>") {
			isThinking = true
			parts := strings.Split(chunk, "<think>")
			if len(parts) > 1 {
				// Text before tag is content, after is reasoning
				if parts[0] != "" { onDelta(parts[0]); fullAIResponse += parts[0] }
				cleanChunk = parts[1]
			}
		} 
		
		if strings.Contains(chunk, "</think>") {
			parts := strings.Split(chunk, "</think>")
			if len(parts) > 1 {
				// Text before closing tag is reasoning, after is content
				if parts[0] != "" { onThought(parts[0]); fullReasoning += parts[0] }
				isThinking = false
				if parts[1] != "" { onDelta(parts[1]); fullAIResponse += parts[1] }
				return
			}
			isThinking = false
			return
		}

		if isThinking {
			fullReasoning += cleanChunk
			onThought(cleanChunk)
		} else {
			fullAIResponse += cleanChunk
			onDelta(cleanChunk)
		}
	})

	if err != nil {
		log.Printf("[ChatService] ERROR in callLLMStream: %v", err)
		return err
	}

	// 3. Save AI Assistant Message with Reasoning
	aiMsg := model.Message{
		Role:      model.RoleAssistant,
		Content:   fullAIResponse,
		Reasoning: fullReasoning,
		CreatedAt: time.Now(),
	}
	return s.repo.AddMessage(tCtx, conv.ID, aiMsg)
}

func (s *chatService) callLLMStream(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation, newContent string, onChunk func(string)) error {
	switch cfg.Provider {
	case model.ProviderOllama:
		return s.streamOllama(ctx, cfg, conv, newContent, onChunk)
	case model.ProviderOpenAI, model.ProviderCustom:
		return s.streamOpenAI(ctx, cfg, conv, newContent, onChunk)
	default:
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}

func (s *chatService) streamOllama(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation, content string, onChunk func(string)) error {
	url := cfg.BaseURL + "/api/chat"
	
	messages := []map[string]string{}
	for _, m := range conv.Messages {
		messages = append(messages, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	messages = append(messages, map[string]string{"role": "user", "content": content})

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":    cfg.ModelName,
		"messages": messages,
		"stream":   true,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned error: %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var line struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		onChunk(line.Message.Content)
		if line.Done {
			break
		}
	}
	return nil
}

func (s *chatService) streamOpenAI(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation, content string, onChunk func(string)) error {
	url := cfg.BaseURL + "/v1/chat/completions"
	if cfg.Provider == model.ProviderOpenAI && cfg.BaseURL == "" {
		url = "https://api.openai.com/v1/chat/completions"
	}

	messages := []map[string]string{}
	for _, m := range conv.Messages {
		messages = append(messages, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	messages = append(messages, map[string]string{"role": "user", "content": content})

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":    cfg.ModelName,
		"messages": messages,
		"stream":   true,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			onChunk(chunk.Choices[0].Delta.Content)
		}
	}
	return nil
}
