package handler

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-chat/internal/service"
)

func TestSSEKeepsNewlinesInsideOneFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	s := newSSE(rec)
	if err := s.event("delta", map[string]string{"text": "line one\n\ndata: fake\nline two"}); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 3 || lines[0] != "event: delta" || lines[2] != "" {
		t.Fatalf("frame %q", lines)
	}
	var got map[string]string
	_ = json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &got)
	if got["text"] != "line one\n\ndata: fake\nline two" {
		t.Fatalf("text %q", got["text"])
	}
}

func TestWriteErrorHidesInternals(t *testing.T) {
	cases := []struct {
		err  error
		code int
		body string
	}{
		{&service.ValidationError{Msg: "title must not be empty"}, 400, "title must not be empty"},
		{service.ErrNotFound, 404, "Not found"},
		{fmt.Errorf("wrap: %w", service.ErrInvalidCredentials), 401, "invalid email or password"},
		{errors.New("mongo: connection refused at 10.0.0.3"), 500, "Internal server error"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeError(rec, httptest.NewRequest("GET", "/x", nil), c.err)
		if rec.Code != c.code || strings.TrimSpace(rec.Body.String()) != c.body {
			t.Errorf("%v: got %d %q", c.err, rec.Code, rec.Body.String())
		}
	}
}

func TestDecodeJSONRequiresContentType(t *testing.T) {
	var v map[string]any
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "text/plain")
	if err := decodeJSON(httptest.NewRecorder(), req, &v); !errors.Is(err, errUnsupportedMedia) {
		t.Fatalf("got %v", err)
	}
	req = httptest.NewRequest("POST", "/", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if err := decodeJSON(httptest.NewRecorder(), req, &v); err != nil || v["a"] != float64(1) {
		t.Fatalf("got %v %v", v, err)
	}
}
