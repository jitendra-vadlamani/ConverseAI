package rag

import (
	"strings"
	"unicode"
)

// Chunks are sized in words, assuming ~1.3 tokens per English word: 300
// words is about 400 tokens, with ~60 tokens of overlap. That keeps chunks
// well inside the embedding model's window without a tokenizer download.
const (
	tokensPerWord = 1.3
	maxWords      = 300
	overlapWords  = 45
)

// EstimateTokens is a cheap token estimate used for budgeting context.
func EstimateTokens(s string) int {
	words := len(strings.Fields(s))
	byChars := len(s) / 4
	byWords := int(float64(words) * tokensPerWord)
	if byChars > byWords {
		return byChars
	}
	return byWords
}

// ChunkText splits text on paragraph and sentence boundaries into chunks of
// about maxWords words, with overlapWords carried over between chunks.
func ChunkText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var chunks []string
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		chunks = append(chunks, strings.Join(cur, " "))
		if len(cur) > overlapWords {
			cur = append([]string(nil), cur[len(cur)-overlapWords:]...)
		} else {
			cur = nil
		}
	}

	for _, unit := range splitUnits(text) {
		words := strings.Fields(unit)
		if len(words) == 0 {
			continue
		}
		// A single sentence longer than a chunk is cut by words.
		for len(words) > maxWords {
			flush()
			cur = append(cur, words[:maxWords]...)
			words = words[maxWords:]
		}
		if len(cur)+len(words) > maxWords {
			flush()
		}
		cur = append(cur, words...)
	}
	if len(cur) > 0 && (len(chunks) == 0 || len(cur) > overlapWords) {
		chunks = append(chunks, strings.Join(cur, " "))
	}
	return chunks
}

// splitUnits breaks text into paragraphs, and paragraphs into sentences.
func splitUnits(text string) []string {
	var units []string
	for _, para := range strings.Split(text, "\n\n") {
		start := 0
		runes := []rune(para)
		for i, r := range runes {
			if (r == '.' || r == '!' || r == '?') && (i+1 == len(runes) || unicode.IsSpace(runes[i+1])) {
				units = append(units, string(runes[start:i+1]))
				start = i + 1
			}
		}
		if start < len(runes) {
			units = append(units, string(runes[start:]))
		}
	}
	return units
}
