package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"bytes"

	"ai-chat/internal/events"
	"ai-chat/internal/middleware"
	"ai-chat/internal/service"
	"ai-chat/internal/storage"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ChatHandler struct {
	chatService    service.ChatService
	storageService storage.StorageService
	eventBroker    events.EventBroker
}

func NewChatHandler(chatService service.ChatService, storageService storage.StorageService, eventBroker events.EventBroker) *ChatHandler {
	return &ChatHandler{
		chatService:    chatService,
		storageService: storageService,
		eventBroker:    eventBroker,
	}
}

func (h *ChatHandler) RegisterRoutes(mux *http.ServeMux, mw *middleware.Middleware) {
	mux.HandleFunc("/api/chat/conversations", mw.JWTMiddleware(h.ListConversations))
	mux.HandleFunc("/api/models", mw.JWTMiddleware(h.ListModels))
	mux.HandleFunc("/api/chat/conversations/get", mw.JWTMiddleware(h.GetConversation))
	mux.HandleFunc("/api/chat/conversations/create", mw.JWTMiddleware(h.CreateConversation))
	mux.HandleFunc("/api/chat/conversations/delete", mw.JWTMiddleware(h.DeleteConversation))
	mux.HandleFunc("/api/chat/conversations/title", mw.JWTMiddleware(h.UpdateConversationTitle))
	mux.HandleFunc("/api/chat/conversations/files", mw.JWTMiddleware(h.DeleteConversationFile))
	mux.HandleFunc("/api/chat/files/presign", mw.JWTMiddleware(h.GetFilePresignedURL))
	mux.HandleFunc("/api/chat/conversations/events", mw.JWTMiddleware(h.GetEvents))
	mux.HandleFunc("/api/chat/conversations/events/stream", mw.JWTMiddleware(h.StreamEvents))
	mux.HandleFunc("/api/chat/completions", mw.JWTMiddleware(h.StreamCompletion))
}

