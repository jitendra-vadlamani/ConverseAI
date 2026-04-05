package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ai-chat/internal/config"
	"ai-chat/internal/ollama"
)

type RagService interface {
	Ingest(ctx context.Context, userID, fileID, filename string, content string) error
	Search(ctx context.Context, userID string, query string, topK int, fileIDs []string) ([]string, error)
	DeleteFileKnowledge(ctx context.Context, userID, fileID string) error
	DeleteUserKnowledge(ctx context.Context, userID string) error
}

type ragService struct {
	chromaURL      string
	embeddingModel string
	ollamaClient   ollama.Client
	httpClient     *http.Client
}

func NewRagService(cfg *config.Config, ollamaClient ollama.Client) RagService {
	return &ragService{
		chromaURL:      strings.TrimSuffix(cfg.ChromaURL, "/"),
		embeddingModel: cfg.EmbeddingModel,
		ollamaClient:   ollamaClient,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *ragService) Ingest(ctx context.Context, userID, fileID, filename string, content string) error {
	collectionName := fmt.Sprintf("user-knowledge-%s", userID)
	collectionID, err := s.ensureCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	chunks := s.chunkText(content, 1000, 200)
	fmt.Printf("[RAG] Ingesting %d chunks for file %s (User: %s)\n", len(chunks), filename, userID)

	for i, chunk := range chunks {
		embResp, err := s.ollamaClient.Embeddings(ctx, &ollama.EmbeddingsRequest{
			Model:  s.embeddingModel,
			Prompt: chunk,
		})
		if err != nil {
			return fmt.Errorf("failed to generate embedding for chunk %d: %w", i, err)
		}

		id := fmt.Sprintf("%s-chunk-%d", fileID, i)
		err = s.addToChroma(ctx, collectionID, id, chunk, embResp.Embedding, map[string]interface{}{
			"file_id":   fileID,
			"filename":  filename,
			"chunk_idx": i,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *ragService) Search(ctx context.Context, userID string, query string, topK int, fileIDs []string) ([]string, error) {
	collectionName := fmt.Sprintf("user-knowledge-%s", userID)
	collectionID, err := s.getCollectionID(ctx, collectionName)
	if err != nil {
		// If collection doesn't exist, no knowledge to return
		return nil, nil
	}

	embResp, err := s.ollamaClient.Embeddings(ctx, &ollama.EmbeddingsRequest{
		Model:  s.embeddingModel,
		Prompt: query,
	})
	if err != nil {
		return nil, err
	}

	return s.queryChroma(ctx, collectionID, embResp.Embedding, topK, fileIDs)
}

func (s *ragService) DeleteFileKnowledge(ctx context.Context, userID, fileID string) error {
	collectionName := fmt.Sprintf("user-knowledge-%s", userID)
	collectionID, err := s.getCollectionID(ctx, collectionName)
	if err != nil {
		return nil // Collection doesn't exist, nothing to delete
	}

	url := fmt.Sprintf("%s/api/v1/collections/%s/delete", s.chromaURL, collectionID)
	payload := map[string]interface{}{
		"where": map[string]string{"file_id": fileID},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (s *ragService) DeleteUserKnowledge(ctx context.Context, userID string) error {
	collectionName := fmt.Sprintf("user-knowledge-%s", userID)
	url := fmt.Sprintf("%s/api/v1/collections/%s", s.chromaURL, collectionName)
	
	req, _ := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// ChromaDB Helpers

func (s *ragService) ensureCollection(ctx context.Context, name string) (string, error) {
	id, err := s.getCollectionID(ctx, name)
	if err == nil {
		return id, nil
	}

	url := s.chromaURL + "/api/v1/collections"
	body, _ := json.Marshal(map[string]string{"name": name})
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.ID, nil
}

func (s *ragService) getCollectionID(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/collections/%s", s.chromaURL, name)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("collection not found")
	}

	var res struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&res)
	return res.ID, nil
}

func (s *ragService) addToChroma(ctx context.Context, collectionID, id, document string, embedding []float64, metadata map[string]interface{}) error {
	url := fmt.Sprintf("%s/api/v1/collections/%s/add", s.chromaURL, collectionID)
	
	payload := map[string]interface{}{
		"ids":        []string{id},
		"embeddings": [][]float64{embedding},
		"metadatas":  []map[string]interface{}{metadata},
		"documents":  []string{document},
	}
	
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (s *ragService) queryChroma(ctx context.Context, collectionID string, embedding []float64, topK int, fileIDs []string) ([]string, error) {
	url := fmt.Sprintf("%s/api/v1/collections/%s/query", s.chromaURL, collectionID)
	
	payload := map[string]interface{}{
		"query_embeddings": [][]float64{embedding},
		"n_results":        topK,
	}

	// Add Metadata Filtering if fileIDs are specified
	if len(fileIDs) > 0 {
		payload["where"] = map[string]interface{}{
			"file_id": map[string]interface{}{
				"$in": fileIDs,
			},
		}
	}
	
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Documents [][]string `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Documents) > 0 {
		return result.Documents[0], nil
	}
	return nil, nil
}

func (s *ragService) chunkText(text string, chunkSize, overlap int) []string {
	if len(text) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkSize - overlap {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
