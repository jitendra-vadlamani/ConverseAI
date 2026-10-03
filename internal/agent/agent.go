// Package agent runs a single tool-calling agent loop on Ollama's native
// tool API: the model streams an answer, may call tools, sees their results
// and answers. It replaces the old keyword router + JSON planner.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"time"

	"ai-chat/internal/manager"
	"ai-chat/internal/metrics"
	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/rag"
	"ai-chat/internal/repository"
	"ai-chat/internal/search"
	"ai-chat/internal/util"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	// MaxToolRounds bounds the loop; after this many rounds the model must answer.
	MaxToolRounds   = 5
	maxCallsPerStep = 4
	toolTimeout     = 90 * time.Second
	maxToolOutput   = 16000
)

var tracer = otel.Tracer("ai-chat/agent")

// FileGetter loads attachment bytes.
type FileGetter interface {
	Get(ctx context.Context, fileID string) ([]byte, error)
}

// Deps are the services the agent and its tools use.
type Deps struct {
	Ollama           ollama.Client
	Models           manager.ModelManager
	RAG              rag.Service
	Search           search.Service
	Files            FileGetter
	Catalog          repository.SystemLLMRepository
	MaxNumCtx        int
	OCRModel         string
	TranslationModel string
}

// options returns the catalog options for a model with overrides applied.
func (d *Deps) options(modelName string, overrides map[string]any) map[string]any {
	opts := map[string]any{"num_ctx": d.MaxNumCtx}
	if meta := d.Catalog.GetMetadata(modelName); meta != nil {
		opts = meta.Options(d.MaxNumCtx)
	}
	maps.Copy(opts, overrides)
	return opts
}

// generate is a non-streaming call under a model lease, with metrics and a span.
func (d *Deps) generate(ctx context.Context, kind, modelName string, req *ollama.GenerateRequest, overrides map[string]any) (*ollama.GenerateResponse, error) {
	ctx, span := tracer.Start(ctx, "llm."+kind)
	defer span.End()
	span.SetAttributes(attribute.String("llm.model", modelName))

	release, err := d.Models.Acquire(ctx, modelName)
	if err != nil {
		return nil, err
	}
	defer release()

	req.Model = modelName
	req.Options = d.options(modelName, overrides)
	start := time.Now()
	resp, err := d.Ollama.Generate(ctx, req)
	observeLLM(modelName, kind, start, err)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	countTokens(modelName, resp.PromptEvalCount, resp.EvalCount)
	return resp, nil
}

func observeLLM(modelName, kind string, start time.Time, err error) {
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	metrics.LLMCallDuration.WithLabelValues(modelName, kind, outcome).Observe(time.Since(start).Seconds())
}

func countTokens(modelName string, prompt, completion int) {
	metrics.LLMTokens.WithLabelValues(modelName, "prompt").Add(float64(prompt))
	metrics.LLMTokens.WithLabelValues(modelName, "completion").Add(float64(completion))
}

// Summarize condenses conversation text for long-term memory.
func (d *Deps) Summarize(ctx context.Context, modelName, previousSummary, transcript string) (string, int, error) {
	prompt := "Write a concise summary of this conversation for your own long-term memory. Keep names, numbers, decisions, open questions and file names. Output only the summary.\n\n"
	if previousSummary != "" {
		prompt += "Summary of the conversation before this part:\n" + previousSummary + "\n\n"
	}
	prompt += "Conversation:\n" + transcript
	resp, err := d.generate(ctx, "summarize", modelName, &ollama.GenerateRequest{Prompt: prompt}, map[string]any{"temperature": 0.2})
	if err != nil {
		return "", 0, err
	}
	summary := strings.TrimSpace(resp.Response)
	if summary == "" {
		return "", 0, errors.New("model returned an empty summary")
	}
	return summary, resp.EvalCount, nil
}

// State is everything needed to continue a run after a restart. It is
// persisted after each tool round.
type State struct {
	Round      int                  `json:"round"`
	Steps      []ollama.ChatMessage `json:"steps"`
	Sources    []Source             `json:"sources"`
	Answer     string               `json:"answer"` // answer text from completed rounds
	Reasoning  string               `json:"reasoning"`
	Seen       map[string]int       `json:"seen"` // tool call fingerprints
	ModelsUsed []string             `json:"models_used"`
	Prompt     int                  `json:"prompt_tokens"`
	Completion int                  `json:"completion_tokens"`
}

func (s *State) addModel(m string) {
	for _, x := range s.ModelsUsed {
		if x == m {
			return
		}
	}
	s.ModelsUsed = append(s.ModelsUsed, m)
}