func (h *ChatHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		log.Printf("[ChatHandler] Unauthorized access to ListConversations")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conversations, err := h.chatService.ListConversations(r.Context(), userID)
	if err != nil {
		log.Printf("[ChatHandler] ERROR listing conversations: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conversations)
}

func (h *ChatHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing conversation ID", http.StatusBadRequest)
		return
	}

	conversation, err := h.chatService.GetConversation(r.Context(), id)
	if err != nil {
		log.Printf("[ChatHandler] ERROR getting conversation %s: %v", id, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// IDOR protection: verify conversation belongs to the authenticated user
	uID, _ := primitive.ObjectIDFromHex(userID)
	if conversation.UserID != uID {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conversation)
}

func (h *ChatHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)

	var req struct {
		Title     string  `json:"title"`
		ProjectID *string `json:"project_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ChatHandler] ERROR decoding create request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	conv, err := h.chatService.CreateConversation(r.Context(), userID, req.Title, req.ProjectID)
	if err != nil {
		log.Printf("[ChatHandler] ERROR creating conversation: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(conv)
}

func (h *ChatHandler) StreamCompletion(w http.ResponseWriter, r *http.Request) {
	// 1. Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var conversationID string
	var modelName string
	var content string
	var attachmentIDs []string

	// 2. Parse Request (JSON or Multipart)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB limit
			http.Error(w, "Error parsing multipart form", http.StatusBadRequest)
			return
		}
		conversationID = r.FormValue("conversation_id")
		modelName = r.FormValue("model_name")
		content = r.FormValue("content")

		// Handle files
		files := r.MultipartForm.File["files"]
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				continue
			}
			
			buf := new(bytes.Buffer)
			if _, err := buf.ReadFrom(file); err == nil {
				// Save to MinIO with user separation
				fileID, err := h.storageService.Save(r.Context(), userID, fileHeader.Filename, buf.Bytes())
				if err == nil {
					attachmentIDs = append(attachmentIDs, fileID)
				}
			}
			file.Close()
		}
	} else {
		var req struct {
			ConversationID string `json:"conversation_id"`
			ModelName      string `json:"model_name"`
			Content        string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[ChatHandler] ERROR decoding completion request: %v", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		conversationID = req.ConversationID
		modelName = req.ModelName
		content = req.Content
	}

	if conversationID == "" || content == "" {
		http.Error(w, "Missing conversation_id or content", http.StatusBadRequest)
		return
	}

	// 3. Start Streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Run the completion synchronously in a goroutine and wait for it to finish
	// before sending [DONE]. The callbacks write directly to the ResponseWriter.
	doneChan := make(chan error, 1)

	go func() {
		doneChan <- h.chatService.StreamCompletion(r.Context(), conversationID, modelName, content, attachmentIDs, func(thought string) {
			fmt.Fprintf(w, "event: thought\ndata: %s\n\n", thought)
			flusher.Flush()
		}, func(delta string) {
			fmt.Fprintf(w, "data: %s\n\n", delta)
			flusher.Flush()
		})
	}()

	// Wait for the completion goroutine to finish or the client to disconnect
	select {
	case err := <-doneChan:
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
			flusher.Flush()
		}
		fmt.Fprint(w, "event: end\ndata: [DONE]\n\n")
		flusher.Flush()
	case <-r.Context().Done():
		// Client disconnected
		return
	}
}

func (h *ChatHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing conversation ID", http.StatusBadRequest)
		return
	}

	// IDOR protection: verify conversation belongs to the authenticated user
	conv, err := h.chatService.GetConversation(r.Context(), id)
	if err != nil || conv == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	uID, _ := primitive.ObjectIDFromHex(userID)
	if conv.UserID != uID {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if err := h.chatService.DeleteConversation(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatHandler) UpdateConversationTitle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == "" || req.Title == "" {
		http.Error(w, "Missing id or title", http.StatusBadRequest)
		return
	}

	// IDOR protection: verify conversation belongs to the authenticated user
	conv, err := h.chatService.GetConversation(r.Context(), req.ID)
	if err != nil || conv == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	uID, _ := primitive.ObjectIDFromHex(userID)
	if conv.UserID != uID {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if err := h.chatService.UpdateConversationTitle(r.Context(), req.ID, req.Title); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatHandler) ListConversationFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing conversation ID", http.StatusBadRequest)
		return
	}

	conv, err := h.chatService.GetConversation(r.Context(), id)
	if err != nil || conv == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// IDOR protection
	uID, _ := primitive.ObjectIDFromHex(userID)
	if conv.UserID != uID {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// Collect unique attachments
	attachmentMap := make(map[string]bool)
	var attachments []string
	for _, msg := range conv.Messages {
		for _, att := range msg.Attachments {
			if !attachmentMap[att] {
				attachmentMap[att] = true
				attachments = append(attachments, att)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attachments)
}

func (h *ChatHandler) GetFilePresignedURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fileID := r.URL.Query().Get("fileID")
	if fileID == "" {
		http.Error(w, "Missing fileID", http.StatusBadRequest)
		return
	}

	// Security check: ensure file belongs to the user
	// Paths are like user-{userID}/...
	userPrefix := fmt.Sprintf("user-%s/", userID)
	if !strings.HasPrefix(fileID, userPrefix) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	url, err := h.storageService.GetPresignedURL(r.Context(), fileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": url})
}

func (h *ChatHandler) DeleteConversationFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.URL.Query().Get("id")
	fileID := r.URL.Query().Get("fileID")
	if id == "" || fileID == "" {
		http.Error(w, "Missing conversation ID or fileID", http.StatusBadRequest)
		return
	}

	// Double check ownership via file prefix
	userPrefix := fmt.Sprintf("user-%s/", userID)
	if !strings.HasPrefix(fileID, userPrefix) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.chatService.DeleteConversationFile(r.Context(), userID, id, fileID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatHandler) GetEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing conversation ID", http.StatusBadRequest)
		return
	}

	// IDOR protection: verify conversation belongs to the authenticated user
	conv, err := h.chatService.GetConversation(r.Context(), id)
	if err != nil || conv == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	uID, _ := primitive.ObjectIDFromHex(userID)
	if conv.UserID != uID {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	events, err := h.chatService.GetEvents(r.Context(), id)
	if err != nil {
		log.Printf("[ChatHandler] ERROR getting events for %s: %v", id, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func (h *ChatHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	models := h.chatService.ListModels(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
}

func (h *ChatHandler) StreamEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing conversation ID", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	stream, err := h.chatService.GetEventStream(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure we clean up the subscription when the client disconnects
	cID, _ := primitive.ObjectIDFromHex(id)
	defer h.eventBroker.Unsubscribe(cID, stream)

	for {
		select {
		case event, ok := <-stream:
			if !ok {
				return
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
