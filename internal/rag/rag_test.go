package rag

import (
	"context"
	"strings"
	"testing"

	"ai-chat/internal/testutil"
)

func TestChunkTextRespectsSizeAndOverlap(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		sb.WriteString("This is sentence number ")
		sb.WriteString(strings.Repeat("x", i%7+1))
		sb.WriteString(". ")
		if i%40 == 39 {
			sb.WriteString("\n\n")
		}
	}
	chunks := ChunkText(sb.String())
	if len(chunks) < 3 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if n := len(strings.Fields(c)); n > maxWords {
			t.Errorf("chunk %d has %d words", i, n)
		}
	}
	// Consecutive chunks share overlap.
	tail := strings.Fields(chunks[0])
	if !strings.HasPrefix(chunks[1], strings.Join(tail[len(tail)-overlapWords:], " ")) {
		t.Error("expected overlap between chunk 0 and 1")
	}
	if ChunkText("   ") != nil {
		t.Error("blank text should give no chunks")
	}
	if got := ChunkText("short text"); len(got) != 1 || got[0] != "short text" {
		t.Errorf("short text: %v", got)
	}
}

func TestChunkTextLongSentence(t *testing.T) {
	long := strings.Repeat("word ", maxWords*2+10)
	for _, c := range ChunkText(long) {
		if len(strings.Fields(c)) > maxWords {
			t.Fatal("a single long sentence must still be split")
		}
	}
}

func TestBM25PrefersMatchingDocs(t *testing.T) {
	docs := []string{"the cat sat on the mat", "kafka pulsar streaming comparison", "pulsar is a streaming platform by apache"}
	s := bm25Scores("pulsar streaming", docs)
	if !(s[2] > 0 && s[1] > 0 && s[0] == 0) {
		t.Fatalf("scores %v", s)
	}
}

func TestFuseRanksCombinesSignals(t *testing.T) {
	// Vector order puts doc 0 first, BM25 strongly prefers doc 2.
	order := fuseRanks([]int{0, 1, 2}, []float64{0, 0, 5})
	if order[0] != 0 && order[0] != 2 {
		t.Fatalf("fused order %v", order)
	}
	if len(order) != 3 {
		t.Fatalf("all candidates must be kept: %v", order)
	}
}

func TestRankReturnsRelevantChunkFirst(t *testing.T) {
	s := &service{ollama: &testutil.FakeOllama{}, embeddingModel: "fake"}
	docs := []Doc{
		{ID: "a", Source: "a.com", URL: "https://a.com", Content: "Recipes for banana bread and muffins.", Authority: 0.4, Freshness: 0.7},
		{ID: "b", Source: "b.com", URL: "https://b.com", Content: "Kubernetes pods are scheduled onto nodes by the scheduler.", Authority: 1, Freshness: 0.9},
	}
	got, vecs, err := s.Rank(context.Background(), "how does the kubernetes scheduler place pods", docs, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].URL != "https://b.com" || got[0].AuthorityScore != 1 || got[0].FreshnessScore != 0.9 {
		t.Fatalf("authority/freshness must carry through ranking: %+v", got[0])
	}
	if len(vecs) != len(got) {
		t.Fatal("one vector per result")
	}
}

func TestClusterByEmbedding(t *testing.T) {
	vecs := [][]float64{{1, 0}, {0.99, 0.01}, {0, 1}}
	clusters := ClusterByEmbedding(vecs, 0.9)
	if len(clusters) != 2 || len(clusters[0]) != 2 || clusters[0][1] != 1 {
		t.Fatalf("clusters %v", clusters)
	}
}

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 || EstimateTokens(strings.Repeat("word ", 100)) < 100 {
		t.Error("estimate off")
	}
}