// Request is one agent run.
type Request struct {
	Model *model.LLMConfig
	// Base is the conversation so far: system prompt, history, user turn.
	Base     []ollama.ChatMessage
	UseTools bool
	State    *State // non-nil when resuming

	OnDelta   func(string)
	OnThought func(string)
	// OnRetry is called before a failed model call is retried, so text
	// already streamed from the failed attempt can be withdrawn.
	OnRetry func(answerSoFar string)
	// OnRound is called after each completed tool round with the state to persist.
	OnRound func(*State) error
}

type Result struct {
	Content    string
	Reasoning  string
	Sources    []Source
	State      *State
	LastPrompt int // prompt tokens of the final call (context size)
	LastOutput int
}

type Agent struct{ deps *Deps }

func New(deps *Deps) *Agent { return &Agent{deps: deps} }

func (a *Agent) Deps() *Deps { return a.deps }

func (a *Agent) Run(ctx context.Context, rc *RunContext, req Request) (*Result, error) {
	ctx, span := tracer.Start(ctx, "agent.run")
	defer span.End()
	span.SetAttributes(attribute.String("llm.model", req.Model.ModelName), attribute.Bool("agent.tools", req.UseTools))

	st := req.State
	if st == nil {
		st = &State{}
	}
	if st.Seen == nil {
		st.Seen = map[string]int{}
	}
	if len(st.Sources) > 0 {
		// Resuming: keep the citation numbers the earlier rounds used.
		rc.setSources(st.Sources)
	}

	var tools []Tool
	var toolDefs []ollama.Tool
	if req.UseTools {
		tools = a.deps.ToolsFor(rc)
		for _, t := range tools {
			toolDefs = append(toolDefs, toolDefinition(t))
		}
	}
	byName := map[string]Tool{}
	for _, t := range tools {
		byName[t.Name()] = t
	}

	for {
		offerTools := len(toolDefs) > 0 && st.Round < MaxToolRounds
		msgs := append(append([]ollama.ChatMessage{}, req.Base...), st.Steps...)
		if len(toolDefs) > 0 && !offerTools {
			msgs = append(msgs, ollama.ChatMessage{Role: "system",
				Content: "You have used all available tool calls. Write the final answer now from the information gathered."})
		}

		res, err := a.chat(ctx, req, msgs, toolDefsIf(offerTools, toolDefs), st.Answer)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		st.addModel(req.Model.ModelName)
		st.Prompt += res.PromptEvalCount
		st.Completion += res.EvalCount
		content, reasoning := res.Message.Content, res.Message.Thinking
		st.Reasoning += reasoning

		calls := res.Message.ToolCalls
		if !offerTools || len(calls) == 0 {
			answer := st.Answer + content
			return &Result{
				Content: answer, Reasoning: st.Reasoning, Sources: rc.Sources(), State: st,
				LastPrompt: res.PromptEvalCount, LastOutput: res.EvalCount,
			}, nil
		}

		// Text streamed before the tool calls was already shown to the user.
		st.Answer += content
		if len(calls) > maxCallsPerStep {
			calls = calls[:maxCallsPerStep]
		}
		st.Steps = append(st.Steps, ollama.ChatMessage{Role: "assistant", Content: content, ToolCalls: calls})
		for _, call := range calls {
			out := a.runTool(ctx, rc, byName, st, call)
			st.Steps = append(st.Steps, ollama.ChatMessage{Role: "tool", ToolName: call.Function.Name, Content: out})
		}
		st.Round++
		st.Sources = rc.Sources()
		if req.OnRound != nil {
			if err := req.OnRound(st); err != nil {
				return nil, err
			}
		}
	}
}

func toolDefsIf(ok bool, defs []ollama.Tool) []ollama.Tool {
	if ok {
		return defs
	}
	return nil
}

