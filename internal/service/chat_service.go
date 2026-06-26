package service

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"ai-chat/internal/config"
	"ai-chat/internal/events"
	"ai-chat/internal/manager"
	"ai-chat/internal/mcp"
	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/orchestrator"
	"ai-chat/internal/repository"
	"ai-chat/internal/storage"
	"ai-chat/internal/util"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ChatService interface {
	CreateConversation(ctx context.Context, userID, title string, projectID *string) (*model.Conversation, error)
	GetConversation(ctx context.Context, id string) (*model.Conversation, error)
	ListConversations(ctx context.Context, userID string) ([]*model.Conversation, error)
	StreamCompletion(ctx context.Context, conversationID, modelName, prompt string, attachmentIDs []string, onThought func(string), onDelta func(string)) error
	UpdateConversationTitle(ctx context.Context, id string, title string) error
	GetEventStream(ctx context.Context, conversationID string) (<-chan model.ConversationEvent, error)
	GetEvents(ctx context.Context, id string) ([]model.ConversationEvent, error)
	ListModels(ctx context.Context) []model.LLMConfig
	DeleteConversation(ctx context.Context, id string) error
	DeleteConversationFile(ctx context.Context, userID, id, fileID string) error
}

type chatService struct {
	repo           repository.ChatRepository
	projectRepo    repository.ProjectRepository
	ollamaClient   ollama.Client
	modelManager   manager.ModelManager
	orchestrator   orchestrator.Orchestrator
	mcpRegistry    mcp.Registry
	storageService storage.StorageService
	eventRepo      repository.EventRepository
	eventBroker    events.EventBroker
	systemRepo     repository.SystemLLMRepository
	cfg            *config.Config
}

func NewChatService(repo repository.ChatRepository, projectRepo repository.ProjectRepository, systemRepo repository.SystemLLMRepository, ollamaClient ollama.Client, modelManager manager.ModelManager, orch orchestrator.Orchestrator, mcpRegistry mcp.Registry, storageService storage.StorageService, eventRepo repository.EventRepository, eventBroker events.EventBroker, cfg *config.Config) ChatService {
	return &chatService{
		repo:           repo,
		projectRepo:    projectRepo,
		systemRepo:     systemRepo,
		ollamaClient:   ollamaClient,
		modelManager:   modelManager,
		orchestrator:   orch,
		mcpRegistry:    mcpRegistry,
		storageService: storageService,
		eventRepo:      eventRepo,
		eventBroker:    eventBroker,
		cfg:            cfg,
	}
}

func (s *chatService) GetEventStream(ctx context.Context, conversationID string) (<-chan model.ConversationEvent, error) {
	cID, err := primitive.ObjectIDFromHex(conversationID)
	if err != nil {
		return nil, fmt.Errorf("invalid conversation ID: %w", err)
	}
	return s.eventBroker.Subscribe(cID), nil
}

func (s *chatService) CreateConversation(ctx context.Context, userID, title string, projectID *string) (*model.Conversation, error) {
	uID, _ := primitive.ObjectIDFromHex(userID)
	var projID *primitive.ObjectID
	if projectID != nil && *projectID != "" {
		pID, err := primitive.ObjectIDFromHex(*projectID)
		if err == nil {
			projID = &pID
		}
	}
	return s.repo.CreateConversation(ctx, &model.Conversation{
		UserID:    uID,
		Title:     title,
		ProjectID: projID,
	})
}

func (s *chatService) GetConversation(ctx context.Context, id string) (*model.Conversation, error) {
	objID, _ := primitive.ObjectIDFromHex(id)
	return s.repo.GetConversationByID(ctx, objID)
}

func (s *chatService) GetEvents(ctx context.Context, id string) ([]model.ConversationEvent, error) {
	objID, _ := primitive.ObjectIDFromHex(id)
	return s.eventRepo.GetEventsByConversationID(ctx, objID)
}

func (s *chatService) ListConversations(ctx context.Context, userID string) ([]*model.Conversation, error) {
	uID, _ := primitive.ObjectIDFromHex(userID)
	return s.repo.GetConversationsByUserID(ctx, uID)
}

