// Package rag indexes uploaded documents in Chroma and retrieves evidence
// with hybrid (vector + BM25) ranking.
package rag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
)

const (
	embedBatch = 16
	// candidates fetched from the vector index before lexical re-ranking
	candidatePool = 30
)

// Doc is a piece of text to rank in memory (used for web pages, which are
// never written to the index).
type Doc struct {
	ID, Source, URL string
	Content         string
	Authority       float64
	Freshness       float64
}

type Service interface {
	// IngestFile indexes a file's text once. load is only called when the
	// file isn't indexed yet, so calling this on every turn is cheap. It
	// returns the number of chunks written (0 if already indexed).
	IngestFile(ctx context.Context, userID, fileID, filename string, load func() (string, error)) (int, error)
	// Search returns the best chunks from the given files only.
	Search(ctx context.Context, userID, query string, topK int, fileIDs []string) ([]model.Evidence, error)
	// Rank chunks docs, embeds them and returns the topK chunks by hybrid
	// score together with their embeddings (for clustering).
	Rank(ctx context.Context, query string, docs []Doc, topK int) ([]model.Evidence, [][]float64, error)
	DeleteFile(ctx context.Context, userID, fileID string) error
	DeleteUser(ctx context.Context, userID string) error
	Ping(ctx context.Context) error
}

type service struct {
	chroma         *chroma
	ollama         ollama.Client
	embeddingModel string
}

func NewService(chromaURL, tenant, database string, client ollama.Client, embeddingModel string) Service {
	return &service{chroma: newChroma(chromaURL, tenant, database), ollama: client, embeddingModel: embeddingModel}
}

func collectionFor(userID string) string { return "user-knowledge-" + userID }

func (s *service) Ping(ctx context.Context) error { return s.chroma.heartbeat(ctx) }

