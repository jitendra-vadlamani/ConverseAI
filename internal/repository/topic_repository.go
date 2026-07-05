package repository

import (
	"context"
	"database/sql"
	"fmt"

	"ai-chat/internal/llm"
	"ai-chat/internal/model"
)

type TopicRepository interface {
	GetByID(ctx context.Context, id string) (*model.TopicDetail, error)
	GetTree(ctx context.Context) ([]model.TopicNode, error)
	GetRelations(ctx context.Context, id string) ([]model.TopicEdge, error)
	SaveTopic(ctx context.Context, topic *model.Topic) error
	SaveEdge(ctx context.Context, edge *model.TopicEdge) error
	SaveProgress(ctx context.Context, progress *model.Progress) error
	GetProgress(ctx context.Context, topicID string) (*model.Progress, error)
	GetLockedStatus(ctx context.Context, topicID string) (bool, []model.Topic, error)
	GetAllLeafTopicsWithProgress(ctx context.Context) ([]model.TopicDetail, error)
	UpdateTopic(ctx context.Context, id, name, description string) error
	DeleteTopic(ctx context.Context, id string) error
	GetAllTopics(ctx context.Context) ([]model.Topic, error)
	GetAllProgress(ctx context.Context) ([]model.Progress, error)
	GetAllEdges(ctx context.Context) ([]model.TopicEdge, error)
	SaveQuizAttempt(ctx context.Context, attempt *model.QuizAttempt) error
	GetQuizAttempts(ctx context.Context, topicID string) ([]model.QuizAttempt, error)
	GetStudyNotes(ctx context.Context, id string) (string, error)
	SaveStudyNotes(ctx context.Context, id string, notes string) error
	SaveChatMessage(ctx context.Context, topicID string, role string, content string) error
	GetChatMessages(ctx context.Context, topicID string) ([]llm.Message, error)
}

type PostgresTopicRepository struct {
	db *sql.DB
}

func NewTopicRepository(db *sql.DB) TopicRepository {
	query := `
	CREATE TABLE IF NOT EXISTS quiz_attempts (
		id SERIAL PRIMARY KEY,
		topic_id VARCHAR(255) REFERENCES topics(id) ON DELETE CASCADE,
		score INTEGER NOT NULL,
		questions_json TEXT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);`
	_, _ = db.Exec(query)

	alterQuery := `ALTER TABLE topics ADD COLUMN IF NOT EXISTS study_notes TEXT;`
	_, _ = db.Exec(alterQuery)

	chatTableQuery := `
	CREATE TABLE IF NOT EXISTS topic_chats (
		id SERIAL PRIMARY KEY,
		topic_id VARCHAR(255) REFERENCES topics(id) ON DELETE CASCADE,
		role VARCHAR(50) NOT NULL,
		content TEXT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);`
	_, _ = db.Exec(chatTableQuery)

	return &PostgresTopicRepository{db: db}
}

func (r *PostgresTopicRepository) GetByID(ctx context.Context, id string) (*model.TopicDetail, error) {
	query := `SELECT id, name, level, description, artifact_type, created_at, updated_at FROM topics WHERE id = $1`
	var t model.Topic
	var artType sql.NullString
	err := r.db.QueryRowContext(ctx, query, id).Scan(&t.ID, &t.Name, &t.Level, &t.Description, &artType, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get topic: %w", err)
	}
	if artType.Valid {
		t.ArtifactType = &artType.String
	}

	progress, err := r.GetProgress(ctx, id)
	if err != nil {
		return nil, err
	}

	locked, prerequisites, err := r.GetLockedStatus(ctx, id)
	if err != nil {
		return nil, err
	}

	return &model.TopicDetail{
		Topic:         t,
		Progress:      progress,
		Prerequisites: prerequisites,
		Locked:        locked,
	}, nil
}

func (r *PostgresTopicRepository) GetProgress(ctx context.Context, topicID string) (*model.Progress, error) {
	query := `SELECT topic_id, mastery_score, last_reviewed, notes, created_at, updated_at FROM progress WHERE topic_id = $1`
	var p model.Progress
	var lastRev sql.NullTime
	err := r.db.QueryRowContext(ctx, query, topicID).Scan(&p.TopicID, &p.MasteryScore, &lastRev, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get progress: %w", err)
	}
	if lastRev.Valid {
		p.LastReviewed = &lastRev.Time
	}
	return &p, nil
}

