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
	"ai-chat/internal/util"
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
	userTokenCount := util.CountTokens(prompt)
	userMsg := model.Message{
		Role:       model.RoleUser,
		Content:    prompt,
		TokenCount: userTokenCount,
		CreatedAt:  time.Now(),
	}

	// Use independent context for saving to ensure it completes even if request is cancelled
	userSaveCtx, userSaveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer userSaveCancel()

	if err := s.repo.AddMessage(userSaveCtx, conv.ID, userMsg); err != nil {
		log.Printf("[ChatService] ERROR adding user message: %v", err)
		return err
	}

	// 2. Check for Summarization Need
	maxContext := 2048
	if llmConfig.ContextWindow > 0 {
		maxContext = llmConfig.ContextWindow
	}

	// Trigger summarization if the CURRENT context (including new prompt) exceeds 90% of the limit
	if conv.TotalTokens+userTokenCount > int(float64(maxContext)*0.9) {
		log.Printf("[ChatService] Triggering summarization (Used: %d, New: %d, Limit: %d)", conv.TotalTokens, userTokenCount, maxContext)
		if err := s.triggerSummarization(tCtx, llmConfig, conv); err != nil {
			log.Printf("[ChatService] WARNING: Summarization failed: %v", err)
		} else {
			// Reload conversation to get the new summary and is_summarized flags
			conv, _ = s.repo.GetConversationByID(tCtx, conv.ID)
		}
	} else {
		// Even if not summarizing, we need to add the NEW user message to the local conv object
		// because we already saved it to the DB but the 'conv' object was loaded before that.
		conv.Messages = append(conv.Messages, userMsg)
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

	// 3. Save AI Assistant Message with Reasoning and Token Count
	aiTokenCount := util.CountTokens(fullAIResponse + fullReasoning)
	aiMsg := model.Message{
		Role:       model.RoleAssistant,
		Content:    fullAIResponse,
		Reasoning:  fullReasoning,
		TokenCount: aiTokenCount,
		CreatedAt:  time.Now(),
	}
	
	// Use Background context for saving to ensure it completes even if request is cancelled
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer saveCancel()

	if err := s.repo.AddMessage(saveCtx, conv.ID, aiMsg); err != nil {
		log.Printf("[ChatService] ERROR adding assistant message: %v", err)
		return err
	}

	// 4. Update Conversation Total Tokens
	log.Printf("[ChatService] Saving Assistant message (%d tokens) for convo %s. Sample: %.20s...", aiTokenCount, conversationID, fullAIResponse)
	return s.repo.UpdateTotalTokens(saveCtx, conv.ID, userTokenCount+aiTokenCount)
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
	
	messages := s.prepareMessages(conv)

	// Ensure we have a sane default for context window if it's somehow 0
	ctxWindow := cfg.ContextWindow
	if ctxWindow == 0 {
		ctxWindow = 2048
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":    cfg.ModelName,
		"messages": messages,
		"stream":   true,
		"options": map[string]interface{}{
			"num_ctx": ctxWindow,
		},
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

	messages := s.prepareMessages(conv)

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
func (s *chatService) prepareMessages(conv *model.Conversation) []map[string]string {
	messages := []map[string]string{}

	// 1. Add Summary if exists
	if conv.Summary != "" {
		messages = append(messages, map[string]string{
			"role":    "assistant",
			"content": "Context Summary of previous conversation parts: " + conv.Summary,
		})
	}

	// 2. Add unsummarized messages
	// Note: The latest prompt is already in conv.Messages by the time this is called
	for _, m := range conv.Messages {
		if !m.IsSummarized {
			messages = append(messages, map[string]string{"role": string(m.Role), "content": m.Content})
		}
	}

	return messages
}

func (s *chatService) triggerSummarization(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation) error {
	url := cfg.BaseURL + "/api/chat"
	if cfg.Provider == model.ProviderOpenAI {
		url = cfg.BaseURL + "/v1/chat/completions"
	}

	historyText := ""
	for _, m := range conv.Messages {
		historyText += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
	}

	prompt := fmt.Sprintf("Summarize the following conversation history for long-term memory. Be concise but cover all key points. Do NOT include any other text, just the summary itself.\n\nCONVERSATION:\n%s", historyText)

	payload := map[string]interface{}{
		"model":  cfg.ModelName,
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	reqBody, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	summary := result.Message.Content
	if len(result.Choices) > 0 {
		summary = result.Choices[0].Message.Content
	}

	if summary == "" {
		return fmt.Errorf("empty summary received")
	}

	// Persist summary and flag messages
	summaryTokens := util.CountTokens(summary)
	if err := s.repo.UpdateSummary(ctx, conv.ID, summary, summaryTokens); err != nil {
		return err
	}

	if err := s.repo.MarkMessagesAsSummarized(ctx, conv.ID); err != nil {
		return err
	}

	// Recalculate total tokens: Summary + any message NOT summarized (though usually we summarize all)
	// For now, assume all were summarized.
	return s.repo.UpdateTotalTokens(ctx, conv.ID, -(conv.TotalTokens - summaryTokens))
}
