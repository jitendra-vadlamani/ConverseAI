package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/util"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type RagService interface {
	Search(ctx context.Context, collectionName string, query string, topK int, fileIDs []string) ([]model.Evidence, error)
	Ingest(ctx context.Context, collectionName, fileID, filename string, content string) error
	DeleteCollection(ctx context.Context, collectionName string) error
	ClusterEvidence(ctx context.Context, evidences []model.Evidence) ([][]model.Evidence, error)
}

type SearchService interface {
	SearchDuckDuckGo(ctx context.Context, query string) ([]model.Evidence, error)
	SearchWikipedia(ctx context.Context, query string) ([]model.Evidence, error)
	FetchPageContent(ctx context.Context, url string) (string, error)
}

type StorageService interface {
	Get(ctx context.Context, id string) ([]byte, error)
}

type builtinServer struct {
	ollamaClient   ollama.Client
	storageService StorageService
	ragService     RagService
	searchService  SearchService
}

func NewBuiltinServer(client ollama.Client, storage StorageService, rag RagService, search SearchService) Server {
	return &builtinServer{
		ollamaClient:   client,
		storageService: storage,
		ragService:     rag,
		searchService:  search,
	}
}

func (s *builtinServer) Name() string {
	return "converseai-builtin"
}

func (s *builtinServer) ListTools(ctx context.Context) ([]Tool, error) {
	return []Tool{
		{
			Name:        "web_search",
			Description: "Search the web for real-time or external information using DuckDuckGo and Wikipedia, rank the results, and check for contradictions.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query to look up on the web.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "retrieve_documents",
			Description: "Search local user-uploaded files for relevant information.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query to match against uploaded documents.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "summarize",
			Description: "Summarize a long piece of text using an LLM.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text to summarize.",
					},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "ocr_extract",
			Description: "Extract all text from an uploaded image or document file using optical character recognition (OCR) / vision LLM.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_id": map[string]interface{}{
						"type":        "string",
						"description": "The storage ID of the uploaded file/image.",
					},
				},
				"required": []string{"file_id"},
			},
		},
	}, nil
}

func (s *builtinServer) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	switch name {
	case "web_search":
		query, ok := arguments["query"].(string)
		if !ok || query == "" {
			return nil, fmt.Errorf("missing or invalid query argument")
		}
		return s.runWebSearch(ctx, query)

	case "retrieve_documents":
		query, ok := arguments["query"].(string)
		if !ok || query == "" {
			return nil, fmt.Errorf("missing or invalid query argument")
		}
		return s.runRetrieveDocuments(ctx, query)

	case "summarize":
		text, ok := arguments["text"].(string)
		if !ok || text == "" {
			return nil, fmt.Errorf("missing or invalid text argument")
		}
		return s.runSummarize(ctx, text)

	case "ocr_extract":
		fileID, ok := arguments["file_id"].(string)
		if !ok || fileID == "" {
			return nil, fmt.Errorf("missing or invalid file_id argument")
		}
		return s.runOcrExtract(ctx, fileID)

	default:
		return nil, fmt.Errorf("unsupported tool: %s", name)
	}
}

func (s *builtinServer) runWebSearch(ctx context.Context, query string) (*CallToolResult, error) {
	// 1. Get Conversation ID & User ID from context if present
	var convID primitive.ObjectID
	if val := ctx.Value(ConversationIDKey); val != nil {
		if id, ok := val.(primitive.ObjectID); ok {
			convID = id
		}
	}

	// 2. Initial Search
	evidences, err := s.searchService.SearchDuckDuckGo(ctx, query)
	if err != nil || len(evidences) == 0 {
		evidences, err = s.searchService.SearchWikipedia(ctx, query)
	}

	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if len(evidences) == 0 {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: "No search results found."}},
		}, nil
	}

	// Extract top 3 pages in parallel
	limit := 3
	if len(evidences) < limit {
		limit = len(evidences)
	}

	tempCollection := fmt.Sprintf("temp-search-%s", convID.Hex())
	_ = s.ragService.DeleteCollection(ctx, tempCollection)

	type pageResult struct {
		id      string
		content string
		source  string
		err     error
	}
	resChan := make(chan pageResult, limit)

	for i := 0; i < limit; i++ {
		go func(ev model.Evidence, idx int) {
			content, err := s.searchService.FetchPageContent(ctx, ev.URL)
			resChan <- pageResult{id: fmt.Sprintf("web-%d", idx), content: content, source: ev.URL, err: err}
		}(evidences[i], i)
	}

	extractedCount := 0
	for i := 0; i < limit; i++ {
		res := <-resChan
		if res.err == nil && res.content != "" {
			_ = s.ragService.Ingest(ctx, tempCollection, res.id, res.source, res.content)
			extractedCount++
		}
	}

	// Search and rank using temp RAG
	rankedResults, err := s.ragService.Search(ctx, tempCollection, query, 5, nil)
	if err != nil || len(rankedResults) == 0 {
		// Fallback to snippets
		var sb strings.Builder
		sb.WriteString("Retrieved snippets (Full page extraction failed):\n")
		for _, ev := range evidences {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", ev.Source, ev.Content))
		}
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: sb.String()}},
		}, nil
	}

	// Fact checking / contradiction detection
	checkCtx, checkCancel := context.WithTimeout(ctx, 15*time.Second)
	defer checkCancel()

	clusters, _ := s.ragService.ClusterEvidence(checkCtx, rankedResults)
	for _, cluster := range clusters {
		if len(cluster) > 1 {
			s.detectContradiction(checkCtx, cluster)
		}
	}

	// Scoring & Sorting
	for i := range rankedResults {
		ev := &rankedResults[i]
		ev.FinalScore = (ev.RelevanceScore * 0.6) + (ev.AuthorityScore * 0.2) + (ev.FreshnessScore * 0.2)
		if ev.IsConflicting {
			ev.FinalScore *= 0.5
		}
	}

	sort.Slice(rankedResults, func(i, j int) bool {
		return rankedResults[i].FinalScore > rankedResults[j].FinalScore
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Top verified information for '%s':\n", query))
	for _, ev := range rankedResults {
		if ev.IsConflicting {
			sb.WriteString(fmt.Sprintf("[Source: %s | Final Score: %.2f | !! CONFLICT !!: %s]\n%s\n---\n", ev.Source, ev.FinalScore, ev.ConflictReason, ev.Content))
		} else {
			sb.WriteString(fmt.Sprintf("[Source: %s | Final Score: %.2f | Authority: %.1f | Freshness: %.1f]\n%s\n---\n", ev.Source, ev.FinalScore, ev.AuthorityScore, ev.FreshnessScore, ev.Content))
		}
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: sb.String()}},
	}, nil
}

