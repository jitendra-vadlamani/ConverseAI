package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/orchestrator"
	"ai-chat/internal/manager"
	"ai-chat/internal/repository"
	"ai-chat/internal/storage"
	"ai-chat/internal/util"
	"ai-chat/internal/events"
	"github.com/ledongthuc/pdf"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ChatService interface {
	CreateConversation(ctx context.Context, userID, modelConfigID, modelName, title string) (*model.Conversation, error)
	GetConversation(ctx context.Context, id string) (*model.Conversation, error)
	ListConversations(ctx context.Context, userID string) ([]*model.Conversation, error)
	StreamCompletion(ctx context.Context, conversationID, prompt string, attachmentIDs []string, onThought func(string), onDelta func(string)) error
	DeleteConversation(ctx context.Context, id string) error
	GetEventStream(ctx context.Context, conversationID string) (<-chan model.ConversationEvent, error)
}

type chatService struct {
	repo           repository.ChatRepository
	llmRepo        repository.LLMRepository
	ollamaClient   ollama.Client
	modelManager   manager.ModelManager
	orchestrator   orchestrator.Orchestrator
	planner        orchestrator.Planner
	storageService storage.StorageService
	eventRepo      repository.EventRepository
	eventBroker    events.EventBroker
	ragService     RagService
}

func NewChatService(repo repository.ChatRepository, llmRepo repository.LLMRepository, ollamaClient ollama.Client, modelManager manager.ModelManager, orch orchestrator.Orchestrator, planner orchestrator.Planner, storageService storage.StorageService, eventRepo repository.EventRepository, eventBroker events.EventBroker, ragService RagService) ChatService {
	return &chatService{
		repo:           repo,
		llmRepo:        llmRepo,
		ollamaClient:   ollamaClient,
		modelManager:   modelManager,
		orchestrator:   orch,
		planner:        planner,
		storageService: storageService,
		eventRepo:      eventRepo,
		eventBroker:    eventBroker,
		ragService:     ragService,
	}
}

func (s *chatService) GetEventStream(ctx context.Context, conversationID string) (<-chan model.ConversationEvent, error) {
	cID, err := primitive.ObjectIDFromHex(conversationID)
	if err != nil {
		return nil, fmt.Errorf("invalid conversation ID: %w", err)
	}
	return s.eventBroker.Subscribe(cID), nil
}

func (s *chatService) CreateConversation(ctx context.Context, userID, modelConfigID, modelName, title string) (*model.Conversation, error) {
	uID, _ := primitive.ObjectIDFromHex(userID)
	var mID primitive.ObjectID
	if modelConfigID != "" {
		mID, _ = primitive.ObjectIDFromHex(modelConfigID)
	}
	return s.repo.CreateConversation(ctx, &model.Conversation{
		UserID:        uID,
		ModelConfigID: mID,
		ModelName:     modelName,
		Title:         title,
	})
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
	
	// 1. Fetch conversation to get attachment list
	conv, err := s.repo.GetConversationByID(ctx, objID)
	if err != nil {
		return err
	}
	if conv == nil {
		return fmt.Errorf("conversation not found")
	}

	// 2. Clean up files in Storage
	for _, msg := range conv.Messages {
		for _, fileID := range msg.Attachments {
			// Clean up binary storage
			if err := s.storageService.Delete(ctx, fileID); err != nil {
				log.Printf("[ChatService] Warning: Failed to delete attachment %s: %v", fileID, err)
			}
			// Clean up vector storage
			if err := s.ragService.DeleteFileKnowledge(ctx, conv.UserID.Hex(), fileID); err != nil {
				log.Printf("[ChatService] Warning: Failed to delete RAG knowledge for %s: %v", fileID, err)
			}
		}
	}

	// 3. Delete from DB
	return s.repo.DeleteConversation(ctx, objID)
}