func (s *chatService) ListModels(ctx context.Context) []model.LLMConfig {
	return s.systemRepo.GetAllSystemModels()
}

func (s *chatService) UpdateConversationTitle(ctx context.Context, id string, title string) error {
	objID, _ := primitive.ObjectIDFromHex(id)
	return s.repo.UpdateConversationTitle(ctx, objID, title)
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

	// 2. Clean up files in Storage (if no other conversation references them)
	for _, msg := range conv.Messages {
		for _, fileID := range msg.Attachments {
			// For each file in the conversation being deleted, we check if it's used elsewhere
			count, _ := s.repo.CountFileReferences(ctx, conv.UserID, fileID)
			// If count is 1, it's ONLY in this conversation (the one being deleted)
			if count <= 1 {
				if err := s.storageService.Delete(ctx, fileID); err != nil {
					log.Printf("[ChatService] Warning: Failed to delete attachment %s: %v", fileID, err)
				}
				mcpCtx := context.WithValue(ctx, mcp.UserIDKey, conv.UserID)
				mcpCtx = context.WithValue(mcpCtx, mcp.ConversationIDKey, conv.ID)
				_, err := s.mcpRegistry.CallTool(mcpCtx, "delete_document", map[string]interface{}{"file_id": fileID})
				if err != nil {
					log.Printf("[ChatService] Warning: Failed to delete RAG knowledge for %s: %v", fileID, err)
				}
			}
		}
	}

	// 3. Delete related events
	if err := s.eventRepo.DeleteEventsByConversationID(ctx, objID); err != nil {
		log.Printf("[ChatService] Warning: Failed to delete events for tracking conversation %s: %v", objID.Hex(), err)
	}

	// 4. Delete from DB
	return s.repo.DeleteConversation(ctx, objID)
}

func (s *chatService) DeleteConversationFile(ctx context.Context, userID, id, fileID string) error {
	uID, _ := primitive.ObjectIDFromHex(userID)
	convID, _ := primitive.ObjectIDFromHex(id)

	// 1. Remove reference from conversation messages
	if err := s.repo.RemoveFileFromConversation(ctx, convID, fileID); err != nil {
		return err
	}

	// 2. Check if file is still used anywhere else for this user
	count, err := s.repo.CountFileReferences(ctx, uID, fileID)
	if err != nil {
		return err
	}

	// 3. If no more references, purge from storage and RAG
	if count == 0 {
		if err := s.storageService.Delete(ctx, fileID); err != nil {
			log.Printf("[ChatService] Warning: Failed to delete binary %s: %v", fileID, err)
		}
		mcpCtx := context.WithValue(ctx, mcp.UserIDKey, uID)
		mcpCtx = context.WithValue(mcpCtx, mcp.ConversationIDKey, convID)
		_, err := s.mcpRegistry.CallTool(mcpCtx, "delete_document", map[string]interface{}{"file_id": fileID})
		if err != nil {
			log.Printf("[ChatService] Warning: Failed to delete vector info for %s: %v", fileID, err)
		}
	}

	return nil
}

