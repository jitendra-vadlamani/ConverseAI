package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type OllamaProvider struct {
	BaseURL string
	Model   string
	client  *http.Client
}

func NewOllamaProvider(baseURL, model string) *OllamaProvider {
	return &OllamaProvider{
		BaseURL: baseURL,
		Model:   model,
		client:  &http.Client{},
	}
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
}

func (p *OllamaProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	url := fmt.Sprintf("%s/api/chat", p.BaseURL)
	
	oReq := ollamaRequest{
		Model:    p.Model,
		Stream:   false,
		Messages: make([]ollamaMessage, len(req.Messages)),
	}
	
	for i, m := range req.Messages {
		oReq.Messages[i] = ollamaMessage{Role: m.Role, Content: m.Content}
	}
	
	body, err := json.Marshal(oReq)
	if err != nil {
		return ChatResponse{}, err
	}
	
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("ollama API error: status %d", resp.StatusCode)
	}
	
	var oResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return ChatResponse{}, err
	}
	
	return ChatResponse{
		Message: Message{
			Role:    oResp.Message.Role,
			Content: oResp.Message.Content,
		},
	}, nil
}