func (s *chatService) emitEvent(ctx context.Context, conversationID, userID primitive.ObjectID, eventType model.EventType, payload interface{}) {
	event := model.ConversationEvent{
		ConversationID: conversationID,
		UserID:         userID,
		Type:           eventType,
		Payload:        payload,
		Timestamp:      time.Now(),
	}
	if err := s.eventRepo.StoreEvent(ctx, event); err != nil {
		log.Printf("[ChatService] Warning: Failed to store event %s: %v", eventType, err)
	}
	s.eventBroker.Publish(event)
}

func (s *chatService) StreamCompletion(ctx context.Context, conversationID, prompt string, attachmentIDs []string, onThought func(string), onDelta func(string)) error {
	tCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	cID, _ := primitive.ObjectIDFromHex(conversationID)
	conv, err := s.repo.GetConversationByID(tCtx, cID)
	if err != nil {
		return err
	}

	llmConfig := s.resolveLLMConfig(tCtx, conv)
	if llmConfig == nil {
		return fmt.Errorf("no model configuration found")
	}

	// 1. Resolve attachments from Storage
	images, extraContext, err := s.resolveAttachments(tCtx, conv.UserID.Hex(), attachmentIDs)
	if err != nil {
		log.Printf("[ChatService] Warning: Failed to resolve some attachments: %v", err)
	}

	// 2. RAG Semantic Search
	ragContext, err := s.ragService.Search(tCtx, conv.UserID.Hex(), prompt, 5)
	if err != nil {
		log.Printf("[ChatService] Warning: RAG search failed: %v", err)
	}
	if len(ragContext) > 0 {
		extraContext += "\n### RELEVANT KNOWLEDGE FROM YOUR DOCUMENTS:\n" + strings.Join(ragContext, "\n---\n")
	}

	finalPrompt := prompt
	if extraContext != "" {
		finalPrompt = fmt.Sprintf("%s\n\n### ADDITIONAL CONTEXT:\n%s", prompt, extraContext)
	}

	// 2. Emit User Message Received Event
	s.emitEvent(context.Background(), conv.ID, conv.UserID, model.EventUserMessageReceived, map[string]interface{}{
		"prompt": prompt, "attachments": attachmentIDs,
	})

	// 3. Save User Message
	userTokenCount := util.CountTokens(finalPrompt)
	if err := s.repo.AddMessage(context.Background(), conv.ID, model.Message{
		Role: model.RoleUser, Content: finalPrompt, Attachments: attachmentIDs, TokenCount: userTokenCount, CreatedAt: time.Now(),
	}); err != nil {
		return err
	}

	// 4. Handle Summarization
	s.handleSummarization(tCtx, llmConfig, conv, userTokenCount)
	conv, _ = s.repo.GetConversationByID(tCtx, conv.ID) // Reload

	// 5. Planning & Orchestration Routing
	onThought("[Planning] Analyzing request and attachments...\n")
	plan, err := s.planner.Plan(tCtx, finalPrompt, llmConfig.ModelName, images)
	
	// Emit Planning Event
	s.emitEvent(context.Background(), conv.ID, conv.UserID, model.EventPlannerOutput, map[string]interface{}{
		"plan": plan, "error": err,
	})

	isOrchestration := false
	if err == nil && len(plan) > 0 {
		if len(plan) > 1 || plan[0].Type != model.TaskChat {
			isOrchestration = true
		}
	}

	var fullAIResponse, fullReasoning string

	if isOrchestration {
		onThought(fmt.Sprintf("[Orchestration] Executing %d tasks...\n", len(plan)))
		s.emitEvent(context.Background(), conv.ID, conv.UserID, model.EventOrchestrationStarted, map[string]interface{}{
			"task_count": len(plan),
		})

		result, err := s.orchestrator.Run(tCtx, finalPrompt, llmConfig.ModelName, images, conv.ID, conv.UserID)
		
		s.emitEvent(context.Background(), conv.ID, conv.UserID, model.EventOrchestrationFinished, map[string]interface{}{
			"success": err == nil, "error": err,
		})

		if err != nil {
			onDelta(fmt.Sprintf("\nError during orchestration: %v", err))
			fullAIResponse = fmt.Sprintf("Orchestration failed: %v", err)
		} else {
			var sb strings.Builder
			sb.WriteString("### Orchestrated Results\n\n")
			for _, t := range result.Plan {
				sb.WriteString(fmt.Sprintf("#### Task: %s\n%s\n\n", t.Type, t.Output))
			}
			fullAIResponse = sb.String()
			onDelta(fullAIResponse)
		}
	} else {
		if llmConfig.Provider == model.ProviderOllama {
			if err := s.modelManager.PrepareModel(tCtx, llmConfig.ModelName); err != nil {
				log.Printf("[ChatService] Warning: PrepareModel failed: %v", err)
			}
		}

		onThought("[Conversation] Starting standard chat...\n")
		fullAIResponse, fullReasoning, err = s.processStream(tCtx, llmConfig, conv, finalPrompt, images, onThought, onDelta)
		if err != nil {
			return err
		}
	}

	// 6. Save Assistant Message
	aiTokenCount := util.CountTokens(fullAIResponse + fullReasoning)
	s.repo.AddMessage(context.Background(), conv.ID, model.Message{
		Role: model.RoleAssistant, Content: fullAIResponse, Reasoning: fullReasoning, TokenCount: aiTokenCount, CreatedAt: time.Now(),
	})
	
	// Emit Generation Event
	s.emitEvent(context.Background(), conv.ID, conv.UserID, model.EventAssistantMessageGenerated, map[string]interface{}{
		"token_count": aiTokenCount,
	})

	return s.repo.UpdateTotalTokens(context.Background(), conv.ID, userTokenCount+aiTokenCount)
}

