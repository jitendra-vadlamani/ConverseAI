package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"ai-chat/internal/middleware"
	"ai-chat/internal/model"
	"ai-chat/internal/repository"
	"ai-chat/internal/service"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ProjectHandler struct {
	projectRepo repository.ProjectRepository
	cosService  service.CosService
}

func NewProjectHandler(projectRepo repository.ProjectRepository, cosService service.CosService) *ProjectHandler {
	return &ProjectHandler{
		projectRepo: projectRepo,
		cosService:  cosService,
	}
}

func (h *ProjectHandler) RegisterRoutes(mux *http.ServeMux, mw *middleware.Middleware) {
	mux.HandleFunc("/api/projects", mw.JWTMiddleware(h.HandleProjects))
	mux.HandleFunc("/api/projects/get", mw.JWTMiddleware(h.GetProject))
	mux.HandleFunc("/api/projects/tasks", mw.JWTMiddleware(h.UpdateTasks))
	mux.HandleFunc("/api/projects/competencies", mw.JWTMiddleware(h.UpdateCompetencies))
	mux.HandleFunc("/api/projects/memory", mw.JWTMiddleware(h.AddMemoryItem))
	mux.HandleFunc("/api/projects/weekly-review", mw.JWTMiddleware(h.GenerateWeeklyReview))
	mux.HandleFunc("/api/projects/reality-gap", mw.JWTMiddleware(h.GetRealityGapReport))
}