func (r *PostgresTopicRepository) GetLockedStatus(ctx context.Context, topicID string) (bool, []model.Topic, error) {
	// A topic is locked if it OR any of its parent topics has a prerequisite with mastery < 70.
	// But we also want to return the prerequisites of the topic itself.
	
	// 1. Fetch direct prerequisites of the topic itself
	directQuery := `
		SELECT t.id, t.name, t.level, t.description, t.artifact_type, t.created_at, t.updated_at, COALESCE(p.mastery_score, 0) as mastery
		FROM topic_edges e
		JOIN topics t ON e.from_id = t.id
		LEFT JOIN progress p ON t.id = p.topic_id
		WHERE e.to_id = $1 AND e.edge_type = 'prerequisite_of'
	`
	rows, err := r.db.QueryContext(ctx, directQuery, topicID)
	if err != nil {
		return false, nil, fmt.Errorf("failed to query prerequisites: %w", err)
	}
	defer rows.Close()

	prerequisites := []model.Topic{}
	locked := false

	for rows.Next() {
		var t model.Topic
		var artType sql.NullString
		var mastery int
		err := rows.Scan(&t.ID, &t.Name, &t.Level, &t.Description, &artType, &t.CreatedAt, &t.UpdatedAt, &mastery)
		if err != nil {
			return false, nil, err
		}
		if artType.Valid {
			t.ArtifactType = &artType.String
		}
		prerequisites = append(prerequisites, t)
		if mastery < 70 {
			locked = true
		}
	}

	// 2. If not already locked, check recursively if any parent/ancestor topic is locked
	if !locked {
		// Recursive CTE query to check if any ancestor topic has an unmastered prerequisite
		ancestorLockQuery := `
			SELECT COALESCE(p.mastery_score, 0) as mastery
			FROM (
				WITH RECURSIVE ancestors AS (
					-- Start with the parent(s) of the current topic
					SELECT to_id AS id FROM topic_edges WHERE from_id = $1 AND edge_type = 'part_of'
					UNION
					-- Recursively select parents of parents
					SELECT e.to_id FROM topic_edges e
					JOIN ancestors a ON e.from_id = a.id
					WHERE e.edge_type = 'part_of'
				)
				SELECT id FROM ancestors
			) a
			JOIN topic_edges e ON e.to_id = a.id
			JOIN progress p ON e.from_id = p.topic_id
			WHERE e.edge_type = 'prerequisite_of' AND COALESCE(p.mastery_score, 0) < 70
			LIMIT 1
		`
		var unmasteredPrereqMastery int
		err := r.db.QueryRowContext(ctx, ancestorLockQuery, topicID).Scan(&unmasteredPrereqMastery)
		if err != nil {
			if err != sql.ErrNoRows {
				return false, nil, fmt.Errorf("failed to query ancestor locks: %w", err)
			}
		} else {
			// Found an ancestor with an unmastered prerequisite!
			locked = true
		}
	}

	return locked, prerequisites, nil
}

func (r *PostgresTopicRepository) GetTree(ctx context.Context) ([]model.TopicNode, error) {
	query := `SELECT id, name, level, description FROM topics`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list topics for tree: %w", err)
	}
	defer rows.Close()

	topicsMap := make(map[string]*model.TopicNode)
	for rows.Next() {
		var t model.TopicNode
		if err := rows.Scan(&t.ID, &t.Name, &t.Level, &t.Description); err != nil {
			return nil, err
		}
		t.Children = []model.TopicNode{}
		topicsMap[t.ID] = &t
	}

	// Fetch part_of relations to build parent-child links
	// from_id is child, to_id is parent
	relationQuery := `SELECT from_id, to_id FROM topic_edges WHERE edge_type = 'part_of'`
	relRows, err := r.db.QueryContext(ctx, relationQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query tree edges: %w", err)
	}
	defer relRows.Close()

	childToParent := make(map[string]string)
	hasParent := make(map[string]bool)

	for relRows.Next() {
		var childID, parentID string
		if err := relRows.Scan(&childID, &parentID); err != nil {
			return nil, err
		}
		childToParent[childID] = parentID
		hasParent[childID] = true
	}

	// Construct tree by appending child pointers
	for childID, parentID := range childToParent {
		parent, parentExists := topicsMap[parentID]
		child, childExists := topicsMap[childID]
		if parentExists && childExists {
			parent.Children = append(parent.Children, *child)
		}
	}

	roots := []model.TopicNode{}
	for id, node := range topicsMap {
		if !hasParent[id] {
			roots = append(roots, *node)
		}
	}

	return roots, nil
}