// chat streams one model call under a lease. If the model fails while tools
// are offered (some models emit malformed tool calls), it retries once with
// the error fed back, then once without tools.
func (a *Agent) chat(ctx context.Context, req Request, msgs []ollama.ChatMessage, tools []ollama.Tool, answerSoFar string) (*ollama.ChatResult, error) {
	attempt := func(msgs []ollama.ChatMessage, tools []ollama.Tool) (*ollama.ChatResult, error) {
		ctx, span := tracer.Start(ctx, "llm.chat")
		defer span.End()
		span.SetAttributes(attribute.String("llm.model", req.Model.ModelName), attribute.Int("llm.tools", len(tools)))

		release, err := a.deps.Models.Acquire(ctx, req.Model.ModelName)
		if err != nil {
			return nil, err
		}
		defer release()

		splitter := &thinkSplitter{}
		start := time.Now()
		res, err := a.deps.Ollama.Chat(ctx, &ollama.ChatRequest{
			Model: req.Model.ModelName, Messages: msgs, Tools: tools,
			Options: req.Model.Options(a.deps.MaxNumCtx),
		}, func(c ollama.ChatChunk) {
			if c.Thinking != "" && req.OnThought != nil {
				req.OnThought(c.Thinking)
			}
			if c.Content != "" {
				answer, thought := splitter.Feed(c.Content)
				if thought != "" && req.OnThought != nil {
					req.OnThought(thought)
				}
				if answer != "" && req.OnDelta != nil {
					req.OnDelta(answer)
				}
			}
		})
		observeLLM(req.Model.ModelName, "chat", start, err)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		answer, thought := splitter.Flush()
		if thought != "" && req.OnThought != nil {
			req.OnThought(thought)
		}
		if answer != "" && req.OnDelta != nil {
			req.OnDelta(answer)
		}
		// Content may have carried <think> tags; keep only the answer part.
		res.Message.Content = splitter.Answer()
		res.Message.Thinking += splitter.Thought()
		countTokens(req.Model.ModelName, res.PromptEvalCount, res.EvalCount)
		span.SetAttributes(attribute.Int("llm.prompt_tokens", res.PromptEvalCount), attribute.Int("llm.completion_tokens", res.EvalCount))
		return res, nil
	}

	res, err := attempt(msgs, tools)
	if err == nil || len(tools) == 0 || ctx.Err() != nil || !retryable(err) {
		return res, err
	}
	slog.Warn("model call with tools failed, retrying", "model", req.Model.ModelName, "err", err)
	if req.OnRetry != nil {
		req.OnRetry(answerSoFar)
	}
	feedback := append(append([]ollama.ChatMessage{}, msgs...), ollama.ChatMessage{Role: "system",
		Content: fmt.Sprintf("Your previous reply could not be processed (%s). If you call a tool, use valid JSON arguments that match its schema.", truncate(err.Error(), 300))})
	if res, err = attempt(feedback, tools); err == nil {
		return res, nil
	}
	slog.Warn("retrying without tools", "model", req.Model.ModelName, "err", err)
	if req.OnRetry != nil {
		req.OnRetry(answerSoFar)
	}
	return attempt(msgs, nil)
}

func retryable(err error) bool {
	var apiErr *ollama.APIError
	return errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status != 404
}

func (a *Agent) runTool(ctx context.Context, rc *RunContext, byName map[string]Tool, st *State, call ollama.ToolCall) string {
	name := call.Function.Name
	args := call.Function.Arguments
	if args == nil {
		args = map[string]any{}
	}
	argsJSON, _ := json.Marshal(sortedArgs(args))
	rc.emit(model.EventToolDecision, map[string]any{
		"tool": name, "arguments": args, "round": st.Round + 1,
		"message": fmt.Sprintf("Model called %s", name),
	})

	tool, ok := byName[name]
	if !ok {
		metrics.ToolCalls.WithLabelValues("unknown", "invalid_args").Inc()
		return fmt.Sprintf("Error: there is no tool named %q. Available tools: %s.", name, strings.Join(sortedKeys(byName), ", "))
	}
	if err := ValidateArgs(tool.Parameters(), args); err != nil {
		metrics.ToolCalls.WithLabelValues(name, "invalid_args").Inc()
		return fmt.Sprintf("Error: invalid arguments for %s: %s. Fix the arguments and call it again.", name, err)
	}
	fingerprint := name + string(argsJSON)
	if st.Seen[fingerprint] > 0 {
		metrics.ToolCalls.WithLabelValues(name, "repeated").Inc()
		return "You already called this tool with these exact arguments; use the earlier result instead of calling it again."
	}
	st.Seen[fingerprint]++

	ctx, span := tracer.Start(ctx, "tool."+name)
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()

	rc.emit(model.EventTaskStarted, map[string]any{"tool": name, "message": "Running " + name})
	start := time.Now()
	out, err := tool.Run(ctx, rc, args)
	metrics.RunStageDuration.WithLabelValues("tool_" + name).Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.ToolCalls.WithLabelValues(name, "error").Inc()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		rc.emit(model.EventTaskFinished, map[string]any{"tool": name, "success": false, "error": err.Error(), "message": name + " failed"})
		return fmt.Sprintf("Error: %s failed: %s", name, err)
	}
	metrics.ToolCalls.WithLabelValues(name, "ok").Inc()
	rc.emit(model.EventTaskFinished, map[string]any{"tool": name, "success": true, "message": name + " finished"})
	return truncate(out, maxToolOutput)
}

func sortedArgs(args map[string]any) [][2]any {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][2]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]any{k, args[k]})
	}
	return out
}

func sortedKeys(m map[string]Tool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return util.TruncateRunes(s, n) + "\n[truncated]"
}
