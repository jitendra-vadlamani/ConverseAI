package util

import (
	"log"

	"github.com/pkoukk/tiktoken-go"
)

// CountTokens provides a precise token count using the tiktoken library.
// It defaults to the 'gpt-3.5-turbo' encoding, which is a standard and reliable 
// measure for most modern LLM interactions (including Ollama following OpenAI conventions).
func CountTokens(text string) int {
	if text == "" {
		return 0
	}

	// Use the generic 'gpt-3.5-turbo' encoding as the baseline.
	// This ensures consistency across different models while providing precision.
	tkm, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		log.Printf("[TokenUtil] Failed to get encoding: %v", err)
		// Fallback to a safe character-based heuristic if library fails
		return (len(text) + 3) / 4
	}

	tokenIds := tkm.Encode(text, nil, nil)
	return len(tokenIds)
}
