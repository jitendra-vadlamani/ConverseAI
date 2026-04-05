package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type Client interface {
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
	Chat(ctx context.Context, req *ChatRequest) (*http.Response, error) // Returns http.Response for streaming
	Tags(ctx context.Context) ([]string, error)
	Show(ctx context.Context, modelName string) (map[string]interface{}, error)
	Unload(ctx context.Context, modelName string) error
	Embeddings(ctx context.Context, req *EmbeddingsRequest) (*EmbeddingsResponse, error)
	GetBaseURL() string
}

type EmbeddingsRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type EmbeddingsResponse struct {
	Embedding []float64 `json:"embedding"`
}

type GenerateRequest struct {
	Model     string                 `json:"model"`
	Prompt    string                 `json:"prompt"`
	Stream    bool                   `json:"stream"`
	System    string                 `json:"system,omitempty"`
	Format    string                 `json:"format,omitempty"`
	Images    []string               `json:"images,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
	KeepAlive int                    `json:"keep_alive,omitempty"`
}

type GenerateResponse struct {
	Response          string `json:"response"`
	Done              bool   `json:"done"`
	PromptEvalCount   int    `json:"prompt_eval_count,omitempty"`
	EvalCount         int    `json:"eval_count,omitempty"`
}

type ChatMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

type ChatRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	Stream    bool          `json:"stream"`
	Options   map[string]interface{} `json:"options,omitempty"`
	KeepAlive int           `json:"keep_alive,omitempty"`
}

type ollamaClient struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient() Client {
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &ollamaClient{
		httpClient: &http.Client{Timeout: 120 * time.Second},
		baseURL:    strings.TrimSuffix(baseURL, "/"),
	}
}

func (c *ollamaClient) GetBaseURL() string {
	return c.baseURL
}

func (c *ollamaClient) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	url := c.baseURL + "/api/generate"
	
	reqBody, _ := json.Marshal(req)
	hReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(hReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama generate failed with status %d", resp.StatusCode)
	}

	var result GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *ollamaClient) Chat(ctx context.Context, req *ChatRequest) (*http.Response, error) {
	url := c.baseURL + "/api/chat"
	
	reqBody, _ := json.Marshal(req)
	hReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(hReq)
	if err != nil {
		return nil, err
	}

	// Check for HTTP error status before returning the stream
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("ollama chat failed with status %d", resp.StatusCode)
	}

	// We return the raw response so the caller can process the stream
	return resp, nil
}

func (c *ollamaClient) Tags(ctx context.Context) ([]string, error) {
	url := c.baseURL + "/api/tags"
	
	hReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(hReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}

	var names []string
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

func (c *ollamaClient) Show(ctx context.Context, modelName string) (map[string]interface{}, error) {
	url := c.baseURL + "/api/show"
	body, _ := json.Marshal(map[string]string{"name": modelName})
	
	hReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(hReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *ollamaClient) Unload(ctx context.Context, modelName string) error {
	_, err := c.Generate(ctx, &GenerateRequest{
		Model:     modelName,
		KeepAlive: 0,
		Stream:    false,
	})
	return err
}

func (c *ollamaClient) Embeddings(ctx context.Context, req *EmbeddingsRequest) (*EmbeddingsResponse, error) {
	url := c.baseURL + "/api/embeddings"
	
	reqBody, _ := json.Marshal(req)
	hReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(hReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embeddings failed with status %d", resp.StatusCode)
	}

	var result EmbeddingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
