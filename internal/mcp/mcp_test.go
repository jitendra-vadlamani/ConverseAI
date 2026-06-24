package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type mockSearchService struct{}

func (m *mockSearchService) SearchDuckDuckGo(ctx context.Context, query string) ([]model.Evidence, error) {
	return []model.Evidence{{URL: "https://example.com/1", Content: "DDG result 1"}}, nil
}
func (m *mockSearchService) SearchWikipedia(ctx context.Context, query string) ([]model.Evidence, error) {
	return []model.Evidence{{URL: "https://example.com/2", Content: "Wiki result 2"}}, nil
}
func (m *mockSearchService) FetchPageContent(ctx context.Context, url string) (string, error) {
	return "Mock page content extracted", nil
}

type mockRagService struct{}

func (m *mockRagService) Search(ctx context.Context, collectionName string, query string, topK int, fileIDs []string) ([]model.Evidence, error) {
	return []model.Evidence{{Content: "RAG matching paragraph details", Source: "file1.txt"}}, nil
}
func (m *mockRagService) Ingest(ctx context.Context, collectionName, fileID, filename string, content string) error {
	return nil
}
func (m *mockRagService) DeleteCollection(ctx context.Context, collectionName string) error {
	return nil
}
func (m *mockRagService) ClusterEvidence(ctx context.Context, evidences []model.Evidence) ([][]model.Evidence, error) {
	return [][]model.Evidence{evidences}, nil
}

type mockStorageService struct{}

func (m *mockStorageService) Get(ctx context.Context, id string) ([]byte, error) {
	return []byte("Dummy file content"), nil
}

type mockOllamaClient struct{}

func (m *mockOllamaClient) Generate(ctx context.Context, req *ollama.GenerateRequest) (*ollama.GenerateResponse, error) {
	return &ollama.GenerateResponse{
		Response: "AGREE\nThe facts match.",
	}, nil
}
func (m *mockOllamaClient) Embeddings(ctx context.Context, req *ollama.EmbeddingsRequest) (*ollama.EmbeddingsResponse, error) {
	return &ollama.EmbeddingsResponse{Embedding: []float64{0.1, 0.2}}, nil
}
func (m *mockOllamaClient) Chat(ctx context.Context, req *ollama.ChatRequest) (*http.Response, error) {
	return nil, nil
}
func (m *mockOllamaClient) Tags(ctx context.Context) ([]string, error) {
	return []string{"gemma4:latest"}, nil
}
func (m *mockOllamaClient) Show(ctx context.Context, modelName string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (m *mockOllamaClient) Unload(ctx context.Context, modelName string) error {
	return nil
}
func (m *mockOllamaClient) GetBaseURL() string {
	return "http://localhost:11434"
}

func TestRegistryAndBuiltinServer(t *testing.T) {
	registry := NewRegistry()

	server := NewBuiltinServer(
		&mockOllamaClient{},
		&mockStorageService{},
		&mockRagService{},
		&mockSearchService{},
	)
	registry.RegisterServer("builtin", server)

	// List Tools
	tools, err := registry.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	if len(tools) != 4 {
		t.Errorf("Expected 4 tools, got %d", len(tools))
	}

	// Verify tool names exist
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	for _, expected := range []string{"web_search", "retrieve_documents", "summarize", "ocr_extract"} {
		if !names[expected] {
			t.Errorf("Missing tool: %s", expected)
		}
	}

	// Call retrieve_documents
	ctx := context.WithValue(context.Background(), UserIDKey, primitive.NewObjectID())
	res, err := registry.CallTool(ctx, "retrieve_documents", map[string]interface{}{"query": "test"})
	if err != nil {
		t.Fatalf("CallTool retrieve_documents failed: %v", err)
	}

	if len(res.Content) != 1 || res.Content[0].Type != "text" {
		t.Errorf("Unexpected CallToolResult format")
	}

	if !strings.Contains(res.Content[0].Text, "RAG matching paragraph details") {
		t.Errorf("Expected text to contain RAG result, got %s", res.Content[0].Text)
	}
}