func (s *chatService) emitEvent(ctx context.Context, conversationID, userID primitive.ObjectID, eventType model.EventType, payload map[string]interface{}) {
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

func (s *chatService) StreamCompletion(ctx context.Context, conversationID, modelName, prompt string, attachmentIDs []string, onThought func(string), onDelta func(string)) error {
	tCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	cID, _ := primitive.ObjectIDFromHex(conversationID)
	conv, err := s.repo.GetConversationByID(tCtx, cID)
	if err != nil {
		return err
	}

	llmConfig := s.resolveLLMConfig(tCtx, modelName)
	if llmConfig == nil {
		return fmt.Errorf("no model configuration found for %s", modelName)
	}

	var userTokenCount, aiTokenCount int

	// 1. Resolve attachments from Storage
	images, extraContext, err := s.resolveAttachments(tCtx, conv.ID, conv.UserID, attachmentIDs)
	if err != nil {
		log.Printf("[ChatService] Warning: Failed to resolve some attachments: %v", err)
	}

	// 2. RAG Semantic Search
	mcpCtx := context.WithValue(tCtx, mcp.UserIDKey, conv.UserID)
	mcpCtx = context.WithValue(mcpCtx, mcp.ConversationIDKey, conv.ID)
	
	res, err := s.mcpRegistry.CallTool(mcpCtx, "retrieve_documents", map[string]interface{}{"query": prompt})
	
	var ragResultText string
	if err == nil && res != nil {
		var sb strings.Builder
		for _, content := range res.Content {
			if content.Type == "text" {
				sb.WriteString(content.Text)
			}
		}
		ragResultText = sb.String()
	}

	count := 0
	if err != nil {
		log.Printf("[ChatService] Warning: RAG search failed: %v", err)
	} else if ragResultText != "" {
		count = strings.Count(ragResultText, "[Source:")
		extraContext += "\n### RELEVANT KNOWLEDGE FROM YOUR DOCUMENTS:\n" + ragResultText + "\n"
	}

	s.emitEvent(tCtx, conv.ID, conv.UserID, model.EventRAGSearchFinished, map[string]interface{}{
		"count": count, "message": fmt.Sprintf("Search completed. Found %d relevant snippets using %s.", count, modelName),
	})

	finalPrompt := prompt
	if extraContext != "" {
		finalPrompt = fmt.Sprintf("%s\n\n### ADDITIONAL CONTEXT:\n%s", prompt, extraContext)
	}

	// 2. Emit User Message Received Event
	s.emitEvent(context.Background(), conv.ID, conv.UserID, model.EventUserMessageReceived, map[string]interface{}{
		"prompt": prompt, "attachments": attachmentIDs, "message": "User message received.",
	})

	// 4. Handle Summarization BEFORE adding new user message
	s.handleSummarization(tCtx, llmConfig, conv)
	conv, _ = s.repo.GetConversationByID(tCtx, conv.ID) // Reload to ensure we have latest history shape

	// 5. Add User Message to History (so it NEVER gets summarized on the current turn)
	if err := s.repo.AddMessage(context.Background(), conv.ID, model.Message{
		Role: model.RoleUser, Content: finalPrompt, ModelName: modelName, Attachments: attachmentIDs, TokenCount: 0, CreatedAt: time.Now(),
	}); err != nil {
		return err
	}
	// Reload again to securely include the newly appended message into the `conv` context list which is sent to LLM
	conv, _ = s.repo.GetConversationByID(tCtx, conv.ID)

	// 5. Smart Routing: decide if we need orchestration or can go straight to chat
	//
	// The file content and RAG results are ALREADY injected into finalPrompt above,
	// so direct chat naturally handles "what's in this PDF?" style questions.
	// We only invoke the expensive LLM planner for detected multi-step queries.

	isOrchestration := needsOrchestration(prompt)
	var fullAIResponse, fullReasoning string

	if isOrchestration {
		onThought("[Orchestration] Multi-step request detected, executing reasoning agent loop...\n")

		var result *model.OrchestrationResult
		var orchErr error
		var inT, outT int
		
		result, inT, outT, orchErr = s.orchestrator.Run(tCtx, finalPrompt, llmConfig.ModelName, images, conv.ID, conv.UserID, onThought, onDelta)
		userTokenCount += inT
		aiTokenCount += outT
		err = orchErr
		
		eventPayload := map[string]interface{}{
			"success": err == nil, "message": "Orchestration workflow completed.",
		}
		if err != nil {
			eventPayload["error"] = err.Error()
			eventPayload["message"] = "Orchestration failed: " + err.Error()
		}
		s.emitEvent(context.Background(), conv.ID, conv.UserID, model.EventOrchestrationFinished, eventPayload)

		if err != nil {
			onDelta(fmt.Sprintf("\nError during orchestration: %v", err))
			fullAIResponse = fmt.Sprintf("Orchestration failed: %v", err)
		} else {
			if len(result.Plan) > 0 {
				// The final response is stored in the last task's Output
				fullAIResponse = result.Plan[len(result.Plan)-1].Output
			}
		}
	} else {
		if llmConfig.Provider == model.ProviderOllama {
			if err := s.modelManager.PrepareModel(tCtx, llmConfig.ModelName); err != nil {
				log.Printf("[ChatService] Warning: PrepareModel failed: %v", err)
			}
		}

		onThought("[Conversation] Starting standard chat...\n")
		var inputTokens, outputTokens int
		fullAIResponse, fullReasoning, inputTokens, outputTokens, err = s.processStream(tCtx, llmConfig, conv, finalPrompt, images, onThought, onDelta)
		if err != nil {
			return err
		}
		userTokenCount = inputTokens
		aiTokenCount = outputTokens
	}

	// 6. Save Assistant Message
	s.repo.AddMessage(context.Background(), conv.ID, model.Message{
		Role: model.RoleAssistant, Content: fullAIResponse, Reasoning: fullReasoning, ModelName: llmConfig.ModelName, TokenCount: aiTokenCount, CreatedAt: time.Now(),
	})
	
	// Emit Generation Event
	s.emitEvent(context.Background(), conv.ID, conv.UserID, model.EventAssistantMessageGenerated, map[string]interface{}{
		"token_count": aiTokenCount, "model": llmConfig.ModelName, "message": fmt.Sprintf("Assistant completed response using %s.", llmConfig.ModelName),
	})

	// Update User Message Token Count and Total Context
	if isOrchestration {
		// Orchestrator tokens are background processing tokens.
		// Approximate standard history context size to prevent premature summarization.
		userMsgTokens := len(finalPrompt) / 4
		aiMsgTokens := len(fullAIResponse) / 4
		s.repo.UpdateLastMessageTokenCount(context.Background(), conv.ID, userMsgTokens)
		return s.repo.UpdateTotalTokens(context.Background(), conv.ID, userMsgTokens+aiMsgTokens)
	} else {
		// Standard chat: userTokenCount represents the ENTIRE context window + new prompt
		newUserTokens := userTokenCount - conv.TotalTokens
		if newUserTokens < 0 {
			newUserTokens = len(finalPrompt) / 4
		}
		s.repo.UpdateLastMessageTokenCount(context.Background(), conv.ID, newUserTokens)
		
		// Since PromptEvalCount encompasses everything up to the user message, memory size = input + output
		return s.repo.SetTotalTokens(context.Background(), conv.ID, userTokenCount+aiTokenCount)
	}
}

func (s *chatService) resolveLLMConfig(ctx context.Context, modelName string) *model.LLMConfig {
	if modelName == "" {
		modelName = s.cfg.DefaultChatModel
	}
	
	if meta := s.systemRepo.GetMetadata(modelName); meta != nil {
		return meta
	}

	return &model.LLMConfig{
		Provider: model.ProviderOllama, ModelName: modelName, BaseURL: s.ollamaClient.GetBaseURL(), ContextWindow: 4096,
	}
}

func (s *chatService) handleSummarization(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation) {
	maxContext := cfg.ContextWindow
	if maxContext == 0 { maxContext = 4096 } // Default safety
	
	// If the history already consumes 85% of the context, summarize before sending the next one
	if conv.TotalTokens > int(float64(maxContext)*0.85) {
		log.Printf("[ChatService] Context threshold reached (%d/%d), triggering summarization\n", conv.TotalTokens, maxContext)
		s.triggerSummarization(ctx, cfg, conv)
	}
}

func (s *chatService) resolveAttachments(ctx context.Context, convID, userID primitive.ObjectID, attachmentIDs []string) ([]string, string, error) {
	var images []string
	var extraContext strings.Builder

	for _, id := range attachmentIDs {
		data, err := s.storageService.Get(ctx, id)
		if err != nil {
			log.Printf("[ChatService] Failed to get attachment %s: %v", id, err)
			continue
		}

		ext := strings.ToLower(filepath.Ext(id))
		if util.IsImage(ext) {
			images = append(images, base64.StdEncoding.EncodeToString(data))
		} else if util.IsText(ext) || ext == ".pdf" {
			textContent := ""
			if ext == ".pdf" {
				textContent, _ = util.ExtractTextFromPDF(data)
			} else {
				textContent = string(data)
			}

			// Selective Ingestion: Large files (>20KB) go to RAG
			if len(data) > 20*1024 {
				fmt.Printf("[ChatService] File %s is large (%d bytes), triggering RAG ingestion\n", id, len(data))
				go func(uID string, fID, fName, content string) {
					uObjID, _ := primitive.ObjectIDFromHex(uID)
					mcpCtx := context.WithValue(context.Background(), mcp.UserIDKey, uObjID)
					mcpCtx = context.WithValue(mcpCtx, mcp.ConversationIDKey, convID)
					
					_, err := s.mcpRegistry.CallTool(mcpCtx, "ingest_document", map[string]interface{}{
						"file_id":  fID,
						"filename": fName,
						"content":  content,
					})
					if err != nil {
						log.Printf("[ChatService] RAG Ingestion failed for %s: %v", fID, err)
					}
				}(userID.Hex(), id, id, textContent)
				
				// Add a preview of the large file to the context
				previewLen := 4000
				if len(textContent) < previewLen { previewLen = len(textContent) }
				extraContext.WriteString(fmt.Sprintf("\n(Large file '%s' indexed for semantic search. Preview: %s...)\n", id, textContent[:previewLen]))
			} else {
				// Small files are injected directly
				label := "FILE"
				if ext == ".pdf" { label = "PDF DOCUMENT" }
				extraContext.WriteString(fmt.Sprintf("\n--- %s: %s ---\n%s\n", label, id, textContent))
			}
			s.emitEvent(ctx, convID, userID, model.EventAttachmentResolved, map[string]interface{}{
				"id": id, "type": ext, "message": fmt.Sprintf("Attachment resolved: %s (%s)", id, ext),
			})
		}
	}

	return images, extraContext.String(), nil
}



func (s *chatService) processStream(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation, prompt string, images []string, onThought, onDelta func(string)) (string, string, int, int, error) {
	fullReasoning, fullAIResponse := "", ""
	isThinking := false
	inputTokens, outputTokens := 0, 0
	
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
	}, func(inT, outT int) {
		inputTokens = inT
		outputTokens = outT
	})
	return fullAIResponse, fullReasoning, inputTokens, outputTokens, err
}