func (r *PostgresTopicRepository) GetRelations(ctx context.Context, id string) ([]model.TopicEdge, error) {
	query := `SELECT from_id, to_id, edge_type FROM topic_edges WHERE from_id = $1 OR to_id = $1`
	rows, err := r.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query relations: %w", err)
	}
	defer rows.Close()

	edges := []model.TopicEdge{}
	for rows.Next() {
		var e model.TopicEdge
		if err := rows.Scan(&e.FromID, &e.ToID, &e.EdgeType); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, nil
}

func (r *PostgresTopicRepository) SaveTopic(ctx context.Context, topic *model.Topic) error {
	query := `
		INSERT INTO topics (id, name, level, description, artifact_type, updated_at)
		VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			level = EXCLUDED.level,
			description = EXCLUDED.description,
			artifact_type = EXCLUDED.artifact_type,
			updated_at = CURRENT_TIMESTAMP
	`
	var artType sql.NullString
	if topic.ArtifactType != nil {
		artType.String = *topic.ArtifactType
		artType.Valid = true
	}
	_, err := r.db.ExecContext(ctx, query, topic.ID, topic.Name, topic.Level, topic.Description, artType)
	return err
}

func (r *PostgresTopicRepository) SaveEdge(ctx context.Context, edge *model.TopicEdge) error {
	query := `
		INSERT INTO topic_edges (from_id, to_id, edge_type)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`
	_, err := r.db.ExecContext(ctx, query, edge.FromID, edge.ToID, edge.EdgeType)
	return err
}

func (r *PostgresTopicRepository) SaveProgress(ctx context.Context, progress *model.Progress) error {
	query := `
		INSERT INTO progress (topic_id, mastery_score, last_reviewed, notes, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (topic_id) DO UPDATE SET
			mastery_score = EXCLUDED.mastery_score,
			last_reviewed = CURRENT_TIMESTAMP,
			notes = EXCLUDED.notes,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := r.db.ExecContext(ctx, query, progress.TopicID, progress.MasteryScore, progress.Notes)
	return err
}

func (r *PostgresTopicRepository) GetAllLeafTopicsWithProgress(ctx context.Context) ([]model.TopicDetail, error) {
	query := `
		SELECT t.id, t.name, t.level, t.description, t.artifact_type, t.created_at, t.updated_at
		FROM topics t
		WHERE t.artifact_type IS NOT NULL
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query leaf topics: %w", err)
	}
	defer rows.Close()

	details := []model.TopicDetail{}
	for rows.Next() {
		var t model.Topic
		var artType sql.NullString
		err := rows.Scan(&t.ID, &t.Name, &t.Level, &t.Description, &artType, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if artType.Valid {
			t.ArtifactType = &artType.String
		}

		progress, err := r.GetProgress(ctx, t.ID)
		if err != nil {
			return nil, err
		}

		locked, prerequisites, err := r.GetLockedStatus(ctx, t.ID)
		if err != nil {
			return nil, err
		}

		details = append(details, model.TopicDetail{
			Topic:         t,
			Progress:      progress,
			Prerequisites: prerequisites,
			Locked:        locked,
		})
	}
	return details, nil
}

func (r *PostgresTopicRepository) UpdateTopic(ctx context.Context, id, name, description string) error {
	query := `UPDATE topics SET name = $2, description = $3, updated_at = CURRENT_TIMESTAMP WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id, name, description)
	if err != nil {
		return fmt.Errorf("failed to update topic: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("topic not found: %s", id)
	}
	return nil
}

func (r *PostgresTopicRepository) DeleteTopic(ctx context.Context, id string) error {
	// First delete child topics (recursive via edges)
	childQuery := `SELECT from_id FROM topic_edges WHERE to_id = $1 AND edge_type = 'part_of'`
	rows, err := r.db.QueryContext(ctx, childQuery, id)
	if err != nil {
		return fmt.Errorf("failed to query children: %w", err)
	}
	defer rows.Close()

	var childIDs []string
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			return err
		}
		childIDs = append(childIDs, childID)
	}

	// Recursively delete children first
	for _, childID := range childIDs {
		if err := r.DeleteTopic(ctx, childID); err != nil {
			return err
		}
	}

	// Now delete the topic itself (FK cascades handle edges and progress)
	_, err = r.db.ExecContext(ctx, `DELETE FROM topics WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete topic: %w", err)
	}
	return nil
}

