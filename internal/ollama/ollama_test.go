package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatStreamsContentAndToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream || len(req.Tools) != 1 {
			t.Errorf("unexpected request %+v", req)
		}
		io.WriteString(w, `{"message":{"role":"assistant","content":"Hel","thinking":"hmm"},"done":false}`+"\n")
		io.WriteString(w, `{"message":{"role":"assistant","content":"lo\nworld"},"done":false}`+"\n")
		io.WriteString(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"web_search","arguments":{"query":"x"}}}]},"done":false}`+"\n")
		io.WriteString(w, `{"message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":12,"eval_count":3}`+"\n")
	}))
	defer srv.Close()

	var chunks []string
	res, err := NewClient(srv.URL).Chat(context.Background(), &ChatRequest{Model: "m", Tools: []Tool{{Type: "function"}}}, func(c ChatChunk) {
		chunks = append(chunks, c.Content)
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Content != "Hello\nworld" || res.Message.Thinking != "hmm" || res.PromptEvalCount != 12 || res.EvalCount != 3 {
		t.Fatalf("result %+v", res)
	}
	if len(res.Message.ToolCalls) != 1 || res.Message.ToolCalls[0].Function.Arguments["query"] != "x" {
		t.Fatalf("tool calls %+v", res.Message.ToolCalls)
	}
	if strings.Join(chunks, "") != "Hello\nworld" {
		t.Fatalf("chunks %q", chunks)
	}
}

func TestChatReportsTruncatedStreamAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":{"content":"partial"},"done":false}`+"\n")
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if _, err := c.Chat(context.Background(), &ChatRequest{}, nil); err == nil || !strings.Contains(err.Error(), "before completion") {
		t.Fatalf("a stream without done must be an error, got %v", err)
	}

	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":{"content":"par"},"done":false}`+"\n")
		io.WriteString(w, `{"error":"model crashed"}`+"\n")
	}))
	defer errSrv.Close()
	if _, err := NewClient(errSrv.URL).Chat(context.Background(), &ChatRequest{}, nil); err == nil || !strings.Contains(err.Error(), "model crashed") {
		t.Fatalf("an error line mid-stream must surface, got %v", err)
	}
}

func TestAPIErrorsAreTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		io.WriteString(w, `{"error":"model 'x' not found"}`)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL).Generate(context.Background(), &GenerateRequest{Model: "x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 || apiErr.Message != "model 'x' not found" {
		t.Fatalf("got %v", err)
	}
}

func TestUnloadSendsExplicitZeroKeepAlive(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"done":true}`)
	}))
	defer srv.Close()
	if err := NewClient(srv.URL).Unload(context.Background(), "m"); err != nil {
		t.Fatal(err)
	}
	if v, ok := body["keep_alive"]; !ok || v != float64(0) {
		t.Fatalf("keep_alive must be sent as 0, body %v", body)
	}
}

func TestEmbedBatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Input []string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		out := make([][]float64, len(req.Input))
		for i := range out {
			out[i] = []float64{float64(i)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
	}))
	defer srv.Close()
	vecs, err := NewClient(srv.URL).Embed(context.Background(), "e", []string{"a", "b"})
	if err != nil || len(vecs) != 2 || vecs[1][0] != 1 {
		t.Fatalf("%v %v", vecs, err)
	}
}