func (s *chatService) resolveLLMConfig(ctx context.Context, conv *model.Conversation) *model.LLMConfig {
	if !conv.ModelConfigID.IsZero() {
		cfg, _ := s.llmRepo.GetByID(ctx, conv.ModelConfigID)
		if cfg != nil { return cfg }
	}
	return &model.LLMConfig{
		Provider: model.ProviderOllama, ModelName: conv.ModelName, BaseURL: s.ollamaClient.GetBaseURL(),
	}
}

func (s *chatService) handleSummarization(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation, userTokenCount int) {
	maxContext := cfg.ContextWindow
	if maxContext == 0 { maxContext = 2048 }
	if conv.TotalTokens+userTokenCount > int(float64(maxContext)*0.9) {
		s.triggerSummarization(ctx, cfg, conv)
	}
}

func (s *chatService) resolveAttachments(ctx context.Context, userID string, attachmentIDs []string) ([]string, string, error) {
	var images []string
	var extraContext strings.Builder

	for _, id := range attachmentIDs {
		data, err := s.storageService.Get(ctx, id)
		if err != nil {
			log.Printf("[ChatService] Failed to get attachment %s: %v", id, err)
			continue
		}

		ext := strings.ToLower(filepath.Ext(id))
		if isImage(ext) {
			images = append(images, base64.StdEncoding.EncodeToString(data))
		} else if isText(ext) || ext == ".pdf" {
			textContent := ""
			if ext == ".pdf" {
				textContent, _ = extractTextFromPDF(data)
			} else {
				textContent = string(data)
			}

			// Selective Ingestion: Large files (>20KB) go to RAG
			if len(data) > 20*1024 {
				fmt.Printf("[ChatService] File %s is large (%d bytes), triggering RAG ingestion\n", id, len(data))
				go func(uID, fID, fName, content string) {
					err := s.ragService.Ingest(context.Background(), uID, fID, fName, content)
					if err != nil {
						log.Printf("[ChatService] RAG Ingestion failed for %s: %v", fID, err)
					}
				}(userID, id, id, textContent)
				
				extraContext.WriteString(fmt.Sprintf("\n(Large file '%s' indexed for semantic search)\n", id))
			} else {
				// Small files are injected directly
				label := "FILE"
				if ext == ".pdf" { label = "PDF DOCUMENT" }
				extraContext.WriteString(fmt.Sprintf("\n--- %s: %s ---\n%s\n", label, id, textContent))
			}
		}
	}

	return images, extraContext.String(), nil
}

