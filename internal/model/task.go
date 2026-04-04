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