func (r *PostgresTopicRepository) GetAllTopics(ctx context.Context) ([]model.Topic, error) {
	query := `SELECT id, name, level, description, artifact_type, created_at, updated_at FROM topics`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all topics: %w", err)
	}
	defer rows.Close()

	var topics []model.Topic
	for rows.Next() {
		var t model.Topic
		var artType sql.NullString
		err := rows.Scan(&t.ID, &t.Name, &t.Level, &t.Description, &artType, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if artType.Valid {
			t.ArtifactType = &artType.String
		}
		topics = append(topics, t)
	}
	return topics, nil
}

func (r *PostgresTopicRepository) GetAllProgress(ctx context.Context) ([]model.Progress, error) {
	query := `SELECT topic_id, mastery_score, last_reviewed, notes, created_at, updated_at FROM progress`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all progress: %w", err)
	}
	defer rows.Close()

	var progressList []model.Progress
	for rows.Next() {
		var p model.Progress
		var lastRev sql.NullTime
		err := rows.Scan(&p.TopicID, &p.MasteryScore, &lastRev, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if lastRev.Valid {
			p.LastReviewed = &lastRev.Time
		}
		progressList = append(progressList, p)
	}
	return progressList, nil
}

func (r *PostgresTopicRepository) GetAllEdges(ctx context.Context) ([]model.TopicEdge, error) {
	query := `SELECT from_id, to_id, edge_type FROM topic_edges`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all edges: %w", err)
	}
	defer rows.Close()

	var edges []model.TopicEdge
	for rows.Next() {
		var e model.TopicEdge
		if err := rows.Scan(&e.FromID, &e.ToID, &e.EdgeType); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, nil
}

func (r *PostgresTopicRepository) SaveQuizAttempt(ctx context.Context, attempt *model.QuizAttempt) error {
	query := `
		INSERT INTO quiz_attempts (topic_id, score, questions_json)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`
	return r.db.QueryRowContext(ctx, query, attempt.TopicID, attempt.Score, attempt.QuestionsJSON).Scan(&attempt.ID, &attempt.CreatedAt)
}

func (r *PostgresTopicRepository) GetQuizAttempts(ctx context.Context, topicID string) ([]model.QuizAttempt, error) {
	query := `
		SELECT id, topic_id, score, questions_json, created_at
		FROM quiz_attempts
		WHERE topic_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, topicID)
	if err != nil {
		return nil, fmt.Errorf("failed to get quiz attempts: %w", err)
	}
	defer rows.Close()

	var attempts []model.QuizAttempt
	for rows.Next() {
		var a model.QuizAttempt
		err := rows.Scan(&a.ID, &a.TopicID, &a.Score, &a.QuestionsJSON, &a.CreatedAt)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, a)
	}
	return attempts, nil
}

func (r *PostgresTopicRepository) GetStudyNotes(ctx context.Context, id string) (string, error) {
	query := `SELECT study_notes FROM topics WHERE id = $1`
	var notes sql.NullString
	err := r.db.QueryRowContext(ctx, query, id).Scan(&notes)
	if err != nil {
		return "", err
	}
	return notes.String, nil
}

func (r *PostgresTopicRepository) SaveStudyNotes(ctx context.Context, id string, notes string) error {
	query := `UPDATE topics SET study_notes = $2 WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id, notes)
	return err
}

func (r *PostgresTopicRepository) SaveChatMessage(ctx context.Context, topicID string, role string, content string) error {
	query := `
		INSERT INTO topic_chats (topic_id, role, content)
		VALUES ($1, $2, $3)
	`
	_, err := r.db.ExecContext(ctx, query, topicID, role, content)
	return err
}

func (r *PostgresTopicRepository) GetChatMessages(ctx context.Context, topicID string) ([]llm.Message, error) {
	query := `
		SELECT role, content
		FROM topic_chats
		WHERE topic_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, topicID)
	if err != nil {
		return nil, fmt.Errorf("failed to get chat messages: %w", err)
	}
	defer rows.Close()

	var msgs []llm.Message
	for rows.Next() {
		var m llm.Message
		err := rows.Scan(&m.Role, &m.Content)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}
