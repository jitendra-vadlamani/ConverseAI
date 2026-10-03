package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
)

// Tool is one capability the model can call. Parameters is a JSON Schema
// object; arguments are validated against it before Run is called.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Run(ctx context.Context, rc *RunContext, args map[string]any) (string, error)
}

func toolDefinition(t Tool) ollama.Tool {
	return ollama.Tool{Type: "function", Function: ollama.ToolFunction{
		Name: t.Name(), Description: t.Description(), Parameters: t.Parameters(),
	}}
}

// Source is a numbered piece of evidence the answer can cite as [N].
type Source struct {
	N     int    `json:"n"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
}

// RunContext is what tools may know about the run they serve.
type RunContext struct {
	UserID         string
	ConversationID string
	// ChatModel is the model answering; tools that need a quick LLM call
	// (fact-checking) reuse it so the GPU doesn't switch models.
	ChatModel string
	// TextFileIDs and ImageFileIDs are the attachments of this conversation;
	// tools may only touch these.
	TextFileIDs  []string
	ImageFileIDs []string
	// Emit records a System Logs event.
	Emit func(t model.EventType, payload map[string]any)

	mu      sync.Mutex
	sources []Source
}

// AddSource registers a source and returns its citation number. A URL or
// title that is already registered keeps its number.
func (rc *RunContext) AddSource(title, url string) int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for _, s := range rc.sources {
		if (url != "" && s.URL == url) || (url == "" && s.URL == "" && s.Title == title) {
			return s.N
		}
	}
	n := len(rc.sources) + 1
	rc.sources = append(rc.sources, Source{N: n, Title: title, URL: url})
	return n
}

func (rc *RunContext) Sources() []Source {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]Source(nil), rc.sources...)
}

func (rc *RunContext) setSources(s []Source) {
	rc.mu.Lock()
	rc.sources = append([]Source(nil), s...)
	rc.mu.Unlock()
}

func (rc *RunContext) emit(t model.EventType, payload map[string]any) {
	if rc.Emit != nil {
		rc.Emit(t, payload)
	}
}

// CitedSourcesFooter lists the sources the answer actually cites.
func CitedSourcesFooter(answer string, sources []Source) string {
	var lines []string
	for _, s := range sources {
		if !strings.Contains(answer, fmt.Sprintf("[%d]", s.N)) {
			continue
		}
		if s.URL != "" {
			lines = append(lines, fmt.Sprintf("[%d] [%s](%s)", s.N, escapeMarkdown(s.Title), s.URL))
		} else {
			lines = append(lines, fmt.Sprintf("[%d] %s", s.N, escapeMarkdown(s.Title)))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n**Sources**\n\n" + strings.Join(lines, "  \n")
}

func escapeMarkdown(s string) string {
	return strings.NewReplacer("[", "(", "]", ")", "\n", " ").Replace(s)
}
