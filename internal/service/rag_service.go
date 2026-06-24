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
	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"math"
)

type RagService interface {
	DeleteCollection(ctx context.Context, collectionName string) error
	DeleteFileKnowledge(ctx context.Context, collectionName, fileID string) error
	ClusterEvidence(ctx context.Context, evidences []model.Evidence) ([][]model.Evidence, error)
	Ingest(ctx context.Context, collectionName, fileID, filename string, content string) error
	Search(ctx context.Context, collectionName string, query string, topK int, fileIDs []string) ([]model.Evidence, error)
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

func (s *ragService) Ingest(ctx context.Context, collectionName, fileID, filename string, content string) error {
	collectionID, err := s.ensureCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	chunks := s.chunkText(content, 1000, 200)
	fmt.Printf("[RAG] Ingesting %d chunks for file %s into %s\n", len(chunks), filename, collectionName)

	for i, chunk := range chunks {
		embResp, err := s.ollamaClient.Embeddings(ctx, &ollama.EmbeddingsRequest{
			Model:  s.embeddingModel,
			Prompt: chunk,
		})
		if err != nil {
			return fmt.Errorf("failed to generate embedding for chunk %d: %w", i, err)
		}

		// Phase 1 Deduplication: Check if highly similar content exists
		existing, _ := s.queryChroma(ctx, collectionID, embResp.Embedding, 1, nil)
		if len(existing) > 0 && existing[0].RelevanceScore > 0.95 {
			fmt.Printf("[RAG] Skipping redundant chunk %d (Similarity: %.2f)\n", i, existing[0].RelevanceScore)
			continue
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

func (s *ragService) Search(ctx context.Context, collectionName string, query string, topK int, fileIDs []string) ([]model.Evidence, error) {
	collectionID, err := s.getCollectionID(ctx, collectionName)
	if err != nil {
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

func (s *ragService) DeleteFileKnowledge(ctx context.Context, collectionName, fileID string) error {
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

func (s *ragService) DeleteCollection(ctx context.Context, collectionName string) error {
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

func (s *ragService) queryChroma(ctx context.Context, collectionID string, embedding []float64, topK int, fileIDs []string) ([]model.Evidence, error) {
	url := fmt.Sprintf("%s/api/v1/collections/%s/query", s.chromaURL, collectionID)
	
	payload := map[string]interface{}{
		"query_embeddings": [][]float64{embedding},
		"n_results":        topK,
		"include":          []string{"documents", "distances", "metadatas"},
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
		Documents [][]string           `json:"documents"`
		Distances [][]float64          `json:"distances"`
		Metadatas [][]map[string]interface{} `json:"metadatas"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var searchResults []model.Evidence
	if len(result.Documents) > 0 {
		for i, doc := range result.Documents[0] {
			dist := result.Distances[0][i]
			// Distance to Similarity (approx for L2/Cosine in Chroma)
			// Squared L2 distance is often < 1.0 for close matches.
			// 1.0 - dist/2 is a reasonable proxy for similarity.
			sim := 1.0 - (dist / 2.0)
			if sim < 0 { sim = 0 }

			source := "Local Files"
			if result.Metadatas != nil && i < len(result.Metadatas[0]) {
				if src, ok := result.Metadatas[0][i]["filename"].(string); ok {
					source = src
				}
			}

			searchResults = append(searchResults, model.Evidence{
				Content: doc,
				Source:  source,
				RelevanceScore: sim,
				FinalScore: sim, // Placeholder for v1
			})
		}
	}
	return searchResults, nil
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

func (s *ragService) ClusterEvidence(ctx context.Context, evidences []model.Evidence) ([][]model.Evidence, error) {
	if len(evidences) <= 1 {
		return [][]model.Evidence{evidences}, nil
	}

	embeddings := make([][]float64, len(evidences))
	for i, ev := range evidences {
		resp, err := s.ollamaClient.Embeddings(ctx, &ollama.EmbeddingsRequest{
			Model:  s.embeddingModel,
			Prompt: ev.Content,
		})
		if err != nil {
			return nil, err
		}
		embeddings[i] = resp.Embedding
	}

	var clusters [][]model.Evidence
	visited := make([]bool, len(evidences))

	for i := 0; i < len(evidences); i++ {
		if visited[i] {
			continue
		}

		cluster := []model.Evidence{evidences[i]}
		visited[i] = true

		for j := i + 1; j < len(evidences); j++ {
			if visited[j] {
				continue
			}

			sim := s.cosineSimilarity(embeddings[i], embeddings[j])
			if sim > 0.85 {
				cluster = append(cluster, evidences[j])
				visited[j] = true
			}
		}
		clusters = append(clusters, cluster)
	}

	return clusters, nil
}

func (s *ragService) cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
