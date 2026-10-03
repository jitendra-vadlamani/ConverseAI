package model

// Evidence is one ranked snippet from a document or web page.
type Evidence struct {
	ID             string  `json:"id"`
	FileID         string  `json:"file_id,omitempty"`
	Content        string  `json:"content"`
	Source         string  `json:"source"`
	URL            string  `json:"url,omitempty"`
	RelevanceScore float64 `json:"relevance_score"`
	AuthorityScore float64 `json:"authority_score"`
	FreshnessScore float64 `json:"freshness_score"`
	FinalScore     float64 `json:"final_score"`
	IsConflicting  bool    `json:"is_conflicting"`
	ConflictReason string  `json:"conflict_reason,omitempty"`
}
