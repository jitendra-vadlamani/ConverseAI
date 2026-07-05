package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"ai-chat/internal/config"
	"ai-chat/internal/llm"
	"ai-chat/internal/model"
	"ai-chat/internal/repository"
)

type TopicService interface {
	GetByID(ctx context.Context, id string) (*model.TopicDetail, error)
	GetTree(ctx context.Context) ([]model.TopicNode, error)
	GetRelations(ctx context.Context, id string) ([]model.TopicEdge, error)
	PreviewDecomposition(ctx context.Context, id string) ([]DecomposedSubTopic, error)
	ConfirmDecomposition(ctx context.Context, parentID string, subTopics []DecomposedSubTopic) error
	ChatWithNode(ctx context.Context, id string, messages []llm.Message) (string, error)
	GetDailyAgenda(ctx context.Context) ([]model.TopicDetail, error)
	GenerateWeeklyReview(ctx context.Context) (string, error)
	UpdateProgress(ctx context.Context, topicID string, score int, notes string) error
	PlanningChat(ctx context.Context, messages []llm.Message) (string, bool, error)
	EditTopic(ctx context.Context, id, name, description string) error
	DeleteTopic(ctx context.Context, id string) error
	GetFullGraph(ctx context.Context) ([]model.TopicDetail, []model.TopicEdge, error)
	GenerateQuiz(ctx context.Context, id string) (string, error)
	SaveQuizAttempt(ctx context.Context, attempt *model.QuizAttempt) error
	GetQuizAttempts(ctx context.Context, topicID string) ([]model.QuizAttempt, error)
	GenerateNotes(ctx context.Context, id string) (string, error)
	GetChatMessages(ctx context.Context, topicID string) ([]llm.Message, error)
	SaveChatMessage(ctx context.Context, topicID string, role string, content string) error
}

type topicService struct {
	repo        repository.TopicRepository
	llmProvider llm.Provider
	cfg         *config.Config
}

func NewTopicService(repo repository.TopicRepository, llmProvider llm.Provider, cfg *config.Config) TopicService {
	return &topicService{
		repo:        repo,
		llmProvider: llmProvider,
		cfg:         cfg,
	}
}

func (s *topicService) GetByID(ctx context.Context, id string) (*model.TopicDetail, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *topicService) GetTree(ctx context.Context) ([]model.TopicNode, error) {
	return s.repo.GetTree(ctx)
}

func (s *topicService) GetRelations(ctx context.Context, id string) ([]model.TopicEdge, error) {
	return s.repo.GetRelations(ctx, id)
}

type DecomposedSubTopic struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	ArtifactType  string   `json:"artifact_type"`
	Prerequisites []string `json:"prerequisites"`
}

type decompositionResponse struct {
	SubTopics []DecomposedSubTopic `json:"sub_topics"`
}

// PreviewDecomposition asks the LLM to suggest sub-topics but does NOT save them.
// Returns the proposals for user review.
func (s *topicService) PreviewDecomposition(ctx context.Context, id string) ([]DecomposedSubTopic, error) {
	detail, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, fmt.Errorf("topic not found: %s", id)
	}

	prompt := fmt.Sprintf(`You are the Chief of Staff AI.
Break down the topic "%s" (Description: "%s") into a list of 3 to 5 logical sub-topics or execution milestones.
Assign appropriate ID values in kebab-case prefixed with "t_".
Optionally, link them to each other using the prerequisites array (referencing the IDs you generate, or the parent ID "%s" if it's a direct prerequisite).

You MUST respond strictly with a JSON object of this exact schema:
{
  "sub_topics": [
    {
      "id": "t_subarray-problems",
      "name": "Subarray Problems",
      "description": "Techniques involving subarrays like sliding window or prefix sum.",
      "artifact_type": "practice_problem",
      "prerequisites": ["%s"]
    }
  ]
}
Do not write any other markdown text, only raw JSON.`, detail.Name, detail.Description, id, id)

	resp, err := s.llmProvider.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM error during decomposition: %w", err)
	}

	var parsedResp decompositionResponse
	if err := json.Unmarshal([]byte(resp.Message.Content), &parsedResp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM decomposition response: %w (Raw: %s)", err, resp.Message.Content)
	}

	return parsedResp.SubTopics, nil
}

