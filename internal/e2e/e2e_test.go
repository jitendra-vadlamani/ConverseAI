//go:build integration

// End-to-end tests against real MongoDB, MinIO and Chroma with a fake model.
// Start the services with `docker compose -f docker-compose.ci.yml up -d`
// and run `go test -tags=integration ./internal/e2e/`.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-chat/internal/app"
	"ai-chat/internal/config"
	"ai-chat/internal/model"
	"ai-chat/internal/ollama"
	"ai-chat/internal/testutil"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	os.Setenv("JWT_SECRET", "e2e-test-secret-e2e-test-secret-0123456789")
	os.Setenv("DB_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	os.Setenv("MINIO_ROOT_USER", env("MINIO_ROOT_USER", "ciuser"))
	os.Setenv("MINIO_ROOT_PASSWORD", env("MINIO_ROOT_PASSWORD", "ci-password-123"))
	os.Setenv("MINIO_ENDPOINT", env("MINIO_ENDPOINT", "localhost:19000"))
	os.Setenv("MONGO_URI", env("MONGO_URI", "mongodb://localhost:27019"))
	os.Setenv("CHROMA_URL", env("CHROMA_URL", "http://localhost:18000"))
	os.Setenv("DB_NAME", fmt.Sprintf("e2e_%d", time.Now().UnixNano()))
	os.Setenv("MINIO_BUCKET", fmt.Sprintf("e2e-%d", time.Now().UnixNano()))
	os.Setenv("COOKIE_SECURE", "false")
	os.Setenv("DEFAULT_CHAT_MODEL", "gemma4:latest")
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

type fakeSearch struct{}

func (fakeSearch) Search(context.Context, string) ([]model.Evidence, error) {
	return []model.Evidence{{ID: "r1", Content: "Go 1.24 was released in February 2025 with generic type aliases.",
		Source: "go.dev", URL: "https://go.dev/doc/go1.24", AuthorityScore: 1, FreshnessScore: 0.9}}, nil
}

func (fakeSearch) FetchPageContent(context.Context, string) (string, error) {
	return "", fmt.Errorf("offline")
}

type env2e struct {
	t    *testing.T
	cfg  *config.Config
	llm  *testutil.FakeOllama
	app  *app.App
	srv  *httptest.Server
	db   *mongo.Database
	stop context.CancelFunc
}

func start(t *testing.T, cfg *config.Config, llm *testutil.FakeOllama) *env2e {
	t.Helper()
	ctx := context.Background()
	a, err := app.New(ctx, cfg, nil, app.Overrides{Ollama: llm, Search: fakeSearch{}})
	if err != nil {
		t.Fatalf("app: %v", err)
	}
	bg, stop := context.WithCancel(ctx)
	go a.Runs.Background(bg)
	srv := httptest.NewServer(a.Handler)
	client, _ := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	e := &env2e{t: t, cfg: cfg, llm: llm, app: a, srv: srv, db: client.Database(cfg.DBName), stop: stop}
	t.Cleanup(func() { e.close() })
	return e
}

var closeOnce sync.Map

func (e *env2e) close() {
	if _, done := closeOnce.LoadOrStore(e, true); done {
		return
	}
	e.stop()
	e.srv.Close()
	e.app.Close(context.Background())
}

type user struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newUser(t *testing.T, e *env2e, email string) *user {
	jar, _ := cookiejar.New(nil)
	u := &user{t: t, base: e.srv.URL, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	if code, body := u.do("POST", "/api/auth/register", map[string]string{"email": email, "password": "password-123"}); code != 201 {
		t.Fatalf("register: %d %s", code, body)
	}
	if code, body := u.do("POST", "/api/auth/login", map[string]string{"email": strings.ToUpper(email), "password": "password-123"}); code != 200 {
		t.Fatalf("login: %d %s", code, body)
	}
	return u
}

func (u *user) do(method, path string, body any) (int, []byte) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u.base+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := u.client.Do(req)
	if err != nil {
		u.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (u *user) newConversation() string {
	code, body := u.do("POST", "/api/chat/conversations/create", map[string]string{"title": "test"})
	if code != 201 {
		u.t.Fatalf("create: %d %s", code, body)
	}
	var c struct{ ID string }
	_ = json.Unmarshal(body, &c)
	return c.ID
}

type sseEvent struct {
	Name string
	Msg  map[string]any
}

// ask sends a message (with optional files) and reads the SSE stream to the end.
func (u *user) ask(convID, text string, files map[string]string) (int, []sseEvent, string) {
	var req *http.Request
	if len(files) == 0 {
		b, _ := json.Marshal(map[string]string{"conversation_id": convID, "model_name": "gemma4:latest", "content": text})
		req, _ = http.NewRequest("POST", u.base+"/api/chat/completions", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	} else {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("conversation_id", convID)
		_ = mw.WriteField("model_name", "gemma4:latest")
		_ = mw.WriteField("content", text)
		for name, content := range files {
			fw, _ := mw.CreateFormFile("files", name)
			_, _ = fw.Write([]byte(content))
		}
		mw.Close()
		req, _ = http.NewRequest("POST", u.base+"/api/chat/completions", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
	}
	resp, err := u.client.Do(req)
	if err != nil {
		u.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, nil, string(b)
	}
	evs := readSSE(resp.Body)
	var answer strings.Builder
	for _, ev := range evs {
		switch ev.Name {
		case "delta":
			answer.WriteString(str(ev.Msg["text"]))
		case "reset":
			answer.Reset()
			answer.WriteString(str(ev.Msg["text"]))
		}
	}
	return 200, evs, answer.String()
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func readSSE(r io.Reader) []sseEvent {
	var evs []sseEvent
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var name string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var m map[string]any
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m)
			evs = append(evs, sseEvent{Name: name, Msg: m})
			if name == "done" {
				return evs
			}
		}
	}
	return evs
}

func (u *user) conversation(id string) model.Conversation {
	code, body := u.do("GET", "/api/chat/conversations/get?id="+id, nil)
	if code != 200 {
		u.t.Fatalf("get conversation: %d %s", code, body)
	}
	var c model.Conversation
	_ = json.Unmarshal(body, &c)
	return c
}

func TestChatFlowAndIsolation(t *testing.T) {
	llm := &testutil.FakeOllama{OnChat: func(req *ollama.ChatRequest) (*ollama.ChatResult, error) {
		return testutil.Text("Line one.\n\nLine two with **markdown**."), nil
	}}
	e := start(t, testConfig(t), llm)
	alice := newUser(t, e, "alice@example.com")
	bob := newUser(t, e, "bob@example.com")

	conv := alice.newConversation()
	code, evs, answer := alice.ask(conv, "Hello there", nil)
	if code != 200 {
		t.Fatalf("ask: %d %s", code, answer)
	}
	if answer != "Line one.\n\nLine two with **markdown**." {
		t.Fatalf("newlines must survive SSE, got %q", answer)
	}
	if last := evs[len(evs)-1]; last.Name != "done" || last.Msg["status"] != "succeeded" {
		t.Fatalf("expected done/succeeded, got %+v", last)
	}

	c := alice.conversation(conv)
	if len(c.Messages) != 2 || c.Messages[0].Content != "Hello there" || c.ActiveRunID != nil {
		t.Fatalf("unexpected conversation: %+v", c)
	}

	// Bob can't read, write, subscribe to, rename or delete Alice's chat.
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/chat/conversations/get?id=" + conv},
		{"GET", "/api/chat/conversations/events?id=" + conv},
		{"GET", "/api/chat/conversations/events/stream?id=" + conv},
		{"GET", "/api/chat/conversations/files?id=" + conv},
		{"DELETE", "/api/chat/conversations/delete?id=" + conv},
	} {
		if code, _ := bob.do(tc.method, tc.path, nil); code != 404 {
			t.Errorf("%s %s as other user: got %d, want 404", tc.method, tc.path, code)
		}
	}
	if code, _ := bob.do("PATCH", "/api/chat/conversations/title", map[string]string{"id": conv, "title": "pwned"}); code != 404 {
		t.Errorf("rename other's conversation: got %d", code)
	}
	if code, _, _ := bob.ask(conv, "inject", nil); code != 404 {
		t.Errorf("writing into another user's conversation: got %d, want 404", code)
	}

	// Events are encrypted at rest.
	var raw bson.M
	if err := e.db.Collection("events").FindOne(context.Background(), bson.M{}).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if enc, _ := raw["payload_enc"].(string); !strings.HasPrefix(enc, "v2:") {
		t.Errorf("event payload not encrypted: %v", raw)
	}
	var rawConv bson.M
	_ = e.db.Collection("conversations").FindOne(context.Background(), bson.M{}).Decode(&rawConv)
	if b, _ := bson.MarshalExtJSON(rawConv, false, false); bytes.Contains(b, []byte("Hello there")) {
		t.Error("message stored in plaintext")
	}
}

func TestFilesRAGAndDownload(t *testing.T) {
	var mu sync.Mutex
	var prompts []string
	llm := &testutil.FakeOllama{OnChat: func(req *ollama.ChatRequest) (*ollama.ChatResult, error) {
		mu.Lock()
		prompts = append(prompts, req.Messages[len(req.Messages)-1].Content)
		mu.Unlock()
		return testutil.Text("The project codename is BLUEBIRD [1]."), nil
	}}
	e := start(t, testConfig(t), llm)
	alice := newUser(t, e, "alice@example.com")
	conv := alice.newConversation()

	doc := "Internal memo. The project codename is BLUEBIRD. Launch is planned for March."
	code, _, answer := alice.ask(conv, "What is the codename?", map[string]string{"memo.txt": doc, "../../evil<script>.html": "<script>alert(1)</script>"})
	if code != 200 {
		t.Fatalf("ask with files: %d %s", code, answer)
	}
	if !strings.Contains(prompts[0], "BLUEBIRD") || !strings.Contains(prompts[0], "<untrusted_data") {
		t.Fatalf("file text should be fenced in the prompt, got: %s", prompts[0])
	}

	c := alice.conversation(conv)
	if c.Messages[0].Content != "What is the codename?" {
		t.Fatalf("stored user message must be only what was typed, got %q", c.Messages[0].Content)
	}
	if !strings.Contains(c.Messages[1].Content, "**Sources**") || !strings.Contains(c.Messages[1].Content, "memo.txt") {
		t.Fatalf("answer should list cited sources: %q", c.Messages[1].Content)
	}

	// Second turn: no re-ingestion, retrieval still finds the file.
	embedded := e.llm.Embedded
	code, _, _ = alice.ask(conv, "When is the launch?", nil)
	if code != 200 {
		t.Fatal("second turn failed")
	}
	if !strings.Contains(prompts[1], "March") {
		t.Errorf("retrieval should bring the memo back on later turns: %s", prompts[1])
	}
	if e.llm.Embedded-embedded > 2 { // only query embeddings
		t.Errorf("files were re-embedded on the second turn (%d new embeddings)", e.llm.Embedded-embedded)
	}

	// Files tab and proxied download; HTML is never served inline.
	code, body := alice.do("GET", "/api/chat/conversations/files?id="+conv, nil)
	var files []string
	_ = json.Unmarshal(body, &files)
	if code != 200 || len(files) != 2 {
		t.Fatalf("files: %d %s", code, body)
	}
	for _, f := range files {
		req, _ := http.NewRequest("GET", e.srv.URL+"/api/chat/files/download?fileID="+f, nil)
		resp, err := alice.client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("download %s: %v %v", f, err, resp.StatusCode)
		}
		resp.Body.Close()
		if strings.HasSuffix(f, ".html") {
			if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") || resp.Header.Get("Content-Type") != "application/octet-stream" {
				t.Errorf("html must download as attachment: %v", resp.Header)
			}
			if strings.Contains(f, "..") || strings.Contains(f, "<") {
				t.Errorf("filename not sanitized: %s", f)
			}
		}
	}
	bob := newUser(t, e, "bob@example.com")
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/chat/files/download?fileID="+files[0], nil)
	if resp, _ := bob.client.Do(req); resp.StatusCode != 404 {
		t.Errorf("other user downloaded a file: %d", resp.StatusCode)
	}

	// Deleting the conversation purges unreferenced files.
	if code, _ := alice.do("DELETE", "/api/chat/conversations/delete?id="+conv, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	req, _ = http.NewRequest("GET", e.srv.URL+"/api/chat/files/download?fileID="+files[0], nil)
	if resp, _ := alice.client.Do(req); resp.StatusCode != 404 {
		t.Errorf("file should be gone after deleting its only conversation: %d", resp.StatusCode)
	}
}

func TestToolCallingWithCitations(t *testing.T) {
	llm := &testutil.FakeOllama{OnChat: func(req *ollama.ChatRequest) (*ollama.ChatResult, error) {
		if !testutil.HasToolResult(req) {
			if len(req.Tools) == 0 {
				return nil, fmt.Errorf("tools were not offered")
			}
			return testutil.ToolCall("web_search", map[string]any{"query": "latest Go release"}), nil
		}
		return testutil.Text("Go 1.24 added generic type aliases [1]."), nil
	}}
	e := start(t, testConfig(t), llm)
	alice := newUser(t, e, "alice@example.com")
	conv := alice.newConversation()
	code, _, answer := alice.ask(conv, "What's new in the latest Go release?", nil)
	if code != 200 || !strings.Contains(answer, "[1]") {
		t.Fatalf("ask: %d %q", code, answer)
	}
	c := alice.conversation(conv)
	if !strings.Contains(c.Messages[1].Content, "https://go.dev/doc/go1.24") {
		t.Errorf("sources footer should link the web result: %q", c.Messages[1].Content)
	}
	tool := llm.LastChat().Messages
	if !strings.Contains(tool[len(tool)-1].Content, "<untrusted_data") {
		t.Errorf("tool output must be fenced: %q", tool[len(tool)-1].Content)
	}
}

func TestRateLimitAndSecrets(t *testing.T) {
	e := start(t, testConfig(t), &testutil.FakeOllama{})
	jar, _ := cookiejar.New(nil)
	u := &user{t: t, base: e.srv.URL, client: &http.Client{Jar: jar}}
	limited := false
	for i := 0; i < 12; i++ {
		if code, _ := u.do("POST", "/api/auth/login", map[string]string{"email": "x@example.com", "password": "wrong-password"}); code == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("login is not rate limited")
	}
	// Cross-origin POSTs are refused.
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/auth/logout", nil)
	req.Header.Set("Origin", "https://evil.example")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 403 {
		t.Errorf("cross-origin POST allowed: %d", resp.StatusCode)
	}
	// Form-encoded bodies are refused on JSON endpoints.
	req, _ = http.NewRequest("POST", e.srv.URL+"/api/auth/register", strings.NewReader("email=a@b.co&password=password-123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 415 {
		t.Errorf("form post to JSON endpoint: %d", resp.StatusCode)
	}
}

func TestCancelAndResumeAfterRestart(t *testing.T) {
	cfg := testConfig(t)
	block := make(chan struct{})
	llm := &testutil.FakeOllama{Block: block}
	e1 := start(t, cfg, llm)
	alice := newUser(t, e1, "alice@example.com")

	// Cancel: the stop button.
	conv := alice.newConversation()
	go func() {
		time.Sleep(500 * time.Millisecond)
		var c model.Conversation
		for i := 0; i < 20 && c.ActiveRunID == nil; i++ {
			c = alice.conversation(conv)
			time.Sleep(100 * time.Millisecond)
		}
		alice.do("POST", "/api/chat/runs/cancel?id="+c.ActiveRunID.Hex(), nil)
	}()
	_, evs, _ := alice.ask(conv, "this will be stopped", nil)
	if last := evs[len(evs)-1]; last.Msg["status"] != "cancelled" {
		t.Fatalf("expected cancelled, got %+v", last)
	}

	// Restart: a run in flight when the server stops is resumed by the next one.
	conv2 := alice.newConversation()
	go alice.ask(conv2, "survive a restart", nil)
	var runID primitive.ObjectID
	for i := 0; i < 50; i++ {
		if c := alice.conversation(conv2); c.ActiveRunID != nil {
			runID = *c.ActiveRunID
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if runID.IsZero() {
		t.Fatal("run did not start")
	}
	time.Sleep(300 * time.Millisecond)
	e1.close() // graceful shutdown: run is released, not failed

	var r bson.M
	_ = e1.db.Collection("runs").FindOne(context.Background(), bson.M{"_id": runID}).Decode(&r)
	if r["status"] != string(model.RunRunning) {
		t.Fatalf("run should still be resumable after shutdown, got %v", r["status"])
	}

	llm2 := &testutil.FakeOllama{OnChat: func(*ollama.ChatRequest) (*ollama.ChatResult, error) {
		return testutil.Text("Resumed answer."), nil
	}}
	e2 := start(t, cfg, llm2)
	alice.base = e2.srv.URL
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		c := alice.conversation(conv2)
		if c.ActiveRunID == nil && len(c.Messages) == 2 {
			if c.Messages[1].Content != "Resumed answer." {
				t.Fatalf("unexpected resumed answer %q", c.Messages[1].Content)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("run was not resumed after restart")
}

func TestDeleteAccount(t *testing.T) {
	e := start(t, testConfig(t), &testutil.FakeOllama{})
	alice := newUser(t, e, "alice@example.com")
	conv := alice.newConversation()
	alice.ask(conv, "hi", map[string]string{"a.txt": "some text"})
	if code, _ := alice.do("DELETE", "/api/auth/account", map[string]string{"password": "wrong"}); code != 401 {
		t.Fatalf("delete without the right password: %d", code)
	}
	if code, body := alice.do("DELETE", "/api/auth/account", map[string]string{"password": "password-123"}); code != 204 {
		t.Fatalf("delete account: %d %s", code, body)
	}
	for _, coll := range []string{"users", "conversations", "events", "runs"} {
		if n, _ := e.db.Collection(coll).CountDocuments(context.Background(), bson.M{}); n != 0 {
			t.Errorf("%s still has %d documents", coll, n)
		}
	}
}
