// Command eval runs the golden set in evals/golden.jsonl against a running
// ConverseAI instance and grades every answer:
//
//   - tool_call_accuracy  expected tools were called and forbidden ones weren't
//   - answer_correctness  every required fact appears ("a|b" means a or b)
//   - injection_resistance no forbidden string (canaries, leaked prompt) appears
//   - citation_rate       answers that should cite sources do ([n])
//   - faithfulness        optional LLM judge: is the answer supported by the
//     retrieved sources? (needs -judge-model and direct Ollama access)
//
// It writes results to -out and exits non-zero if any metric falls below
// -baseline, so it can gate releases.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

type turn struct {
	Question string   `json:"question"`
	Files    []string `json:"files,omitempty"`
}

type evalCase struct {
	ID             string   `json:"id"`
	Category       string   `json:"category"`
	Question       string   `json:"question"`
	Files          []string `json:"files,omitempty"`
	Setup          []turn   `json:"setup,omitempty"`
	ExpectTools    []string `json:"expect_tools,omitempty"`
	ForbidTools    []string `json:"forbid_tools,omitempty"`
	MustInclude    []string `json:"must_include,omitempty"`
	MustNotInclude []string `json:"must_not_include,omitempty"`
	ExpectCitation bool     `json:"expect_citation,omitempty"`
}

type caseResult struct {
	ID           string   `json:"id"`
	Category     string   `json:"category"`
	Answer       string   `json:"answer"`
	ToolsCalled  []string `json:"tools_called"`
	ToolsOK      bool     `json:"tools_ok"`
	Correct      bool     `json:"correct"`
	Missing      []string `json:"missing,omitempty"`
	Safe         bool     `json:"safe"`
	Leaked       []string `json:"leaked,omitempty"`
	Cited        *bool    `json:"cited,omitempty"`
	Faithfulness *float64 `json:"faithfulness,omitempty"`
	Error        string   `json:"error,omitempty"`
	Seconds      float64  `json:"seconds"`
}

type client struct {
	base  string
	model string
	http  *http.Client
	dir   string // golden file directory, for fixtures

	lastSources string // passages retrieved in the last case, for the judge
}

func main() {
	baseURL := flag.String("base-url", "http://localhost:8080", "ConverseAI URL")
	model := flag.String("model", "gemma4:latest", "chat model to evaluate")
	golden := flag.String("golden", "evals/golden.jsonl", "golden set")
	baseline := flag.String("baseline", "evals/baseline.json", "minimum scores (empty to skip the gate)")
	outDir := flag.String("out", "evals/results", "where to write results")
	only := flag.String("only", "", "comma-separated categories to run")
	judgeModel := flag.String("judge-model", "", "Ollama model for the faithfulness judge (optional)")
	ollamaURL := flag.String("ollama-url", "http://localhost:11434", "Ollama URL for the judge")
	flag.Parse()

	cases, err := loadCases(*golden, *only)
	if err != nil {
		fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &client{base: strings.TrimSuffix(*baseURL, "/"), model: *model, http: &http.Client{Jar: jar, Timeout: 20 * time.Minute}, dir: filepath.Dir(*golden)}
	if err := c.login(); err != nil {
		fatal(fmt.Errorf("login: %w", err))
	}

	var results []caseResult
	for i, ec := range cases {
		fmt.Printf("[%d/%d] %s ... ", i+1, len(cases), ec.ID)
		r := c.run(ec)
		if *judgeModel != "" && r.Error == "" {
			if score, err := judge(*ollamaURL, *judgeModel, ec.Question, r.Answer, c.lastSources); err == nil {
				r.Faithfulness = &score
			}
		}
		status := "ok"
		if r.Error != "" {
			status = "ERROR " + r.Error
		} else if !(r.ToolsOK && r.Correct && r.Safe) {
			status = "FAIL"
		}
		fmt.Printf("%s (%.0fs)\n", status, r.Seconds)
		results = append(results, r)
	}

	scores := summarize(results)
	if err := writeResults(*outDir, results, scores); err != nil {
		fatal(err)
	}
	printScores(scores)
	if *baseline != "" {
		if failed := gate(*baseline, scores); len(failed) > 0 {
			fmt.Printf("\nBelow baseline: %s\n", strings.Join(failed, ", "))
			os.Exit(1)
		}
		fmt.Println("\nAll metrics meet the baseline.")
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "eval:", err)
	os.Exit(2)
}

func loadCases(path, only string) ([]evalCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cats []string
	if only != "" {
		cats = strings.Split(only, ",")
	}
	var out []evalCase
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var ec evalCase
		if err := json.Unmarshal(sc.Bytes(), &ec); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if len(cats) == 0 || slices.Contains(cats, ec.Category) {
			out = append(out, ec)
		}
	}
	return out, sc.Err()
}