func (s *chatService) callLLMStream(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation, images []string, onChunk func(string), onMeta func(int, int)) error {
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
			var line struct { 
				Message struct { Content string `json:"content"` } `json:"message"`
				Done    bool `json:"done"`
				PromptEvalCount int `json:"prompt_eval_count"`
				EvalCount       int `json:"eval_count"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &line); err == nil {
				if line.Message.Content != "" {
					onChunk(line.Message.Content)
				}
				if line.Done { 
					onMeta(line.PromptEvalCount, line.EvalCount)
					break 
				}
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported provider")
}

func (s *chatService) prepareMessages(ctx context.Context, conv *model.Conversation) []ollama.ChatMessage {
	messages := []ollama.ChatMessage{}

	// Inject Project Context as System Instruction if conversation belongs to a Project
	if conv.ProjectID != nil && s.projectRepo != nil {
		proj, err := s.projectRepo.GetByID(ctx, *conv.ProjectID)
		if err == nil && proj != nil {
			var systemPrompt strings.Builder
			systemPrompt.WriteString("You are the user's Chief of Staff and accountability partner.\n")
			systemPrompt.WriteString(fmt.Sprintf("Your client's North Star Goal is: \"%s\" (Target: %s)\n\n", proj.Title, proj.TargetDate.Format("January 2006")))
			
			systemPrompt.WriteString("PROJECT MILESTONES & TASKS:\n")
			for _, t := range proj.Tasks {
				statusText := "Pending"
				if t.Status == "completed" {
					statusText = "Completed"
				}
				systemPrompt.WriteString(fmt.Sprintf("- %s: %s (Impact: %d/10, Urgency: %d/10, Effort: %d/10)\n", t.Title, statusText, t.Impact, t.Urgency, t.Effort))
			}
			systemPrompt.WriteString("\n")
			
			systemPrompt.WriteString("USER COMPETENCY LEVELS:\n")
			for _, comp := range proj.Competencies {
				systemPrompt.WriteString(fmt.Sprintf("- %s: %d%%\n", comp.Area, comp.ProgressPercentage))
			}
			systemPrompt.WriteString("\n")
			
			systemPrompt.WriteString("LONG-TERM MEMORY (CONSTRAINTS & LESSONS):\n")
			for _, item := range proj.MemoryItems {
				systemPrompt.WriteString(fmt.Sprintf("- [%s] %s\n", strings.ToUpper(item.Category), item.Content))
			}
			systemPrompt.WriteString("\n")
			
			systemPrompt.WriteString("INSTRUCTIONS: Guide the user to take high-leverage actions. Maintain a highly professional, encouraging, yet objective and results-focused tone. Refer to their constraints (e.g. time limitations, preparation needs) and lessons learned when recommending next steps. Focus conversations on actual progress rather than busywork.")
			
			messages = append(messages, ollama.ChatMessage{
				Role:    "system",
				Content: systemPrompt.String(),
			})
		}
	}

	if conv.Summary != "" {
		messages = append(messages, ollama.ChatMessage{Role: "assistant", Content: "Context Summary: " + conv.Summary})
	}
	for _, m := range conv.Messages {
		if !m.IsSummarized {
			img, _, _ := s.resolveAttachments(ctx, conv.ID, conv.UserID, m.Attachments)
			messages = append(messages, ollama.ChatMessage{Role: string(m.Role), Content: m.Content, Images: img})
		}
	}
	return messages
}

func (s *chatService) triggerSummarization(ctx context.Context, cfg *model.LLMConfig, conv *model.Conversation) {
	historyText := ""
	for _, m := range conv.Messages { historyText += fmt.Sprintf("%s: %s\n", m.Role, m.Content) }
	prompt := fmt.Sprintf("Summarize the following conversation history for long-term memory. Be concise. Do NOT include any other text.\n\nCONVERSATION:\n%s", historyText)

	s.emitEvent(ctx, conv.ID, conv.UserID, model.EventSummarizationStarted, map[string]interface{}{"history_len": len(historyText)})
	resp, _ := s.ollamaClient.Generate(ctx, &ollama.GenerateRequest{
		Model: cfg.ModelName, Prompt: prompt, Stream: false,
		Options: map[string]interface{}{"num_ctx": cfg.ContextWindow},
	})
	if resp != nil && resp.Response != "" {
		summaryTokens := resp.EvalCount
		s.repo.UpdateSummary(ctx, conv.ID, resp.Response, summaryTokens)
		s.repo.MarkMessagesAsSummarized(ctx, conv.ID)
		s.repo.SetTotalTokens(ctx, conv.ID, summaryTokens)
		s.emitEvent(ctx, conv.ID, conv.UserID, model.EventSummarizationFinished, map[string]interface{}{"summary_tokens": summaryTokens})
	}
}

// needsOrchestration performs a lightweight keyword check to determine if a user
// prompt requires multi-step orchestration. This avoids calling the expensive
// LLM-based planner for the vast majority of messages.
//
// Returns true only when the prompt shows clear multi-step intent — i.e., it
// contains BOTH a task-type keyword AND a chaining/sequencing keyword.
func needsOrchestration(prompt string) bool {
	lower := strings.ToLower(prompt)

	// Task-type keywords — things the orchestrator can actually decompose
	taskKeywords := []string{
		"summarize", "translate", "analyze", "search for",
		"convert", "extract", "compare", "ocr",
	}

	// Chaining keywords — indicate multi-step intent
	chainKeywords := []string{
		"then", "and then", "after that", "next", "step by step",
		"first", "finally", "followed by", "and also",
	}

	hasTask := false
	for _, kw := range taskKeywords {
		if strings.Contains(lower, kw) {
			hasTask = true
			break
		}
	}

	if !hasTask {
		return false
	}

	// A single task keyword alone isn't enough — "summarize this" can be handled
	// by direct chat. We need chaining intent for orchestration.
	for _, kw := range chainKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// Special case: two or more distinct task keywords suggest multi-step
	taskCount := 0
	for _, kw := range taskKeywords {
		if strings.Contains(lower, kw) {
			taskCount++
		}
	}
	if taskCount >= 2 {
		return true
	}

	return false
}
