package ollama_test

import (
	"aigit/ollama"
	"encoding/json"
	"io"
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
	}, nil)
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

func TestStreamTokens_largeResponse(t *testing.T) {
	// Simulate a model that sends a single large chunk exceeding bufio.Scanner's
	// default 64 KB max token size. This reproduces the "token too long" error
	// that occurs with large multi-file diffs.
	largeToken := strings.Repeat("a", 70_000) // 70 KB — safely over the 64 KB limit
	line, _ := json.Marshal(map[string]any{"response": largeToken, "done": false})
	ndjson := string(line) + "\n" + "{\"response\":\"\",\"done\":true}\n"

	msg, err := ollama.StreamTokens(strings.NewReader(ndjson), func(string) {}, nil)
	if err != nil {
		t.Fatalf("unexpected error (likely bufio token too long): %v", err)
	}
	if msg != largeToken {
		t.Errorf("response length mismatch: got %d bytes, want %d bytes", len(msg), len(largeToken))
	}
}

func TestStreamTokens_ollamaError(t *testing.T) {
	ndjson := "{\"error\":\"model not found\",\"done\":false}\n"
	_, err := ollama.StreamTokens(strings.NewReader(ndjson), func(string) {}, nil)
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("expected model not found error, got %v", err)
	}
}

func TestStreamTokens_ignoresThinkingField(t *testing.T) {
	ndjson := "{\"thinking\":\"considering options\",\"response\":\"feat: add auth\",\"done\":false}\n" +
		"{\"response\":\"\",\"done\":true}\n"

	msg, err := ollama.StreamTokens(strings.NewReader(ndjson), func(string) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "feat: add auth" {
		t.Errorf("got %q want %q", msg, "feat: add auth")
	}
}

func TestStreamTokens_stripsThinkBlocksAcrossChunks(t *testing.T) {
	ndjson := "{\"response\":\"<th\",\"done\":false}\n" +
		"{\"response\":\"ink>internal reasoning\",\"done\":false}\n" +
		"{\"response\":\"</thi\",\"done\":false}\n" +
		"{\"response\":\"nk>fix login bug\",\"done\":false}\n" +
		"{\"response\":\"\",\"done\":true}\n"

	var tokens []string
	msg, err := ollama.StreamTokens(strings.NewReader(ndjson), func(tok string) {
		tokens = append(tokens, tok)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "fix login bug" {
		t.Errorf("got %q want %q", msg, "fix login bug")
	}
	if strings.Contains(strings.Join(tokens, ""), "internal reasoning") {
		t.Errorf("unexpected reasoning leaked into streamed tokens: %q", strings.Join(tokens, ""))
	}
}

func TestStreamTokens_reportsThinkingWithoutLeakingIt(t *testing.T) {
	ndjson := "{\"thinking\":\"planning\",\"response\":\"\",\"done\":false}\n" +
		"{\"response\":\"feat: improve auth flow\",\"done\":false}\n" +
		"{\"response\":\"\",\"done\":true}\n"

	thinkingSignals := 0
	msg, err := ollama.StreamTokens(strings.NewReader(ndjson), func(string) {}, func() {
		thinkingSignals++
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "feat: improve auth flow" {
		t.Errorf("got %q want %q", msg, "feat: improve auth flow")
	}
	if thinkingSignals != 1 {
		t.Errorf("got %d thinking signals want 1", thinkingSignals)
	}
}

func TestStreamTokens_stripsOrphanClosingThinkTag(t *testing.T) {
	ndjson := "{\"response\":\"</think>fix tests\",\"done\":false}\n" +
		"{\"response\":\"\",\"done\":true}\n"

	msg, err := ollama.StreamTokens(strings.NewReader(ndjson), func(string) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "fix tests" {
		t.Errorf("got %q want %q", msg, "fix tests")
	}
}

func TestClient_Generate(t *testing.T) {
	ndjson := "{\"response\":\"hello\",\"done\":false}\n{\"response\":\"\",\"done\":true}\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req map[string]any
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("parsing request body: %v", err)
		}
		if think, ok := req["think"].(bool); !ok || !think {
			t.Fatalf("expected think=true in request, got %#v", req["think"])
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

	msg, err := ollama.StreamTokens(body, func(string) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "hello" {
		t.Errorf("got %q want %q", msg, "hello")
	}
}

func TestNewClient_disablesResponseHeaderTimeout(t *testing.T) {
	client := ollama.NewClient("http://localhost:11434")

	transport, ok := client.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.HTTPClient.Transport)
	}
	if transport.ResponseHeaderTimeout != 0 {
		t.Fatalf("ResponseHeaderTimeout = %v, want 0", transport.ResponseHeaderTimeout)
	}
}

func TestClient_CurrentModel_prefersRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			w.Write([]byte(`{"models":[{"name":"llama3.2:latest"}]}`))
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen3:4b"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := ollama.NewClient(srv.URL)
	model, err := client.CurrentModel(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if model != "llama3.2:latest" {
		t.Errorf("got %q want %q", model, "llama3.2:latest")
	}
}

func TestClient_CurrentModel_fallsBackToInstalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			w.Write([]byte(`{"models":[]}`))
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen3:4b"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := ollama.NewClient(srv.URL)
	model, err := client.CurrentModel(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if model != "qwen3:4b" {
		t.Errorf("got %q want %q", model, "qwen3:4b")
	}
}
