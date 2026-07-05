package model

import "time"

type Topic struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Level        int        `json:"level"`
	Description  string     `json:"description"`
	ArtifactType *string    `json:"artifact_type,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type TopicEdge struct {
	FromID   string `json:"from_id"`
	ToID     string `json:"to_id"`
	EdgeType string `json:"edge_type"` // prerequisite_of, part_of, related_to
}

type Progress struct {
	TopicID      string     `json:"topic_id"`
	MasteryScore int        `json:"mastery_score"`
	LastReviewed *time.Time `json:"last_reviewed,omitempty"`
	Notes        string     `json:"notes"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type TopicDetail struct {
	Topic
	Progress      *Progress   `json:"progress,omitempty"`
	Prerequisites []Topic     `json:"prerequisites"`
	Locked        bool        `json:"locked"`
}

type TopicNode struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Level       int         `json:"level"`
	Description string      `json:"description"`
	Children    []TopicNode `json:"children"`
}

type QuizAttempt struct {
	ID            int       `json:"id"`
	TopicID       string    `json:"topic_id"`
	Score         int       `json:"score"`
	QuestionsJSON string    `json:"questions_json"`
	CreatedAt     time.Time `json:"created_at"`
}
