package handler

import (
	"encoding/json"
	"net/http"

	"ai-chat/internal/orchestrator"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type OrchestratorHandler struct {
	orchestrator orchestrator.Orchestrator
}

func NewOrchestratorHandler(orch orchestrator.Orchestrator) *OrchestratorHandler {
	return &OrchestratorHandler{
		orchestrator: orch,
	}
}

func (h *OrchestratorHandler) Orchestrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query     string `json:"query"`
		ModelName string `json:"model"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	// Fallback to gemma4:latest if no model is provided
	modelToUse := body.ModelName
	if modelToUse == "" {
		modelToUse = "gemma4:latest"
	}

	userID, _ := r.Context().Value("userID").(string)
	uID, _ := primitive.ObjectIDFromHex(userID)
	runID := primitive.NewObjectID() // Track this ad-hoc run as a "conversation"

	result, err := h.orchestrator.Run(r.Context(), body.Query, modelToUse, nil, runID, uID)
	
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
	json.NewEncoder(w).Encode(result)
}
