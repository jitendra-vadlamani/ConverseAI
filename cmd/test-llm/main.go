package main

import (
	"context"
	"fmt"
	"log"

	"ai-chat/internal/config"
	"ai-chat/internal/llm"
)

func main() {
	cfg := config.LoadConfig()
	fmt.Printf("Testing LLM at backend=%s URL=%s model=%s...\n", cfg.LLMBackend, cfg.LLMBaseURL, cfg.LLMModel)
	provider := llm.NewOllamaProvider(cfg.LLMBaseURL, cfg.LLMModel)
	
	resp, err := provider.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Hello, testing the new abstraction!"},
		},
	})
	
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	
	fmt.Printf("Response: %s\n", resp.Message.Content)
}