func (s *service) embed(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += embedBatch {
		end := min(start+embedBatch, len(texts))
		vecs, err := s.ollama.Embed(ctx, s.embeddingModel, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embed with %s: %w", s.embeddingModel, err)
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (s *service) IngestFile(ctx context.Context, userID, fileID, filename string, load func() (string, error)) (int, error) {
	collID, err := s.chroma.ensureCollection(ctx, collectionFor(userID))
	if err != nil {
		return 0, fmt.Errorf("ensure collection: %w", err)
	}
	where := map[string]any{"file_id": fileID}
	if n, err := s.chroma.count(ctx, collID, where); err != nil {
		return 0, fmt.Errorf("check existing chunks: %w", err)
	} else if n > 0 {
		return 0, nil
	}

	text, err := load()
	if err != nil {
		return 0, fmt.Errorf("load %s: %w", filename, err)
	}
	chunks := ChunkText(text)
	if len(chunks) == 0 {
		return 0, nil
	}
	vecs, err := s.embed(ctx, chunks)
	if err != nil {
		return 0, err
	}
	ids := make([]string, len(chunks))
	metas := make([]map[string]any, len(chunks))
	for i := range chunks {
		// Chunk ids are derived from the file id, so a retried ingest
		// overwrites rather than duplicates.
		ids[i] = fmt.Sprintf("%s#%d", fileID, i)
		metas[i] = map[string]any{"file_id": fileID, "filename": filename, "chunk_idx": i}
	}
	for start := 0; start < len(chunks); start += 100 {
		end := min(start+100, len(chunks))
		if err := s.chroma.upsert(ctx, collID, ids[start:end], vecs[start:end], chunks[start:end], metas[start:end]); err != nil {
			return start, fmt.Errorf("upsert chunks: %w", err)
		}
	}
	slog.Info("ingested file", "file", fileID, "chunks", len(chunks))
	return len(chunks), nil
}

func (s *service) Search(ctx context.Context, userID, query string, topK int, fileIDs []string) ([]model.Evidence, error) {
	if len(fileIDs) == 0 || query == "" {
		return nil, nil
	}
	collID, err := s.chroma.collectionID(ctx, collectionFor(userID))
	if errors.Is(err, errCollectionNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find collection: %w", err)
	}
	vecs, err := s.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	res, err := s.chroma.query(ctx, collID, vecs[0], candidatePool, map[string]any{"file_id": map[string]any{"$in": fileIDs}})
	if err != nil {
		return nil, fmt.Errorf("query chroma: %w", err)
	}
	if len(res.Documents) == 0 || len(res.Documents[0]) == 0 {
		return nil, nil
	}
	docs := res.Documents[0]
	cands := make([]model.Evidence, len(docs))
	vectorOrder := make([]int, len(docs))
	for i, doc := range docs {
		ev := model.Evidence{Content: doc, Source: "uploaded file", RelevanceScore: 1 - res.Distances[0][i]}
		if len(res.IDs) > 0 && i < len(res.IDs[0]) {
			ev.ID = res.IDs[0][i]
		}
		if len(res.Metadatas) > 0 && i < len(res.Metadatas[0]) {
			if name, ok := res.Metadatas[0][i]["filename"].(string); ok {
				ev.Source = name
			}
			if fid, ok := res.Metadatas[0][i]["file_id"].(string); ok {
				ev.FileID = fid
			}
		}
		ev.FinalScore = ev.RelevanceScore
		cands[i] = ev
		vectorOrder[i] = i // Chroma returns nearest first
	}
	order := fuseRanks(vectorOrder, bm25Scores(query, docs))
	out := make([]model.Evidence, 0, topK)
	for _, idx := range order {
		if len(out) == topK {
			break
		}
		out = append(out, cands[idx])
	}
	return out, nil
}

func (s *service) Rank(ctx context.Context, query string, docs []Doc, topK int) ([]model.Evidence, [][]float64, error) {
	var cands []model.Evidence
	var texts []string
	for _, d := range docs {
		for i, chunk := range ChunkText(d.Content) {
			cands = append(cands, model.Evidence{
				ID: fmt.Sprintf("%s#%d", d.ID, i), Content: chunk, Source: d.Source, URL: d.URL,
				AuthorityScore: d.Authority, FreshnessScore: d.Freshness,
			})
			texts = append(texts, chunk)
		}
	}
	if len(texts) == 0 {
		return nil, nil, nil
	}
	vecs, err := s.embed(ctx, append([]string{query}, texts...))
	if err != nil {
		return nil, nil, err
	}
	qv, chunkVecs := vecs[0], vecs[1:]
	vectorOrder := make([]int, len(cands))
	for i := range cands {
		cands[i].RelevanceScore = CosineSimilarity(qv, chunkVecs[i])
		vectorOrder[i] = i
	}
	sort.SliceStable(vectorOrder, func(a, b int) bool {
		return cands[vectorOrder[a]].RelevanceScore > cands[vectorOrder[b]].RelevanceScore
	})
	order := fuseRanks(vectorOrder, bm25Scores(query, texts))
	var out []model.Evidence
	var outVecs [][]float64
	for _, idx := range order {
		if len(out) == topK {
			break
		}
		out = append(out, cands[idx])
		outVecs = append(outVecs, chunkVecs[idx])
	}
	return out, outVecs, nil
}

func (s *service) DeleteFile(ctx context.Context, userID, fileID string) error {
	collID, err := s.chroma.collectionID(ctx, collectionFor(userID))
	if errors.Is(err, errCollectionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.chroma.deleteWhere(ctx, collID, map[string]any{"file_id": fileID})
}

func (s *service) DeleteUser(ctx context.Context, userID string) error {
	return s.chroma.deleteCollection(ctx, collectionFor(userID))
}

// ClusterByEmbedding groups indices whose vectors have cosine similarity
// above threshold with the cluster's first member.
func ClusterByEmbedding(vecs [][]float64, threshold float64) [][]int {
	visited := make([]bool, len(vecs))
	var clusters [][]int
	for i := range vecs {
		if visited[i] {
			continue
		}
		visited[i] = true
		cluster := []int{i}
		for j := i + 1; j < len(vecs); j++ {
			if !visited[j] && CosineSimilarity(vecs[i], vecs[j]) > threshold {
				visited[j] = true
				cluster = append(cluster, j)
			}
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}