// ConfirmDecomposition saves only the user-approved sub-topics.
func (s *topicService) ConfirmDecomposition(ctx context.Context, parentID string, subTopics []DecomposedSubTopic) error {
	detail, err := s.repo.GetByID(ctx, parentID)
	if err != nil {
		return err
	}
	if detail == nil {
		return fmt.Errorf("parent topic not found: %s", parentID)
	}

	for _, st := range subTopics {
		subTopic := model.Topic{
			ID:           st.ID,
			Name:         st.Name,
			Level:        detail.Level + 1,
			Description:  st.Description,
			ArtifactType: &st.ArtifactType,
		}
		if err := s.repo.SaveTopic(ctx, &subTopic); err != nil {
			return fmt.Errorf("failed to save sub-topic %s: %w", st.ID, err)
		}

		partOfEdge := model.TopicEdge{
			FromID:   st.ID,
			ToID:     parentID,
			EdgeType: "part_of",
		}
		if err := s.repo.SaveEdge(ctx, &partOfEdge); err != nil {
			return fmt.Errorf("failed to save part_of edge: %w", err)
		}

		for _, prereqID := range st.Prerequisites {
			prereqEdge := model.TopicEdge{
				FromID:   prereqID,
				ToID:     st.ID,
				EdgeType: "prerequisite_of",
			}
			if err := s.repo.SaveEdge(ctx, &prereqEdge); err != nil {
				return fmt.Errorf("failed to save prerequisite edge: %w", err)
			}
		}
	}

	return nil
}

func (s *topicService) ChatWithNode(ctx context.Context, id string, messages []llm.Message) (string, error) {
	detail, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if detail == nil {
		return "", fmt.Errorf("topic not found: %s", id)
	}

	mastery := 0
	notes := "None"
	if detail.Progress != nil {
		mastery = detail.Progress.MasteryScore
		notes = detail.Progress.Notes
	}

	systemPrompt := fmt.Sprintf(`You are an expert technical tutor. The user is studying the topic "%s".
Description: %s
Level: %d
User's current mastery score: %d/100
Study notes: %s

Help the user understand this topic. You can ask guiding questions, explain concepts, or present brief exercises. Keep your responses focused directly on this topic.`,
		detail.Name, detail.Description, detail.Level, mastery, notes)

	chatMessages := append([]llm.Message{
		{Role: "system", Content: systemPrompt},
	}, messages...)

	resp, err := s.llmProvider.Chat(ctx, llm.ChatRequest{
		Messages: chatMessages,
	})
	if err != nil {
		return "", err
	}

	// Save the user's latest message and assistant response
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.Role == "user" {
			_ = s.repo.SaveChatMessage(ctx, id, "user", lastMsg.Content)
		}
	}
	_ = s.repo.SaveChatMessage(ctx, id, "assistant", resp.Message.Content)

	return resp.Message.Content, nil
}

func (s *topicService) GetDailyAgenda(ctx context.Context) ([]model.TopicDetail, error) {
	leafs, err := s.repo.GetAllLeafTopicsWithProgress(ctx)
	if err != nil {
		return nil, err
	}

	agenda := []model.TopicDetail{}
	now := time.Now()

	type scoredTopic struct {
		detail model.TopicDetail
		score  float64
	}
	var scoredList []scoredTopic

	for _, t := range leafs {
		// Filter out locked topics
		if t.Locked {
			continue
		}

		mastery := 0
		daysSinceReview := 30.0 // Default high value for unreviewed items
		if t.Progress != nil {
			mastery = t.Progress.MasteryScore
			if t.Progress.LastReviewed != nil {
				daysSinceReview = now.Sub(*t.Progress.LastReviewed).Hours() / 24.0
			}
		}

		// IPS = (100 - mastery) + daysSinceReview
		ips := float64(100-mastery) + daysSinceReview
		scoredList = append(scoredList, scoredTopic{detail: t, score: ips})
	}

	// Sort scored list by IPS descending
	for i := 0; i < len(scoredList); i++ {
		for j := i + 1; j < len(scoredList); j++ {
			if scoredList[i].score < scoredList[j].score {
				scoredList[i], scoredList[j] = scoredList[j], scoredList[i]
			}
		}
	}

	// Take top 3
	limit := 3
	if len(scoredList) < limit {
		limit = len(scoredList)
	}

	for i := 0; i < limit; i++ {
		agenda = append(agenda, scoredList[i].detail)
	}

	return agenda, nil
}

