// Package ollama is a small client for the Ollama HTTP API.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

type Client interface {
	// Chat streams a chat completion. onChunk is called for every streamed
	// piece; the returned ChatResult holds the accumulated message.
	Chat(ctx context.Context, req *ChatRequest, onChunk func(ChatChunk)) (*ChatResult, error)
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
	Embed(ctx context.Context, model string, inputs []string) ([][]float64, error)
	Unload(ctx context.Context, modelName string) error
	Capabilities(ctx context.Context, modelName string) ([]string, error)
	Ping(ctx context.Context) error
}

// Tool is a function the model may call, in Ollama's native tool format.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolCall struct {
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ChatMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Thinking  string     `json:"thinking,omitempty"`
	Images    []string   `json:"images,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
}

type ChatRequest struct {
	Model    string         `json:"model"`
	Messages []ChatMessage  `json:"messages"`
	Tools    []Tool         `json:"tools,omitempty"`
	Format   any            `json:"format,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
	// KeepAlive is omitted when nil; set it to 0 to unload after the call.
	KeepAlive any  `json:"keep_alive,omitempty"`
	Stream    bool `json:"stream"`
}

type ChatChunk struct {
	Content  string
	Thinking string
}

type ChatResult struct {
	Message         ChatMessage
	PromptEvalCount int
	EvalCount       int
}

type GenerateRequest struct {
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt"`
	System    string         `json:"system,omitempty"`
	Format    any            `json:"format,omitempty"`
	Images    []string       `json:"images,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
	KeepAlive any            `json:"keep_alive,omitempty"`
	Stream    bool           `json:"stream"`
}

type GenerateResponse struct {
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	PromptEvalCount int    `json:"prompt_eval_count,omitempty"`
	EvalCount       int    `json:"eval_count,omitempty"`
}

// APIError is a non-2xx response from Ollama.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ollama returned %d: %s", e.Status, e.Message)
}

type client struct {
	baseURL string
	http    *http.Client

	capMu sync.Mutex
	caps  map[string][]string
}

// NewClient builds a client whose transport bounds connecting and waiting for
// headers, but not reading the body: a long streamed answer is bounded only by
// the caller's context, so it is never cut off mid-stream.
func NewClient(baseURL string) Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: 5 * time.Minute, // includes model load time
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   8,
	}
	return &client{baseURL: baseURL, http: &http.Client{Transport: transport}, caps: map[string][]string{}}
}

func (c *client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(msg, &e) == nil && e.Error != "" {
			msg = []byte(e.Error)
		}
		return nil, &APIError{Status: resp.StatusCode, Message: string(msg)}
	}
	return resp, nil
}

func (c *client) Chat(ctx context.Context, req *ChatRequest, onChunk func(ChatChunk)) (*ChatResult, error) {
	r := *req
	r.Stream = true
	resp, err := c.post(ctx, "/api/chat", &r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	result := &ChatResult{Message: ChatMessage{Role: "assistant"}}
	var content, thinking bytes.Buffer
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	done := false
	for scanner.Scan() {
		var line struct {
			Message         ChatMessage `json:"message"`
			Done            bool        `json:"done"`
			Error           string      `json:"error"`
			PromptEvalCount int         `json:"prompt_eval_count"`
			EvalCount       int         `json:"eval_count"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return nil, fmt.Errorf("decode ollama stream: %w", err)
		}
		if line.Error != "" {
			return nil, &APIError{Status: http.StatusInternalServerError, Message: line.Error}
		}
		if line.Message.Content != "" || line.Message.Thinking != "" {
			content.WriteString(line.Message.Content)
			thinking.WriteString(line.Message.Thinking)
			if onChunk != nil {
				onChunk(ChatChunk{Content: line.Message.Content, Thinking: line.Message.Thinking})
			}
		}
		result.Message.ToolCalls = append(result.Message.ToolCalls, line.Message.ToolCalls...)
		if line.Done {
			result.PromptEvalCount = line.PromptEvalCount
			result.EvalCount = line.EvalCount
			done = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read ollama stream: %w", err)
	}
	if !done {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("ollama stream ended before completion")
	}
	result.Message.Content = content.String()
	result.Message.Thinking = thinking.String()
	return result, nil
}

func (c *client) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	r := *req
	r.Stream = false
	resp, err := c.post(ctx, "/api/generate", &r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode ollama generate: %w", err)
	}
	return &out, nil
}

func (c *client) Embed(ctx context.Context, model string, inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	resp, err := c.post(ctx, "/api/embed", map[string]any{"model": model, "input": inputs})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode ollama embed: %w", err)
	}
	if len(out.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(out.Embeddings), len(inputs))
	}
	return out.Embeddings, nil
}

// Unload asks Ollama to evict the model now. keep_alive must be sent as an
// explicit 0; omitting it would load the model with the default keep-alive.
func (c *client) Unload(ctx context.Context, modelName string) error {
	resp, err := c.post(ctx, "/api/generate", map[string]any{"model": modelName, "keep_alive": 0, "stream": false})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Capabilities returns what Ollama reports for the model ("completion",
// "tools", "vision", "thinking", ...). Results are cached per model.
func (c *client) Capabilities(ctx context.Context, modelName string) ([]string, error) {
	c.capMu.Lock()
	if caps, ok := c.caps[modelName]; ok {
		c.capMu.Unlock()
		return caps, nil
	}
	c.capMu.Unlock()

	resp, err := c.post(ctx, "/api/show", map[string]string{"model": modelName})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	c.capMu.Lock()
	c.caps[modelName] = out.Capabilities
	c.capMu.Unlock()
	return out.Capabilities, nil
}

func (c *client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/version", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama /api/version returned %d", resp.StatusCode)
	}
	return nil
}
