package llm

import "context"

type ChatRequest struct {
	Messages []Message
}

type Message struct {
	Role    string
	Content string
}

type ChatResponse struct {
	Message Message
}

type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}
