package model

import "slices"

// LLMConfig describes a model from system_models.json.
type LLMConfig struct {
	Name          string `json:"name"`
	ModelName     string `json:"model_name"`
	Description   string `json:"description,omitempty"`
	ContextWindow int    `json:"context_window"`

	Architecture    string   `json:"architecture,omitempty"`
	ParametersCount string   `json:"parameters_count,omitempty"`
	EmbeddingLength int      `json:"embedding_length,omitempty"`
	Quantization    string   `json:"quantization,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`

	// Sampling defaults; nil means "use the model's own default".
	Temperature   *float64 `json:"temperature,omitempty"`
	TopK          *int     `json:"top_k,omitempty"`
	TopP          *float64 `json:"top_p,omitempty"`
	RepeatPenalty *float64 `json:"repeat_penalty,omitempty"`
	StopSequences []string `json:"stop_sequences,omitempty"`
}

func (c *LLMConfig) Has(capability string) bool {
	return slices.Contains(c.Capabilities, capability)
}

// NumCtx is the context size actually requested from Ollama: the model's
// window, capped so the KV cache fits on consumer GPUs.
func (c *LLMConfig) NumCtx(maxNumCtx int) int {
	if c.ContextWindow <= 0 || c.ContextWindow > maxNumCtx {
		return maxNumCtx
	}
	return c.ContextWindow
}

// Options builds the Ollama options for this model.
func (c *LLMConfig) Options(maxNumCtx int) map[string]any {
	opts := map[string]any{"num_ctx": c.NumCtx(maxNumCtx)}
	if c.Temperature != nil {
		opts["temperature"] = *c.Temperature
	}
	if c.TopK != nil {
		opts["top_k"] = *c.TopK
	}
	if c.TopP != nil {
		opts["top_p"] = *c.TopP
	}
	if c.RepeatPenalty != nil {
		opts["repeat_penalty"] = *c.RepeatPenalty
	}
	if len(c.StopSequences) > 0 {
		opts["stop"] = c.StopSequences
	}
	return opts
}
