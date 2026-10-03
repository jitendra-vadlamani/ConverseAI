package rag

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// tokenize lower-cases and splits on anything that isn't a letter or digit.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// bm25Scores scores each doc against the query with Okapi BM25, using the
// candidate set itself as the corpus for document frequencies.
func bm25Scores(query string, docs []string) []float64 {
	const k1, b = 1.2, 0.75
	qTerms := uniq(tokenize(query))
	scores := make([]float64, len(docs))
	if len(qTerms) == 0 || len(docs) == 0 {
		return scores
	}
	docTerms := make([]map[string]int, len(docs))
	lengths := make([]int, len(docs))
	df := map[string]int{}
	total := 0
	for i, d := range docs {
		tf := map[string]int{}
		toks := tokenize(d)
		for _, t := range toks {
			tf[t]++
		}
		for t := range tf {
			df[t]++
		}
		docTerms[i], lengths[i] = tf, len(toks)
		total += len(toks)
	}
	avg := float64(total) / float64(len(docs))
	if avg == 0 {
		return scores
	}
	n := float64(len(docs))
	for i := range docs {
		for _, q := range qTerms {
			f := float64(docTerms[i][q])
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(df[q])+0.5)/(float64(df[q])+0.5))
			scores[i] += idf * f * (k1 + 1) / (f + k1*(1-b+b*float64(lengths[i])/avg))
		}
	}
	return scores
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// fuseRanks combines a vector ranking and a BM25 ranking with reciprocal rank
// fusion and returns candidate indices, best first. vectorOrder is the
// candidates' order by vector similarity (index 0 = most similar).
func fuseRanks(vectorOrder []int, bm25 []float64) []int {
	const k = 60.0
	fused := make(map[int]float64, len(vectorOrder))
	for rank, idx := range vectorOrder {
		fused[idx] += 1 / (k + float64(rank+1))
	}
	lexOrder := make([]int, len(bm25))
	for i := range lexOrder {
		lexOrder[i] = i
	}
	sort.SliceStable(lexOrder, func(a, b int) bool { return bm25[lexOrder[a]] > bm25[lexOrder[b]] })
	for rank, idx := range lexOrder {
		if bm25[idx] > 0 {
			fused[idx] += 1 / (k + float64(rank+1))
		}
	}
	out := make([]int, 0, len(fused))
	for idx := range fused {
		out = append(out, idx)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if fused[out[a]] != fused[out[b]] {
			return fused[out[a]] > fused[out[b]]
		}
		return out[a] < out[b]
	})
	return out
}

// CosineSimilarity of two equal-length vectors; 0 if either is zero.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
