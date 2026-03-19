package lmstudio_test

import (
	"aigit/lmstudio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamTokens_basic(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"feat: \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"update auth\"}}]}\n\n" +
		"data: [DONE]\n"

	var tokens []string
	msg, err := lmstudio.StreamTokens(strings.NewReader(stream), func(tok string) {
		tokens = append(tokens, tok)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "feat: update auth" {
		t.Fatalf("got %q want %q", msg, "feat: update auth")
	}
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens want 2", len(tokens))
	}
}

func TestClientCurrentModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"qwen/qwen3-4b"}]}`)
	}))
	defer srv.Close()

	client := lmstudio.NewClient(srv.URL)
	model, err := client.CurrentModel(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if model != "qwen/qwen3-4b" {
		t.Fatalf("got %q want %q", model, "qwen/qwen3-4b")
	}
}
