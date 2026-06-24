package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ProjectTask struct {
	ID          string     `json:"id" bson:"id"`
	Title       string     `json:"title" bson:"title"`
	Description string     `json:"description" bson:"description"`
	Impact      int        `json:"impact" bson:"impact"`       // Score 1-10
	Urgency     int        `json:"urgency" bson:"urgency"`     // Score 1-10
	Effort      int        `json:"effort" bson:"effort"`       // Score 1-10
	Alignment   int        `json:"alignment" bson:"alignment"` // Score 1-10
	Status      string     `json:"status" bson:"status"`       // "pending", "completed"
	CompletedAt *time.Time `json:"completed_at,omitempty" bson:"completed_at,omitempty"`
	TargetDate  *time.Time `json:"target_date,omitempty" bson:"target_date,omitempty"`
}

type Competency struct {
	Area               string `json:"area" bson:"area"`                             // e.g. "Graphs", "System Design"
	ProgressPercentage int    `json:"progress_percentage" bson:"progress_percentage"` // 0-100
}

type MemoryItem struct {
	Category  string    `json:"category" bson:"category"` // "goal", "decision", "constraint", "lesson"
	Content   string    `json:"content" bson:"content"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
}

type Project struct {
	ID           primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	UserID       primitive.ObjectID `json:"user_id" bson:"user_id"`
	Title        string             `json:"title" bson:"title"` // e.g. "Become Senior Google Engineer"
	TargetDate   time.Time          `json:"target_date" bson:"target_date"`
	Status       string             `json:"status" bson:"status"` // "active", "completed"
	Tasks        []ProjectTask      `json:"tasks" bson:"tasks"`
	Competencies []Competency       `json:"competencies" bson:"competencies"`
	MemoryItems  []MemoryItem       `json:"memory_items" bson:"memory_items"`
	CreatedAt    time.Time          `json:"created_at" bson:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at" bson:"updated_at"`
}