func extractTextFromPDF(data []byte) (string, error) {
	r := bytes.NewReader(data)
	pdfReader, err := pdf.NewReader(r, int64(len(data)))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	for i := 1; i <= pdfReader.NumPage(); i++ {
		p := pdfReader.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
	}
	return buf.String(), nil
}

func isImage(ext string) bool {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	}
	return false
}

func isText(ext string) bool {
	switch ext {
	case ".txt", ".csv", ".json", ".md", ".go", ".py", ".js", ".ts":
		return true
	}
	return false
}

func (s *chatService) processStream(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation, prompt string, images []string, onThought, onDelta func(string)) (string, string, error) {
	fullReasoning, fullAIResponse := "", ""
	isThinking := false
	
	err := s.callLLMStream(ctx, cfg, conv, images, func(chunk string) {
		cleanChunk := chunk
		if strings.Contains(chunk, "<think>") {
			isThinking = true
			parts := strings.Split(chunk, "<think>")
			if len(parts) > 1 {
				if parts[0] != "" { onDelta(parts[0]); fullAIResponse += parts[0] }
				cleanChunk = parts[1]
			}
		} 
		if strings.Contains(chunk, "</think>") {
			parts := strings.Split(chunk, "</think>")
			if len(parts) > 1 {
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
	return fullAIResponse, fullReasoning, err
}

func (s *chatService) callLLMStream(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation, images []string, onChunk func(string)) error {
	if cfg.Provider == model.ProviderOllama {
		messages := s.prepareMessages(ctx, conv)
		// If images were passed for the CURRENT prompt, add them to the last message
		if len(images) > 0 && len(messages) > 0 && messages[len(messages)-1].Role == string(model.RoleUser) {
			messages[len(messages)-1].Images = images
		}

		resp, err := s.ollamaClient.Chat(ctx, &ollama.ChatRequest{
			Model: cfg.ModelName, Messages: messages, Stream: true,
			Options: map[string]interface{}{"num_ctx": cfg.ContextWindow},
		})
		if err != nil { return err }
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var line struct { Message struct { Content string `json:"content"` } `json:"message"`; Done bool `json:"done"` }
			if err := json.Unmarshal(scanner.Bytes(), &line); err == nil {
				onChunk(line.Message.Content)
				if line.Done { break }
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported provider")
}

func (s *chatService) prepareMessages(ctx context.Context, conv *model.Conversation) []ollama.ChatMessage {
	messages := []ollama.ChatMessage{}
	if conv.Summary != "" {
		messages = append(messages, ollama.ChatMessage{Role: "assistant", Content: "Context Summary: " + conv.Summary})
	}
	for _, m := range conv.Messages {
		if !m.IsSummarized {
			img, _, _ := s.resolveAttachments(ctx, conv.UserID.Hex(), m.Attachments)
			messages = append(messages, ollama.ChatMessage{Role: string(m.Role), Content: m.Content, Images: img})
		}
	}
	return messages
}

func (s *chatService) triggerSummarization(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation) {
	historyText := ""
	for _, m := range conv.Messages { historyText += fmt.Sprintf("%s: %s\n", m.Role, m.Content) }
	prompt := fmt.Sprintf("Summarize the following conversation history for long-term memory. Be concise. Do NOT include any other text.\n\nCONVERSATION:\n%s", historyText)

	resp, _ := s.ollamaClient.Generate(ctx, &ollama.GenerateRequest{
		Model: cfg.ModelName, Prompt: prompt, Stream: false,
	})
	if resp != nil && resp.Response != "" {
		summaryTokens := util.CountTokens(resp.Response)
		s.repo.UpdateSummary(ctx, conv.ID, resp.Response, summaryTokens)
		s.repo.MarkMessagesAsSummarized(ctx, conv.ID)
		s.repo.UpdateTotalTokens(ctx, conv.ID, -(conv.TotalTokens - summaryTokens))
	}
}