func (s *builtinServer) detectContradiction(ctx context.Context, cluster []model.Evidence) {
	if len(cluster) < 2 {
		return
	}

	var sb strings.Builder
	for i, ev := range cluster {
		fmt.Fprintf(&sb, "Source [%d]: %s\nContent: %s\n\n", i, ev.Source, ev.Content)
	}

	prompt := fmt.Sprintf(`Analyze these %d pieces of evidence. Do they agree on the facts, or is there a contradiction?
Reply with EXACTLY "AGREE" or "CONTRADICT" on the first line, followed by a one-sentence reason on the second line.

Evidence:
%s`, len(cluster), sb.String())

	resp, err := s.ollamaClient.Generate(ctx, &ollama.GenerateRequest{
		Model:  "gemma4:latest",
		Prompt: prompt,
		System: "You are a professional fact-checker. Be strict. Only flag as CONTRADICT if they actively disagree on a quantifiable or qualitative claim.",
	})
	if err != nil {
		return
	}

	lines := strings.Split(strings.TrimSpace(resp.Response), "\n")
	if len(lines) > 0 && strings.ToUpper(lines[0]) == "CONTRADICT" {
		reason := "Known conflict between sources."
		if len(lines) > 1 {
			reason = lines[1]
		}
		for i := range cluster {
			cluster[i].IsConflicting = true
			cluster[i].ConflictReason = reason
		}
	}
}

func (s *builtinServer) runRetrieveDocuments(ctx context.Context, query string) (*CallToolResult, error) {
	var userID primitive.ObjectID
	if val := ctx.Value(UserIDKey); val != nil {
		if id, ok := val.(primitive.ObjectID); ok {
			userID = id
		}
	}

	if userID.IsZero() {
		return nil, fmt.Errorf("user ID context missing for document retrieval")
	}

	collectionName := fmt.Sprintf("user-knowledge-%s", userID.Hex())
	evidences, err := s.ragService.Search(ctx, collectionName, query, 5, nil)
	if err != nil {
		return nil, err
	}

	if len(evidences) == 0 {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: "No relevant documents found in local knowledge."}},
		}, nil
	}

	var sb strings.Builder
	for _, ev := range evidences {
		sb.WriteString(fmt.Sprintf("[Source: %s | Relevance: %.2f]\n%s\n---\n", ev.Source, ev.RelevanceScore, ev.Content))
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: sb.String()}},
	}, nil
}

func (s *builtinServer) runSummarize(ctx context.Context, text string) (*CallToolResult, error) {
	prompt := fmt.Sprintf("Summarize the following input. Respond ONLY with the summary.\n\nINPUT:\n%s", text)
	resp, err := s.ollamaClient.Generate(ctx, &ollama.GenerateRequest{
		Model:  "gemma4:latest",
		Prompt: prompt,
	})
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: resp.Response}},
	}, nil
}

func (s *builtinServer) runOcrExtract(ctx context.Context, fileID string) (*CallToolResult, error) {
	data, err := s.storageService.Get(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve file from storage: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(fileID))
	var images []string
	var extraContext strings.Builder

	if util.IsImage(ext) {
		images = append(images, base64.StdEncoding.EncodeToString(data))
	} else if util.IsText(ext) {
		extraContext.WriteString(string(data))
	} else if ext == ".pdf" {
		text, err := util.ExtractTextFromPDF(data)
		if err != nil {
			return nil, fmt.Errorf("failed to extract text from PDF: %w", err)
		}
		extraContext.WriteString(text)
	} else {
		return nil, fmt.Errorf("unsupported file format for OCR: %s", ext)
	}

	prompt := "Extract all text from this image."
	if extraContext.Len() > 0 {
		prompt = fmt.Sprintf("Analyze and output the contents of this text document:\n\n%s", extraContext.String())
	}

	resp, err := s.ollamaClient.Generate(ctx, &ollama.GenerateRequest{
		Model:  "gemma4:latest",
		Prompt: prompt,
		Images: images,
	})
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: resp.Response}},
	}, nil
}