func (s *topicService) GenerateWeeklyReview(ctx context.Context) (string, error) {
	leafs, err := s.repo.GetAllLeafTopicsWithProgress(ctx)
	if err != nil {
		return "", err
	}

	completedList := ""
	needsReviewList := ""
	notStartedList := ""

	for _, t := range leafs {
		mastery := 0
		if t.Progress != nil {
			mastery = t.Progress.MasteryScore
		}
		itemStr := fmt.Sprintf("- %s (Mastery: %d/100, Locked: %t)\n", t.Name, mastery, t.Locked)
		if mastery >= 70 {
			completedList += itemStr
		} else if mastery > 0 {
			needsReviewList += itemStr
		} else {
			notStartedList += itemStr
		}
	}

	prompt := fmt.Sprintf(`You are the Chief of Staff AI running a Weekly Topic Mastery Review.
Evaluate the user's current progress in their study topics and provide an executive summary:
1. **Mastery Status**: Critique their mastery rate (topics with score >= 70).
2. **Current Bottlenecks**: Identify locked topics and which prerequisite topics are holding them back.
3. **Strategic Recommendations**: Suggest exactly which topics they should prioritize next week to unlock maximum nodes.

Completed/Mastered Topics:
%s

In Progress Topics:
%s

Not Started Topics:
%s

Write in an encouraging, highly objective, and strategic tone. Use clean markdown.`,
		completedList, needsReviewList, notStartedList)

	resp, err := s.llmProvider.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", err
	}

	return resp.Message.Content, nil
}

func (s *topicService) UpdateProgress(ctx context.Context, topicID string, score int, notes string) error {
	score = int(math.Min(math.Max(float64(score), 0), 100))
	return s.repo.SaveProgress(ctx, &model.Progress{
		TopicID:      topicID,
		MasteryScore: score,
		Notes:        notes,
	})
}

func (s *topicService) EditTopic(ctx context.Context, id, name, description string) error {
	return s.repo.UpdateTopic(ctx, id, name, description)
}

func (s *topicService) DeleteTopic(ctx context.Context, id string) error {
	return s.repo.DeleteTopic(ctx, id)
}

// --- Planning Chat: LLM-driven conversational graph creation ---

type planningOperation struct {
	Op          string `json:"op"`           // create_topic, create_edge
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Level       int    `json:"level,omitempty"`
	FromID      string `json:"from_id,omitempty"`
	ToID        string `json:"to_id,omitempty"`
	EdgeType    string `json:"edge_type,omitempty"`
}

type planningActionBlock struct {
	Operations []planningOperation `json:"operations"`
}

func serializeTree(nodes []model.TopicNode, indent string) string {
	var sb strings.Builder
	for _, n := range nodes {
		sb.WriteString(fmt.Sprintf("%s- %s (id: %s, level: %d)\n", indent, n.Name, n.ID, n.Level))
		if len(n.Children) > 0 {
			sb.WriteString(serializeTree(n.Children, indent+"  "))
		}
	}
	return sb.String()
}

