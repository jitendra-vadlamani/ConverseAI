package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"ai-chat/internal/service"
)

type ChatHandler struct {
	chatService service.ChatService
}

func NewChatHandler(chatService service.ChatService) *ChatHandler {
	return &ChatHandler{
		chatService: chatService,
	}
}

func (h *ChatHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value("userID").(string)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conversation)
}

func (h *ChatHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value("userID").(string)

	var req struct {
		ModelConfigID string `json:"model_config_id"`
		ModelName     string `json:"model_name"`
		Title         string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ChatHandler] ERROR decoding create request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	conv, err := h.chatService.CreateConversation(r.Context(), userID, req.ModelConfigID, req.ModelName, req.Title)
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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// 2. Decode initial request
	var req struct {
		ConversationID string `json:"conversation_id"`
		Content        string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ChatHandler] ERROR decoding completion request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 3. Start Streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	out := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		err := h.chatService.StreamCompletion(r.Context(), req.ConversationID, req.Content, func(thought string) {
			fmt.Fprintf(w, "event: thought\ndata: %s\n\n", thought)
			flusher.Flush()
		}, func(delta string) {
			fmt.Fprintf(w, "data: %s\n\n", delta)
			flusher.Flush()
		})
		if err != nil {
			errChan <- err
		}
		close(out)
	}()

	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				fmt.Fprint(w, "event: end\ndata: [DONE]\n\n")
				flusher.Flush()
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		case err := <-errChan:
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
			flusher.Flush()
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (h *ChatHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if err := h.chatService.DeleteConversation(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
