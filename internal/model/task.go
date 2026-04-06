package model

import "time"

type TaskType string

const (
	TaskSummarize TaskType = "summarize"
	TaskTranslate TaskType = "translate"
	TaskSearch    TaskType = "search"
	TaskAnalyze   TaskType = "analyze"
	TaskChat      TaskType = "chat"
)

const (
	ToolWebSearch     = "web_search"
	ToolRetrieveDocs  = "retrieve_documents"
	ToolSummarize     = "summarize"
	ToolOCRExtract    = "ocr_extract"
	ToolNone          = "none"
)

type PlanStep struct {
	Tool       string                 `json:"tool"`
	Input      map[string]interface{} `json:"input"`
	Reason     string                 `json:"reason"`
	Confidence float64                `json:"confidence"`
	Output     string                 `json:"output,omitempty"` // Added for execution trace
}

type Evidence struct {
	ID             string  `json:"id"`
	Content        string  `json:"content"`
	Source         string  `json:"source"`
	URL            string  `json:"url,omitempty"`
	RelevanceScore float64 `json:"relevance_score"`
	AuthorityScore float64 `json:"authority_score"`
	FreshnessScore float64 `json:"freshness_score"`
	FinalScore     float64 `json:"final_score"`
	IsConflicting  bool    `json:"is_conflicting"`
	ConflictReason string  `json:"conflict_reason,omitempty"`
}

type Task struct {
	ID          string    `json:"id"`
	Type        TaskType  `json:"type"`
	Model       string    `json:"model"`
	Input       string    `json:"input"`
	Output      string    `json:"output,omitempty"`
	Attachments []string  `json:"attachments,omitempty"` // IDs/Paths in Storage
	Status      string    `json:"status"` // pending, running, completed, failed
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

type TaskPlan struct {
	Query string `json:"query"`
	Tasks []Task `json:"tasks"`
}

type OrchestrationResult struct {
	Query     string `json:"query"`
	Plan      []Task `json:"plan"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}