func (h *ProjectHandler) HandleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.ListProjects(w, r)
	case http.MethodPost:
		h.CreateProject(w, r)
	case http.MethodDelete:
		h.DeleteProject(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	uID, _ := primitive.ObjectIDFromHex(userID)

	var req struct {
		Title      string `json:"title"`
		TargetDate string `json:"target_date"` // YYYY-MM-DD
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "Goal title is required", http.StatusBadRequest)
		return
	}

	targetDate, err := time.Parse("2006-01-02", req.TargetDate)
	if err != nil {
		targetDate = time.Now().AddDate(1, 0, 0) // Default: 1 year from now
	}

	// Step 1: Trigger LLM Goal Decomposition
	initialTasks, err := h.cosService.DecomposeGoal(r.Context(), req.Title)
	if err != nil {
		http.Error(w, "Failed to decompose goal: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 2: Create initial competency categories based on tasks
	competencies := []model.Competency{}
	seenAreas := make(map[string]bool)
	for _, t := range initialTasks {
		// Group tasks to initialize a baseline competency area
		// e.g. "Arrays & Strings" -> competency on DSA
		area := "General Knowledge"
		if t.Alignment >= 8 {
			area = t.Title
			if len(area) > 20 {
				area = area[:20] + "..."
			}
		}
		if !seenAreas[area] && len(competencies) < 5 {
			seenAreas[area] = true
			competencies = append(competencies, model.Competency{
				Area:               area,
				ProgressPercentage: 10, // baseline starts at 10%
			})
		}
	}

	// Ensure we have at least some competency track
	if len(competencies) == 0 {
		competencies = append(competencies, model.Competency{
			Area:               "Core Focus Area",
			ProgressPercentage: 10,
		})
	}

	project := &model.Project{
		ID:           primitive.NewObjectID(),
		UserID:       uID,
		Title:        req.Title,
		TargetDate:   targetDate,
		Status:       "active",
		Tasks:        initialTasks,
		Competencies: competencies,
		MemoryItems:  []model.MemoryItem{},
	}

	created, err := h.projectRepo.Create(r.Context(), project)
	if err != nil {
		http.Error(w, "Failed to store project: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	uID, _ := primitive.ObjectIDFromHex(userID)

	projects, err := h.projectRepo.ListByUserID(r.Context(), uID)
	if err != nil {
		http.Error(w, "Failed to list projects: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	uID, _ := primitive.ObjectIDFromHex(userID)

	idStr := r.URL.Query().Get("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	project, err := h.projectRepo.GetByID(r.Context(), id)
	if err != nil || project == nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// IDOR Protection
	if project.UserID != uID {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
}

func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	uID, _ := primitive.ObjectIDFromHex(userID)

	idStr := r.URL.Query().Get("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	project, err := h.projectRepo.GetByID(r.Context(), id)
	if err != nil || project == nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if project.UserID != uID {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.projectRepo.Delete(r.Context(), id); err != nil {
		http.Error(w, "Failed to delete project: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ProjectHandler) UpdateTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	uID, _ := primitive.ObjectIDFromHex(userID)

	var req struct {
		ProjectID string              `json:"project_id"`
		Tasks     []model.ProjectTask `json:"tasks"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	pID, err := primitive.ObjectIDFromHex(req.ProjectID)
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	project, err := h.projectRepo.GetByID(r.Context(), pID)
	if err != nil || project == nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if project.UserID != uID {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Update completed timestamp for changed states
	existingTasks := make(map[string]model.ProjectTask)
	for _, t := range project.Tasks {
		existingTasks[t.ID] = t
	}

	for i, t := range req.Tasks {
		if t.Status == "completed" {
			old, exists := existingTasks[t.ID]
			if !exists || old.Status != "completed" {
				now := time.Now()
				req.Tasks[i].CompletedAt = &now
			} else {
				req.Tasks[i].CompletedAt = old.CompletedAt
			}
		} else {
			req.Tasks[i].CompletedAt = nil
		}
	}

	project.Tasks = req.Tasks
	if err := h.projectRepo.Update(r.Context(), project); err != nil {
		http.Error(w, "Failed to update project tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
}

func (h *ProjectHandler) UpdateCompetencies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	uID, _ := primitive.ObjectIDFromHex(userID)

	var req struct {
		ProjectID    string             `json:"project_id"`
		Competencies []model.Competency `json:"competencies"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	pID, err := primitive.ObjectIDFromHex(req.ProjectID)
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	project, err := h.projectRepo.GetByID(r.Context(), pID)
	if err != nil || project == nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if project.UserID != uID {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	project.Competencies = req.Competencies
	if err := h.projectRepo.Update(r.Context(), project); err != nil {
		http.Error(w, "Failed to update competencies: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
}

func (h *ProjectHandler) AddMemoryItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	uID, _ := primitive.ObjectIDFromHex(userID)

	var req struct {
		ProjectID string `json:"project_id"`
		Category  string `json:"category"` // "goal", "decision", "constraint", "lesson"
		Content   string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	pID, err := primitive.ObjectIDFromHex(req.ProjectID)
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	project, err := h.projectRepo.GetByID(r.Context(), pID)
	if err != nil || project == nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if project.UserID != uID {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	project.MemoryItems = append(project.MemoryItems, model.MemoryItem{
		Category:  req.Category,
		Content:   req.Content,
		CreatedAt: time.Now(),
	})

	if err := h.projectRepo.Update(r.Context(), project); err != nil {
		http.Error(w, "Failed to save memory item: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
}

func (h *ProjectHandler) GenerateWeeklyReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	uID, _ := primitive.ObjectIDFromHex(userID)

	idStr := r.URL.Query().Get("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	project, err := h.projectRepo.GetByID(r.Context(), id)
	if err != nil || project == nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if project.UserID != uID {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	report, err := h.cosService.GenerateWeeklyReviewFeedback(r.Context(), project)
	if err != nil {
		http.Error(w, "Failed to run weekly review analysis: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"report": report})
}

func (h *ProjectHandler) GetRealityGapReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(middleware.UserIDKey).(string)
	uID, _ := primitive.ObjectIDFromHex(userID)

	idStr := r.URL.Query().Get("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	project, err := h.projectRepo.GetByID(r.Context(), id)
	if err != nil || project == nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if project.UserID != uID {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	report, err := h.cosService.AnalyzeRealityGap(r.Context(), project)
	if err != nil {
		http.Error(w, "Failed to run reality gap detection: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"report": report})
}
