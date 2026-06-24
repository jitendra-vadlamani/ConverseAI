package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ai-chat/internal/config"
	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
)

type CosService interface {
	DecomposeGoal(ctx context.Context, goal string) ([]model.ProjectTask, error)
	AnalyzeRealityGap(ctx context.Context, project *model.Project) (string, error)
	GenerateWeeklyReviewFeedback(ctx context.Context, project *model.Project) (string, error)
}

type cosService struct {
	ollamaClient ollama.Client
	cfg          *config.Config
}

func NewCosService(ollamaClient ollama.Client, cfg *config.Config) CosService {
	return &cosService{
		ollamaClient: ollamaClient,
		cfg:          cfg,
	}
}

// JSONTask represents the structure we request from the LLM
type JSONTask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Impact      int    `json:"impact"`
	Urgency     int    `json:"urgency"`
	Effort      int    `json:"effort"`
	Alignment   int    `json:"alignment"`
}

type GoalDecompositionResponse struct {
	Tasks []JSONTask `json:"tasks"`
}

func (s *cosService) DecomposeGoal(ctx context.Context, goal string) ([]model.ProjectTask, error) {
	prompt := fmt.Sprintf(`You are the Chief of Staff AI.
Your job is to break down the user's primary North Star goal into a series of 5 to 8 structured, high-leverage milestones/tasks.
Every task should be assigned priorites (from 1 to 10):
- Impact (how much this contributes to the goal)
- Urgency (how time-sensitive it is)
- Effort (relative difficulty/time required)
- Alignment (how closely related it is to the core objective)

User Goal: "%s"

You MUST respond strictly with a JSON object of this exact schema:
{
  "tasks": [
    {
      "title": "Task title",
      "description": "Short explanation of the milestone and action items",
      "impact": 8,
      "urgency": 6,
      "effort": 5,
      "alignment": 9
    }
  ]
}
Do not write any other markdown text, only raw JSON.`, goal)

	modelName := s.cfg.DefaultPlannerModel
	if modelName == "" {
		modelName = "gemma4:latest"
	}

	resp, err := s.ollamaClient.Generate(ctx, &ollama.GenerateRequest{
		Model:  modelName,
		Prompt: prompt,
		Format: "json",
		Stream: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM for goal decomposition: %w", err)
	}

	var parsedResp GoalDecompositionResponse
	if err := json.Unmarshal([]byte(resp.Response), &parsedResp); err != nil {
		// Fallback - maybe response contains JSON but wrapped in codeblocks
		cleanJSON := resp.Response
		if idx := len(cleanJSON); idx > 0 {
			// Basic cleanup if any markdown formatting leaks
			cleanJSON = cleanupJSONString(cleanJSON)
			if err2 := json.Unmarshal([]byte(cleanJSON), &parsedResp); err2 != nil {
				return nil, fmt.Errorf("failed to parse LLM goal decomposition JSON: %w (Raw: %s)", err, resp.Response)
			}
		}
	}

	var tasks []model.ProjectTask
	for i, jt := range parsedResp.Tasks {
		tasks = append(tasks, model.ProjectTask{
			ID:          fmt.Sprintf("task_%d_%d", time.Now().Unix(), i),
			Title:       jt.Title,
			Description: jt.Description,
			Impact:      jt.Impact,
			Urgency:     jt.Urgency,
			Effort:      jt.Effort,
			Alignment:   jt.Alignment,
			Status:      "pending",
		})
	}

	return tasks, nil
}

func (s *cosService) AnalyzeRealityGap(ctx context.Context, project *model.Project) (string, error) {
	// Construct the context representing current progress
	taskCompletions := 0
	totalTasks := len(project.Tasks)
	for _, t := range project.Tasks {
		if t.Status == "completed" {
			taskCompletions++
		}
	}

	competencyInfo := ""
	for _, comp := range project.Competencies {
		competencyInfo += fmt.Sprintf("- %s: %d%%\n", comp.Area, comp.ProgressPercentage)
	}

	memoryConstraints := ""
	for _, mem := range project.MemoryItems {
		if mem.Category == "constraint" {
			memoryConstraints += fmt.Sprintf("- %s\n", mem.Content)
		}
	}

	prompt := fmt.Sprintf(`You are the Chief of Staff AI. Perform a Reality Gap Detection and Feasibility analysis.
Assess if the user's timeline and goals are realistic given their current completion status, competency levels, and memory constraints.

Goal: "%s"
Target Completion Date: %s
Current Date: %s

Execution Progress:
- Total Milestones: %d
- Completed Milestones: %d

Competency Levels:
%s

Known Constraints:
%s

Provide a highly professional evaluation including:
1. **Current Readiness Assessment**: Summary of current competence.
2. **Projected Timeline & Feasibility**: Given the constraints, is the target date achievable? What is the projected timeline?
3. **Actionable Adjustments**: Concrete advice (e.g. increase weekly study hours, drop non-critical dependencies, focus on specific weak areas).

Format with clean markdown and use a supportive but strict, realistic tone.`,
		project.Title,
		project.TargetDate.Format("January 2006"),
		time.Now().Format("January 2, 2006"),
		totalTasks,
		taskCompletions,
		competencyInfo,
		memoryConstraints,
	)

	modelName := s.cfg.DefaultChatModel
	if modelName == "" {
		modelName = "gemma4:latest"
	}

	resp, err := s.ollamaClient.Generate(ctx, &ollama.GenerateRequest{
		Model:  modelName,
		Prompt: prompt,
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate reality gap report: %w", err)
	}

	return resp.Response, nil
}

func (s *cosService) GenerateWeeklyReviewFeedback(ctx context.Context, project *model.Project) (string, error) {
	completedCount := 0
	pendingTasks := ""
	completedTasks := ""

	for _, t := range project.Tasks {
		if t.Status == "completed" {
			completedCount++
			completedTasks += fmt.Sprintf("- %s (Impact: %d/10)\n", t.Title, t.Impact)
		} else {
			pendingTasks += fmt.Sprintf("- %s (Urgency: %d/10, Effort: %d/10)\n", t.Title, t.Urgency, t.Effort)
		}
	}

	lessonsList := ""
	constraintsList := ""
	for _, m := range project.MemoryItems {
		if m.Category == "lesson" {
			lessonsList += fmt.Sprintf("- %s\n", m.Content)
		} else if m.Category == "constraint" {
			constraintsList += fmt.Sprintf("- %s\n", m.Content)
		}
	}

	executionRate := 0.0
	if len(project.Tasks) > 0 {
		executionRate = (float64(completedCount) / float64(len(project.Tasks))) * 100
	}

	prompt := fmt.Sprintf(`You are the Chief of Staff AI running a Weekly Executive Review.
Evaluate the user's progress and output a review summarizing their execution performance, identifying alignment risk, and defining next steps.

Goal: "%s"
Execution Rate: %.1f%% (%d/%d Milestones completed)

Completed Tasks This Week:
%s

Uncompleted/Delayed Tasks:
%s

Active Constraints:
%s

Logged Lessons:
%s

Provide an analytical review:
1. **Performance Metric Analysis**: Calculate execution score and rate the week.
2. **Major Risks & Bottlenecks**: Pinpoint why tasks were delayed or what constraints are impacting progress.
3. **Course Correction / Action Plan for Next Week**: Recommend a refined priority list of tasks to execute next.

Format in markdown, and write in an encouraging but objective tone.`,
		project.Title,
		executionRate,
		completedCount,
		len(project.Tasks),
		completedTasks,
		pendingTasks,
		constraintsList,
		lessonsList,
	)

	modelName := s.cfg.DefaultChatModel
	if modelName == "" {
		modelName = "gemma4:latest"
	}

	resp, err := s.ollamaClient.Generate(ctx, &ollama.GenerateRequest{
		Model:  modelName,
		Prompt: prompt,
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate weekly review feedback: %w", err)
	}

	return resp.Response, nil
}

func cleanupJSONString(s string) string {
	// Simple bracket boundaries cleanup in case of conversational prefix/suffix leaks
	start := -1
	end := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			start = i
			break
		}
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '}' {
			end = i
			break
		}
	}
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}
	return s
}
