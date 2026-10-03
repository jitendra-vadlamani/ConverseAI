// Command fakeollama serves a scripted subset of the Ollama HTTP API for
// browser end-to-end tests and offline development. It never calls a real
// model.
//
// Chat behaviour, by the text of the last user message:
//   - contains "slow": streams "word 1 ... word 40" with a delay per word
//     (for testing stop and reconnect)
//   - has attached file text: answers "I read your file: <first line of it>"
//   - anything else: a fixed two-paragraph markdown answer
//
// Tools are never called, so no web access is needed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ai-chat/internal/testutil"
)

const defaultAnswer = "Hello from the **test model**.\n\nThis is the second paragraph."

var fenced = regexp.MustCompile(`(?s)<untrusted_data source="[^"]*">\n(.*?)\n</untrusted_data>`)

func main() {
	addr := flag.String("addr", "127.0.0.1:11998", "listen address")
	delay := flag.Duration("slow-delay", 250*time.Millisecond, "delay per word in slow mode")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"version": "fake"})
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"capabilities": []string{"completion", "tools", "vision"}})
	})
	mux.HandleFunc("POST /api/embed", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		vecs := make([][]float64, len(req.Input))
		for i, in := range req.Input {
			vecs[i] = testutil.HashEmbedding(in)
		}
		writeJSON(w, map[string]any{"embeddings": vecs})
	})
	mux.HandleFunc("POST /api/generate", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"response": "Summary of the earlier conversation.", "done": true, "eval_count": 8})
	})
	mux.HandleFunc("POST /api/chat", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		last := ""
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				last = req.Messages[i].Content
				break
			}
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		send := func(content string, done bool) bool {
			line := map[string]any{"message": map[string]string{"role": "assistant", "content": content}, "done": done}
			if done {
				line["prompt_eval_count"], line["eval_count"] = 120, 30
			}
			if err := json.NewEncoder(w).Encode(line); err != nil {
				return false
			}
			if flusher != nil {
				flusher.Flush()
			}
			return true
		}

		question := strings.SplitN(last, "\n\n---\n", 2)[0]
		switch {
		case strings.Contains(strings.ToLower(question), "slow"):
			for i := 1; i <= 40; i++ {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(*delay):
				}
				if !send(fmt.Sprintf("word %d ", i), false) {
					return
				}
			}
		case fenced.MatchString(last):
			text := strings.TrimSpace(fenced.FindStringSubmatch(last)[1])
			first, _, _ := strings.Cut(text, "\n")
			send("I read your file: "+first, false)
		default:
			for _, piece := range strings.SplitAfter(defaultAnswer, " ") {
				send(piece, false)
			}
		}
		send("", true)
	})

	log.Printf("fake ollama listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
