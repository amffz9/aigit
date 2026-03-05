package ollama_test

import (
	"aigit/ollama"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamTokens_basic(t *testing.T) {
	ndjson := "{\"response\":\"fix \",\"done\":false}\n" +
		"{\"response\":\"the bug\",\"done\":false}\n" +
		"{\"response\":\"\",\"done\":true}\n"

	var tokens []string
	msg, err := ollama.StreamTokens(strings.NewReader(ndjson), func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "fix the bug" {
		t.Errorf("got %q want %q", msg, "fix the bug")
	}
	if len(tokens) != 2 {
		t.Errorf("got %d tokens want 2", len(tokens))
	}
}

func TestStreamTokens_ollamaError(t *testing.T) {
	ndjson := "{\"error\":\"model not found\",\"done\":false}\n"
	_, err := ollama.StreamTokens(strings.NewReader(ndjson), func(string) {})
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("expected model not found error, got %v", err)
	}
}

func TestClient_Generate(t *testing.T) {
	ndjson := "{\"response\":\"hello\",\"done\":false}\n{\"response\":\"\",\"done\":true}\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(ndjson))
	}))
	defer srv.Close()

	client := ollama.NewClient(srv.URL)
	body, err := client.Generate(t.Context(), "test-model", "test prompt")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()

	msg, err := ollama.StreamTokens(body, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "hello" {
		t.Errorf("got %q want %q", msg, "hello")
	}
}
