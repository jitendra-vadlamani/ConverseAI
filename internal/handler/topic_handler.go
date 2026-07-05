package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"ai-chat/internal/llm"
	"ai-chat/internal/middleware"
	"ai-chat/internal/model"
	"ai-chat/internal/service"
)

type TopicHandler struct {
	service service.TopicService
}

func NewTopicHandler(svc service.TopicService) *TopicHandler {
	return &TopicHandler{service: svc}
}

func (h *TopicHandler) RegisterRoutes(mux *http.ServeMux, mw *middleware.Middleware) {
	mux.HandleFunc("/api/topics/get", mw.JWTMiddleware(h.GetTopic))
	mux.HandleFunc("/api/topics/relations", mw.JWTMiddleware(h.GetRelations))
	mux.HandleFunc("/api/topics/chat", mw.JWTMiddleware(h.Chat))
	mux.HandleFunc("/api/topics/progress", mw.JWTMiddleware(h.UpdateProgress))
	mux.HandleFunc("/api/topics/edit", mw.JWTMiddleware(h.EditTopic))
	mux.HandleFunc("/api/topics/delete", mw.JWTMiddleware(h.DeleteTopic))
	mux.HandleFunc("/api/topics/all_graph", mw.JWTMiddleware(h.GetFullGraph))
	mux.HandleFunc("/api/topics/quiz/generate", mw.JWTMiddleware(h.GenerateQuiz))
	mux.HandleFunc("/api/topics/quiz/submit", mw.JWTMiddleware(h.SubmitQuiz))
	mux.HandleFunc("/api/topics/quiz/attempts", mw.JWTMiddleware(h.GetQuizAttempts))
	mux.HandleFunc("/api/topics/notes", mw.JWTMiddleware(h.GetTopicNotes))
	mux.HandleFunc("/api/topics/plan", mw.JWTMiddleware(h.PlanningChat))
}

func (h *TopicHandler) GetTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tree, err := h.service.GetTree(r.Context())
	if err != nil {
		http.Error(w, "Failed to fetch topic tree: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tree)
}

func (h *TopicHandler) GetTopic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}
	detail, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to fetch topic: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if detail == nil {
		http.Error(w, "Topic not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detail)
}

func (h *TopicHandler) GetRelations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}
	relations, err := h.service.GetRelations(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to fetch relations: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(relations)
}

func (h *TopicHandler) PreviewDecomposition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}
	proposals, err := h.service.PreviewDecomposition(r.Context(), id)
	if err != nil {
		http.Error(w, "Decomposition preview failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"sub_topics": proposals})
}

func (h *TopicHandler) ConfirmDecomposition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ParentID  string                    `json:"parent_id"`
		SubTopics []service.DecomposedSubTopic `json:"sub_topics"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ParentID == "" {
		http.Error(w, "parent_id is required", http.StatusBadRequest)
		return
	}
	err := h.service.ConfirmDecomposition(r.Context(), req.ParentID, req.SubTopics)
	if err != nil {
		http.Error(w, "Decomposition confirm failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Decomposition confirmed successfully"})
}

func (h *TopicHandler) Chat(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodGet {
		msgs, err := h.service.GetChatMessages(r.Context(), id)
		if err != nil {
			http.Error(w, "Failed to get chat messages: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msgs)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		response, err := h.service.ChatWithNode(r.Context(), id, req.Messages)
		if err != nil {
			http.Error(w, "Chat failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"response": response})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *TopicHandler) UpdateProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TopicID      string `json:"topic_id"`
		MasteryScore int    `json:"mastery_score"`
		Notes        string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.TopicID == "" {
		http.Error(w, "topic_id is required", http.StatusBadRequest)
		return
	}

	err := h.service.UpdateProgress(r.Context(), req.TopicID, req.MasteryScore, req.Notes)
	if err != nil {
		http.Error(w, "Failed to update progress: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Progress updated successfully"})
}

func (h *TopicHandler) GetDailyAgenda(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	agenda, err := h.service.GetDailyAgenda(r.Context())
	if err != nil {
		http.Error(w, "Failed to fetch agenda: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agenda)
}

func (h *TopicHandler) GenerateWeeklyReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	report, err := h.service.GenerateWeeklyReview(r.Context())
	if err != nil {
		http.Error(w, "Failed to generate weekly review: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"report": report})
}

func (h *TopicHandler) PlanningChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Messages []llm.Message `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	response, graphUpdated, err := h.service.PlanningChat(r.Context(), req.Messages)
	if err != nil {
		http.Error(w, "Planning chat failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"response":      response,
		"graph_updated": graphUpdated,
	})
}

func (h *TopicHandler) EditTopic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	err := h.service.EditTopic(r.Context(), req.ID, req.Name, req.Description)
	if err != nil {
		http.Error(w, "Failed to edit topic: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Topic updated successfully"})
}

func (h *TopicHandler) DeleteTopic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	err := h.service.DeleteTopic(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to delete topic: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Topic deleted successfully"})
}

func (h *TopicHandler) GetFullGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	nodes, edges, err := h.service.GetFullGraph(r.Context())
	if err != nil {
		http.Error(w, "Failed to fetch full graph: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": nodes,
		"edges": edges,
	})
}

func (h *TopicHandler) GenerateQuiz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	quizJSON, err := h.service.GenerateQuiz(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to generate quiz: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(quizJSON))
}

func (h *TopicHandler) SubmitQuiz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TopicID       string `json:"topic_id"`
		Score         int    `json:"score"`
		QuestionsJSON string `json:"questions_json"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.TopicID == "" {
		http.Error(w, "topic_id is required", http.StatusBadRequest)
		return
	}

	attempt := &model.QuizAttempt{
		TopicID:       req.TopicID,
		Score:         req.Score,
		QuestionsJSON: req.QuestionsJSON,
	}
	err := h.service.SaveQuizAttempt(r.Context(), attempt)
	if err != nil {
		http.Error(w, "Failed to save quiz attempt: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = h.service.UpdateProgress(r.Context(), req.TopicID, req.Score, fmt.Sprintf("Completed quiz. Scored %d%%.", req.Score))
	if err != nil {
		http.Error(w, "Failed to update progress: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Quiz submitted successfully",
		"attempt": attempt,
	})
}

func (h *TopicHandler) GetQuizAttempts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	topicID := r.URL.Query().Get("id")
	if topicID == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}
	attempts, err := h.service.GetQuizAttempts(r.Context(), topicID)
	if err != nil {
		http.Error(w, "Failed to get attempts: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attempts)
}

func (h *TopicHandler) GetTopicNotes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	notes, err := h.service.GenerateNotes(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to generate notes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"notes": notes})
}
