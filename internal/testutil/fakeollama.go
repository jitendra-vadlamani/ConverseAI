// Package testutil has fakes shared by unit and integration tests.
package testutil

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"sync"
	"unicode"

	"ai-chat/internal/ollama"
)

// ChatFunc decides the fake model's reply to a chat request. Returning
// tool calls makes the agent run tools; returning text ends the turn.
type ChatFunc func(req *ollama.ChatRequest) (*ollama.ChatResult, error)

// FakeOllama implements ollama.Client without a server. Embeddings are a
// hashed bag of words, so texts sharing words are similar, which is enough
// for retrieval tests.
type FakeOllama struct {
	mu       sync.Mutex
	OnChat   ChatFunc
	OnGen    func(req *ollama.GenerateRequest) (*ollama.GenerateResponse, error)
	Chats    []ollama.ChatRequest
	Unloaded []string
	Embedded int
	// Block, if set, makes Chat wait until it is closed or ctx ends.
	Block chan struct{}
}

func (f *FakeOllama) Chat(ctx context.Context, req *ollama.ChatRequest, onChunk func(ollama.ChatChunk)) (*ollama.ChatResult, error) {
	f.mu.Lock()
	f.Chats = append(f.Chats, *req)
	fn, block := f.OnChat, f.Block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var res *ollama.ChatResult
	var err error
	if fn != nil {
		res, err = fn(req)
	} else {
		res = &ollama.ChatResult{Message: ollama.ChatMessage{Role: "assistant", Content: "Hello from the fake model."}}
	}
	if err != nil {
		return nil, err
	}
	// Stream the content word by word, like the real server.
	if onChunk != nil {
		for _, w := range strings.SplitAfter(res.Message.Content, " ") {
			if w != "" {
				onChunk(ollama.ChatChunk{Content: w})
			}
		}
	}
	if res.PromptEvalCount == 0 {
		res.PromptEvalCount = 100
	}
	if res.EvalCount == 0 {
		res.EvalCount = 20
	}
	return res, nil
}

func (f *FakeOllama) LastChat() ollama.ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Chats[len(f.Chats)-1]
}

func (f *FakeOllama) ChatCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Chats)
}

func (f *FakeOllama) Generate(_ context.Context, req *ollama.GenerateRequest) (*ollama.GenerateResponse, error) {
	if f.OnGen != nil {
		return f.OnGen(req)
	}
	return &ollama.GenerateResponse{Response: "summary of the conversation", Done: true, EvalCount: 10}, nil
}

func (f *FakeOllama) Embed(_ context.Context, _ string, inputs []string) ([][]float64, error) {
	f.mu.Lock()
	f.Embedded += len(inputs)
	f.mu.Unlock()
	out := make([][]float64, len(inputs))
	for i, in := range inputs {
		out[i] = HashEmbedding(in)
	}
	return out, nil
}

func (f *FakeOllama) Unload(_ context.Context, model string) error {
	f.mu.Lock()
	f.Unloaded = append(f.Unloaded, model)
	f.mu.Unlock()
	return nil
}

func (f *FakeOllama) Capabilities(context.Context, string) ([]string, error) {
	return []string{"completion", "tools"}, nil
}

func (f *FakeOllama) Ping(context.Context) error { return nil }

// HashEmbedding maps text to a normalized 64-dim bag-of-words vector.
func HashEmbedding(text string) []float64 {
	v := make([]float64, 64)
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[h.Sum32()%64]++
	}
	var n float64
	for _, x := range v {
		n += x * x
	}
	if n == 0 {
		v[0] = 1
		return v
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] /= n
	}
	return v
}

// ToolCall builds a tool-call reply.
func ToolCall(name string, args map[string]any) *ollama.ChatResult {
	return &ollama.ChatResult{Message: ollama.ChatMessage{Role: "assistant",
		ToolCalls: []ollama.ToolCall{{Function: ollama.ToolCallFunction{Name: name, Arguments: args}}}}}
}

// Text builds a plain text reply.
func Text(s string) *ollama.ChatResult {
	return &ollama.ChatResult{Message: ollama.ChatMessage{Role: "assistant", Content: s}}
}

// HasToolResult reports whether the request already contains a tool result.
func HasToolResult(req *ollama.ChatRequest) bool {
	for _, m := range req.Messages {
		if m.Role == "tool" {
			return true
		}
	}
	return false
}