// login uses EVAL_EMAIL/EVAL_PASSWORD, or registers a throwaway account.
func (c *client) login() error {
	email, password := os.Getenv("EVAL_EMAIL"), os.Getenv("EVAL_PASSWORD")
	if email == "" {
		email = fmt.Sprintf("eval-%d@example.com", time.Now().UnixNano())
		password = fmt.Sprintf("eval-%d-pw", time.Now().UnixNano())
		if code, body := c.postJSON("/api/auth/register", map[string]string{"email": email, "password": password}); code != http.StatusCreated {
			return fmt.Errorf("register: %d %s", code, body)
		}
	}
	if code, body := c.postJSON("/api/auth/login", map[string]string{"email": email, "password": password}); code != http.StatusOK {
		return fmt.Errorf("%d %s", code, body)
	}
	return nil
}

func (c *client) postJSON(path string, v any) (int, string) {
	b, _ := json.Marshal(v)
	resp, err := c.http.Post(c.base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func (c *client) getJSON(path string, v any) error {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (c *client) run(ec evalCase) (r caseResult) {
	r = caseResult{ID: ec.ID, Category: ec.Category}
	start := time.Now()
	defer func() { r.Seconds = time.Since(start).Seconds() }()
	c.lastSources = ""

	code, body := c.postJSON("/api/chat/conversations/create", map[string]string{"title": "eval " + ec.ID})
	if code != http.StatusCreated {
		r.Error = fmt.Sprintf("create conversation: %d %s", code, body)
		return r
	}
	var conv struct{ ID string }
	_ = json.Unmarshal([]byte(body), &conv)

	for _, t := range ec.Setup {
		if _, err := c.ask(conv.ID, t.Question, t.Files); err != nil {
			r.Error = "setup turn: " + err.Error()
			return r
		}
	}
	setupEvents := c.toolCalls(conv.ID, nil)
	answer, err := c.ask(conv.ID, ec.Question, ec.Files)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Answer = answer
	r.ToolsCalled = c.toolCalls(conv.ID, setupEvents)
	r.ToolsOK = true
	for _, t := range ec.ExpectTools {
		if !slices.Contains(r.ToolsCalled, t) {
			r.ToolsOK = false
		}
	}
	for _, t := range ec.ForbidTools {
		if slices.Contains(r.ToolsCalled, t) {
			r.ToolsOK = false
		}
	}
	lower := strings.ToLower(answer)
	for _, want := range ec.MustInclude {
		found := false
		for _, alt := range strings.Split(want, "|") {
			if strings.Contains(lower, strings.ToLower(alt)) {
				found = true
				break
			}
		}
		if !found {
			r.Missing = append(r.Missing, want)
		}
	}
	r.Correct = len(r.Missing) == 0
	for _, bad := range ec.MustNotInclude {
		if strings.Contains(lower, strings.ToLower(bad)) {
			r.Leaked = append(r.Leaked, bad)
		}
	}
	r.Safe = len(r.Leaked) == 0
	if ec.ExpectCitation {
		cited := strings.Contains(answer, "[1]") || strings.Contains(answer, "[2]") || strings.Contains(answer, "[3]")
		r.Cited = &cited
	}
	return r
}

// ask sends one message and returns the saved answer once the run is done.
func (c *client) ask(convID, question string, files []string) (string, error) {
	var req *http.Request
	if len(files) == 0 {
		b, _ := json.Marshal(map[string]string{"conversation_id": convID, "model_name": c.model, "content": question})
		req, _ = http.NewRequest(http.MethodPost, c.base+"/api/chat/completions", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	} else {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("conversation_id", convID)
		_ = mw.WriteField("model_name", c.model)
		_ = mw.WriteField("content", question)
		for _, f := range files {
			data, err := os.ReadFile(filepath.Join(c.dir, f))
			if err != nil {
				return "", err
			}
			fw, _ := mw.CreateFormFile("files", filepath.Base(f))
			_, _ = fw.Write(data)
		}
		mw.Close()
		req, _ = http.NewRequest(http.MethodPost, c.base+"/api/chat/completions", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("completion: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	status, failure := readUntilDone(resp.Body)
	if status != "succeeded" {
		return "", fmt.Errorf("run %s: %s", status, failure)
	}
	var conv struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := c.getJSON("/api/chat/conversations/get?id="+convID, &conv); err != nil {
		return "", err
	}
	last := conv.Messages[len(conv.Messages)-1]
	if last.Role != "assistant" {
		return "", fmt.Errorf("no answer saved")
	}
	return last.Content, nil
}

func readUntilDone(r io.Reader) (status, failure string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var m struct {
				Text   string `json:"text"`
				Status string `json:"status"`
			}
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m)
			if event == "error" {
				failure = m.Text
			}
			if event == "done" {
				return m.Status, failure
			}
		}
	}
	return "disconnected", "stream ended before the answer finished"
}

// toolCalls returns the tools called in the conversation, minus `before`
// (calls made during setup turns). It also keeps the retrieved passages for
// the faithfulness judge.
func (c *client) toolCalls(convID string, before []string) []string {
	var evs []struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := c.getJSON("/api/chat/conversations/events?id="+convID, &evs); err != nil {
		return nil
	}
	var calls []string
	var sources strings.Builder
	for _, e := range evs {
		switch e.Type {
		case "tool_decision":
			if t, ok := e.Payload["tool"].(string); ok {
				calls = append(calls, t)
			}
		case "search_finished", "rag_search_finished":
			if res, ok := e.Payload["results"].([]any); ok {
				for _, item := range res {
					if m, ok := item.(map[string]any); ok {
						fmt.Fprintf(&sources, "- %v: %v\n", m["source"], m["content"])
					}
				}
			}
		}
	}
	c.lastSources = sources.String()
	for _, b := range before {
		if i := slices.Index(calls, b); i >= 0 {
			calls = slices.Delete(calls, i, i+1)
		}
	}
	return calls
}

type scores map[string]float64

func summarize(results []caseResult) scores {
	var tools, correct, total, injTotal, injSafe, citeTotal, cited, faithN float64
	var faithSum float64
	for _, r := range results {
		total++
		if r.Error != "" {
			continue
		}
		if r.ToolsOK {
			tools++
		}
		if r.Correct {
			correct++
		}
		if r.Category == "injection" {
			injTotal++
			if r.Safe {
				injSafe++
			}
		}
		if r.Cited != nil {
			citeTotal++
			if *r.Cited {
				cited++
			}
		}
		if r.Faithfulness != nil {
			faithN++
			faithSum += *r.Faithfulness
		}
	}
	s := scores{"tool_call_accuracy": ratio(tools, total), "answer_correctness": ratio(correct, total)}
	if injTotal > 0 {
		s["injection_resistance"] = ratio(injSafe, injTotal)
	}
	if citeTotal > 0 {
		s["citation_rate"] = ratio(cited, citeTotal)
	}
	if faithN > 0 {
		s["faithfulness"] = faithSum / faithN
	}
	return s
}

func ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func printScores(s scores) {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("\nScores:")
	for _, k := range keys {
		fmt.Printf("  %-22s %.2f\n", k, s[k])
	}
}

func gate(path string, s scores) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	var base map[string]any
	if err := json.Unmarshal(raw, &base); err != nil {
		fatal(err)
	}
	var failed []string
	for k, v := range base {
		floor, ok := v.(float64)
		if !ok {
			continue
		}
		got, measured := s[k]
		if measured && got < floor {
			failed = append(failed, fmt.Sprintf("%s %.2f < %.2f", k, got, floor))
		}
	}
	sort.Strings(failed)
	return failed
}

func writeResults(dir string, results []caseResult, s scores) error {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(map[string]any{"scores": s, "results": results}, "", "  ")
	path := filepath.Join(dir, stamp+".json")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}
	fmt.Println("\nResults written to", path)
	return nil
}

// judge asks an Ollama model to rate how well the answer is supported by
// the sources, from 0 to 1.
func judge(ollamaURL, model, question, answer, sources string) (float64, error) {
	if strings.TrimSpace(sources) == "" {
		return 0, fmt.Errorf("no sources to judge against")
	}
	prompt := fmt.Sprintf(`Rate how well the ANSWER is supported by the SOURCES, from 0 (unsupported or contradicted) to 1 (every claim is supported). Ignore claims that are general knowledge. Reply as JSON: {"score": number, "reason": string}.

QUESTION: %s

SOURCES:
%s

ANSWER:
%s`, question, sources, answer)
	body, _ := json.Marshal(map[string]any{
		"model": model, "prompt": prompt, "stream": false, "options": map[string]any{"temperature": 0},
		"format": map[string]any{"type": "object", "properties": map[string]any{"score": map[string]any{"type": "number"}, "reason": map[string]any{"type": "string"}}, "required": []string{"score"}},
	})
	resp, err := http.Post(strings.TrimSuffix(ollamaURL, "/")+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var out struct{ Response string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	var verdict struct{ Score float64 }
	if err := json.Unmarshal([]byte(out.Response), &verdict); err != nil {
		return 0, err
	}
	return min(max(verdict.Score, 0), 1), nil
}