func (s *topicService) PlanningChat(ctx context.Context, messages []llm.Message) (string, bool, error) {
	// Get current graph for context
	tree, err := s.repo.GetTree(ctx)
	if err != nil {
		return "", false, err
	}

	treeText := "(empty graph — no topics yet)"
	if len(tree) > 0 {
		treeText = serializeTree(tree, "")
	}

	systemPrompt := fmt.Sprintf(`You are the Chief of Staff AI, a personal learning planner.
The user is telling you what they want to learn. Your job is to:
1. Respond conversationally — confirm what you're creating, explain your reasoning
2. Create the appropriate topics, sub-topics, and prerequisite links in the knowledge graph

Current Knowledge Graph:
%s

When you need to modify the graph, include an action block in your response using these exact markers:
|||ACTION|||
{"operations": [
  {"op": "create_topic", "id": "t_topic_id", "name": "Topic Name", "description": "...", "level": 0},
  {"op": "create_edge", "from_id": "t_child", "to_id": "t_parent", "edge_type": "part_of"},
  {"op": "create_edge", "from_id": "t_prereq", "to_id": "t_dependent", "edge_type": "prerequisite_of"}
]}
|||END|||

Rules for action blocks:
- Topic IDs must be lowercase with underscores, prefixed with "t_" (e.g., t_kubernetes, t_k8s_pods)
- Root goals have level 0, direct children level 1, grandchildren level 2, etc.
- Use "part_of" edges for parent-child hierarchy (from_id=child, to_id=parent)
- Use "prerequisite_of" edges for dependencies (from_id=prerequisite, to_id=dependent)
- You can reference existing topic IDs from the current graph to link new topics to existing ones
- If the user's request is just a question or doesn't require graph changes, respond without an action block
- Always explain what you're creating in your conversational response
- Generate leaf-level sub-topics (the actual study items) with practical descriptions`, treeText)

	chatMessages := append([]llm.Message{
		{Role: "system", Content: systemPrompt},
	}, messages...)

	resp, err := s.llmProvider.Chat(ctx, llm.ChatRequest{
		Messages: chatMessages,
	})
	if err != nil {
		return "", false, fmt.Errorf("LLM error in planning chat: %w", err)
	}

	responseText := resp.Message.Content
	graphUpdated := false

	// Parse and execute action blocks
	if idx := strings.Index(responseText, "|||ACTION|||"); idx != -1 {
		endIdx := strings.Index(responseText, "|||END|||")
		if endIdx > idx {
			actionJSON := strings.TrimSpace(responseText[idx+len("|||ACTION|||") : endIdx])

			var actionBlock planningActionBlock
			if err := json.Unmarshal([]byte(actionJSON), &actionBlock); err != nil {
				log.Printf("Warning: Failed to parse planning action block: %v (raw: %s)", err, actionJSON)
			} else {
				// Execute operations in order
				for _, op := range actionBlock.Operations {
					switch op.Op {
					case "create_topic":
						topic := &model.Topic{
							ID:          op.ID,
							Name:        op.Name,
							Description: op.Description,
							Level:       op.Level,
						}
						if err := s.repo.SaveTopic(ctx, topic); err != nil {
							log.Printf("Warning: Failed to create topic %s: %v", op.ID, err)
						} else {
							graphUpdated = true
						}
					case "create_edge":
						edge := &model.TopicEdge{
							FromID:   op.FromID,
							ToID:     op.ToID,
							EdgeType: op.EdgeType,
						}
						if err := s.repo.SaveEdge(ctx, edge); err != nil {
							log.Printf("Warning: Failed to create edge %s->%s: %v", op.FromID, op.ToID, err)
						} else {
							graphUpdated = true
						}
					}
				}
			}

			// Strip action block from the user-visible response
			responseText = strings.TrimSpace(responseText[:idx] + responseText[endIdx+len("|||END|||"):])
		}
	}

	return responseText, graphUpdated, nil
}

