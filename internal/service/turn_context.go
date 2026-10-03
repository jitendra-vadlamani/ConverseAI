package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"ai-chat/internal/agent"
	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/rag"
	"ai-chat/internal/storage"
	"ai-chat/internal/util"
)

const (
	// Files up to this much text are put into the prompt whole; larger ones
	// are retrieved from the index.
	inlineFileBytes  = 24 * 1024
	largeFilePreview = 3000
	preRetrieveCount = 5
	// Tokens kept free for the answer and tool results.
	answerReserve = 1536
	// Summarize when the prompt would fill this share of num_ctx.
	summarizeAt = 0.80
)

// turnInput is everything that varies per turn when building the prompt.
type turnInput struct {
	conv        *model.Conversation
	userMsgIdx  int
	llm         *model.LLMConfig
	maxNumCtx   int
	useTools    bool
	fileContext string   // text files attached to this turn
	retrieved   string   // passages pre-retrieved for this turn
	images      []string // base64 images attached to this turn
	attachments []string // ids of every attachment, for the file list
}

// buildMessages renders the conversation into chat messages. Stored
// messages contain only what the user typed; file text and retrieved
// passages are added to the current turn here and never persisted.
func buildMessages(in turnInput, now time.Time) []ollama.ChatMessage {
	msgs := []ollama.ChatMessage{{Role: "system", Content: agent.SystemPrompt(now, in.useTools)}}
	if in.conv.Summary != "" {
		msgs = append(msgs, ollama.ChatMessage{Role: "system", Content: "Summary of the earlier part of this conversation:\n" + in.conv.Summary})
	}
	for _, m := range in.conv.Messages[:in.userMsgIdx] {
		if m.IsSummarized || m.Content == "" {
			continue
		}
		content := m.Content
		if len(m.Attachments) > 0 {
			content += "\n\n(Attached: " + displayNames(m.Attachments) + ")"
		}
		msgs = append(msgs, ollama.ChatMessage{Role: string(m.Role), Content: content})
	}

	cur := in.conv.Messages[in.userMsgIdx]
	var sb strings.Builder
	sb.WriteString(cur.Content)
	if len(in.attachments) > 0 {
		sb.WriteString("\n\n---\nFiles attached to this conversation:\n")
		for _, id := range in.attachments {
			fmt.Fprintf(&sb, "- %s (file_id: %s)\n", storage.DisplayName(id), id)
		}
	}
	if in.fileContext != "" {
		sb.WriteString("\nContents of the files attached to this message:\n")
		sb.WriteString(in.fileContext)
	}
	if in.retrieved != "" {
		sb.WriteString("\n")
		sb.WriteString(in.retrieved)
	}
	msgs = append(msgs, ollama.ChatMessage{Role: "user", Content: sb.String(), Images: in.images})
	return msgs
}

func displayNames(ids []string) string {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = storage.DisplayName(id)
	}
	return strings.Join(names, ", ")
}

func estimateMessages(msgs []ollama.ChatMessage) int {
	total := 0
	for _, m := range msgs {
		total += rag.EstimateTokens(m.Content) + 4 + 768*len(m.Images)
	}
	return total
}

// fileText loads an attachment and returns its text ("" for non-text).
func fileText(ctx context.Context, store storage.StorageService, fileID string) (string, error) {
	data, err := store.Get(ctx, fileID)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(fileID))
	switch {
	case ext == ".pdf":
		return util.ExtractTextFromPDF(data)
	case util.IsText(ext):
		return string(data), nil
	}
	return "", nil
}

// inlineFile renders one attached text file for the prompt.
func inlineFile(fileID, text string) string {
	name := storage.DisplayName(fileID)
	if len(text) <= inlineFileBytes {
		return util.FenceUntrusted(name, text) + "\n"
	}
	return fmt.Sprintf("%s is large (%d KB); only the start is shown. Relevant passages are retrieved separately.\n%s\n",
		name, len(text)/1024, util.FenceUntrusted(name, util.TruncateRunes(text, largeFilePreview)+"\n[...]"))
}

func encodeImage(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

// transcript renders messages for the summarizer.
func transcript(msgs []model.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m.IsSummarized || m.Content == "" {
			continue
		}
		fmt.Fprintf(&sb, "%s: %s\n\n", m.Role, m.Content)
	}
	return sb.String()
}
