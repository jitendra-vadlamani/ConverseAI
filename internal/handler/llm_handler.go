package handler

import (
	"encoding/json"
	"net/http"

	"ai-chat/internal/model"
	"ai-chat/internal/service"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type LLMHandler struct {
	llmService service.LLMService
}

func NewLLMHandler(llmService service.LLMService) *LLMHandler {
	return &LLMHandler{
		llmService: llmService,
	}
}

func (h *LLMHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value("userID").(string)
	models, err := h.llmService.ListModels(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
}

func (h *LLMHandler) Add(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userIDStr, _ := r.Context().Value("userID").(string)
	userID, _ := primitive.ObjectIDFromHex(userIDStr)

	var cfg model.LLMConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	cfg.UserID = userID
	newCfg, err := h.llmService.AddModel(r.Context(), &cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newCfg)
}

func (h *LLMHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing model ID", http.StatusBadRequest)
		return
	}

	userID, _ := r.Context().Value("userID").(string)

	if err := h.llmService.DeleteModel(r.Context(), id, userID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