func (s *topicService) GetFullGraph(ctx context.Context) ([]model.TopicDetail, []model.TopicEdge, error) {
	topics, err := s.repo.GetAllTopics(ctx)
	if err != nil {
		return nil, nil, err
	}

	progressList, err := s.repo.GetAllProgress(ctx)
	if err != nil {
		return nil, nil, err
	}

	edges, err := s.repo.GetAllEdges(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Map topic_id -> model.Topic
	topicMap := make(map[string]model.Topic)
	for _, t := range topics {
		topicMap[t.ID] = t
	}

	// Map topic_id -> *model.Progress
	progressMap := make(map[string]*model.Progress)
	for i := range progressList {
		p := progressList[i]
		progressMap[p.TopicID] = &p
	}

	// Map topic_id -> list of prerequisite topics
	prereqMap := make(map[string][]model.Topic)
	// Map topic_id -> parent_id (part_of edges)
	parentMap := make(map[string]string)
	for _, e := range edges {
		if e.EdgeType == "prerequisite_of" {
			prereqTopic, exists := topicMap[e.FromID]
			if exists {
				prereqMap[e.ToID] = append(prereqMap[e.ToID], prereqTopic)
			}
		} else if e.EdgeType == "part_of" {
			// from_id is child, to_id is parent
			parentMap[e.FromID] = e.ToID
		}
	}

	// Memoized recursive function to compute full locked status
	resolvedLocked := make(map[string]bool)
	var isNodeLocked func(id string) bool
	isNodeLocked = func(id string) bool {
		if val, exists := resolvedLocked[id]; exists {
			return val
		}

		// Check direct prerequisites
		locked := false
		prereqs := prereqMap[id]
		for _, pTopic := range prereqs {
			mastery := 0
			if pProg, hasProg := progressMap[pTopic.ID]; hasProg {
				mastery = pProg.MasteryScore
			}
			if mastery < 70 || isNodeLocked(pTopic.ID) {
				locked = true
				break
			}
		}

		// Inherit parent's locked status if parent exists and is locked
		if !locked {
			parentID, hasParent := parentMap[id]
			if hasParent {
				if isNodeLocked(parentID) {
					locked = true
				}
			}
		}

		resolvedLocked[id] = locked
		return locked
	}

	// Construct TopicDetails
	var details []model.TopicDetail
	for _, t := range topics {
		progress := progressMap[t.ID]
		prereqs := prereqMap[t.ID]
		if prereqs == nil {
			prereqs = []model.Topic{}
		}

		details = append(details, model.TopicDetail{
			Topic:         t,
			Progress:      progress,
			Prerequisites: prereqs,
			Locked:        isNodeLocked(t.ID),
		})
	}

	return details, edges, nil
}

func (s *topicService) GenerateQuiz(ctx context.Context, id string) (string, error) {
	detail, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if detail == nil {
		return "", fmt.Errorf("topic not found: %s", id)
	}

	prompt := fmt.Sprintf(`You are an expert technical interviewer. Generate a quiz for the topic "%s" (Description: %s).
The quiz must consist of a dynamic number of multiple-choice questions testing core concepts, depending on the complexity of the topic (usually between 2 and 5 questions).
Each question must have exactly 4 choices.
Return ONLY a raw JSON array matching this schema:
[
  {
    "question": "question text",
    "options": ["option A", "option B", "option C", "option D"],
    "correct_index": 0
  }
]
Do not include any markdown formatting, backticks, or text before or after the JSON. Return only the raw JSON string starting with [ and ending with ].`, detail.Name, detail.Description)

	resp, err := s.llmProvider.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(resp.Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	return content, nil
}

func (s *topicService) SaveQuizAttempt(ctx context.Context, attempt *model.QuizAttempt) error {
	return s.repo.SaveQuizAttempt(ctx, attempt)
}

func (s *topicService) GetQuizAttempts(ctx context.Context, topicID string) ([]model.QuizAttempt, error) {
	return s.repo.GetQuizAttempts(ctx, topicID)
}

func (s *topicService) GenerateNotes(ctx context.Context, id string) (string, error) {
	// 1. Check cache first
	cached, err := s.repo.GetStudyNotes(ctx, id)
	if err == nil && cached != "" {
		return cached, nil
	}

	detail, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if detail == nil {
		return "", fmt.Errorf("topic not found: %s", id)
	}

	prompt := fmt.Sprintf(`You are an expert technical tutor. Provide a comprehensive, structured study guide and core notes for the topic "%s" (Description: %s).
Include key definitions, core concepts, and code snippets or configuration examples where relevant.
Structure it beautifully with clear headings, lists, and bold text. Keep it highly informative but concise.`, detail.Name, detail.Description)

	resp, err := s.llmProvider.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", err
	}

	notes := resp.Message.Content

	// 2. Save/Cache in database
	_ = s.repo.SaveStudyNotes(ctx, id, notes)

	return notes, nil
}

func (s *topicService) GetChatMessages(ctx context.Context, topicID string) ([]llm.Message, error) {
	return s.repo.GetChatMessages(ctx, topicID)
}

func (s *topicService) SaveChatMessage(ctx context.Context, topicID string, role string, content string) error {
	return s.repo.SaveChatMessage(ctx, topicID, role, content)
}
