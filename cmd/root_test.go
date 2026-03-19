package cmd

import (
	"aigit/lmstudio"
	"aigit/review"
	"aigit/runtimecheck"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type generateRecorder struct {
	mu         sync.Mutex
	count      int
	lastModel  string
	lastPrompt string
}

type lmStudioRecorder struct {
	mu         sync.Mutex
	count      int
	lastModel  string
	lastSystem string
	lastUser   string
}

func newTestOllamaServer(t *testing.T, response string) (*httptest.Server, *generateRecorder) {
	t.Helper()

	recorder := &generateRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"test-model"}]}`))
			return
		case "/api/generate":
		default:
			http.NotFound(w, r)
			return
		}

		var req struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		recorder.mu.Lock()
		recorder.count++
		recorder.lastModel = req.Model
		recorder.lastPrompt = req.Prompt
		recorder.mu.Unlock()

		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"response":"` + response + `","done":false}` + "\n"))
		_, _ = w.Write([]byte(`{"response":"","done":true}` + "\n"))
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, recorder
}

func newTestLMStudioServer(t *testing.T, response string) (*httptest.Server, *lmStudioRecorder) {
	t.Helper()

	recorder := &lmStudioRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"lmstudio-default"}]}`))
		case "/v1/chat/completions":
			var req struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode lm studio request: %v", err)
			}
			recorder.mu.Lock()
			recorder.count++
			recorder.lastModel = req.Model
			for _, msg := range req.Messages {
				switch msg.Role {
				case "system":
					recorder.lastSystem = msg.Content
				case "user":
					recorder.lastUser = msg.Content
				}
			}
			recorder.mu.Unlock()

			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + response + `"}}]}` + "\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n"))
		default:
			http.NotFound(w, r)
		}
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, recorder
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	return dir
}

func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

func recorderSnapshot(rec *generateRecorder) (count int, model, prompt string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.count, rec.lastModel, rec.lastPrompt
}

func lmStudioSnapshot(rec *lmStudioRecorder) (count int, model, system, user string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.count, rec.lastModel, rec.lastSystem, rec.lastUser
}

func TestRun_rejectsConflictingStageModes(t *testing.T) {
	var stdout, stderr bytes.Buffer

	exitCode := Run([]string{"--all", "--dir", ".", "foo.go"}, strings.NewReader(""), &stdout, &stderr)

	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "choose only one staging mode") {
		t.Fatalf("stderr = %q, want staging mode error", stderr.String())
	}
}

func TestRun_allStagesUntrackedFiles(t *testing.T) {
	repoDir := initRepo(t)
	chdirForTest(t, repoDir)
	writeRepoFile(t, repoDir, "new.go", "package main\n")

	srv, recorder := newTestOllamaServer(t, "feat: add new file")
	var stdout, stderr bytes.Buffer

	exitCode := Run([]string{"--all", "--dry-run", "--model", "test-model", "--url", srv.URL}, strings.NewReader(""), &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", exitCode, stderr.String())
	}

	count, model, prompt := recorderSnapshot(recorder)
	if count != 1 {
		t.Fatalf("generate requests = %d, want 1", count)
	}
	if model != "test-model" {
		t.Fatalf("model = %q, want test-model", model)
	}
	if !strings.Contains(prompt, "package main") {
		t.Fatalf("prompt missing staged file content:\n%s", prompt)
	}
}

func TestRun_configFlagLoadsOverrides(t *testing.T) {
	repoDir := initRepo(t)
	chdirForTest(t, repoDir)
	writeRepoFile(t, repoDir, "config.go", "package main\n")
	runGitCmd(t, repoDir, "add", "config.go")

	srv, recorder := newTestOllamaServer(t, "feat: use config file")
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := map[string]string{
		"model":  "cfg-model",
		"url":    srv.URL,
		"prompt": "Configured prompt text.",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"--config", cfgPath, "--dry-run"}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", exitCode, stderr.String())
	}

	count, model, prompt := recorderSnapshot(recorder)
	if count != 1 {
		t.Fatalf("generate requests = %d, want 1", count)
	}
	if model != "cfg-model" {
		t.Fatalf("model = %q, want cfg-model", model)
	}
	if !strings.Contains(prompt, "Configured prompt text.") {
		t.Fatalf("prompt missing configured text:\n%s", prompt)
	}
	if !strings.Contains(prompt, "STAGED REVIEW SUMMARY") || !strings.Contains(prompt, "STAGED FILES") {
		t.Fatalf("prompt missing structured review context:\n%s", prompt)
	}
	if !strings.Contains(prompt, promptSafetyPreamble) || !strings.Contains(prompt, "BEGIN UNTRUSTED GIT DIFF") {
		t.Fatalf("prompt missing safety wrapper:\n%s", prompt)
	}
}

func TestRun_warnsOnLargeDiffButContinues(t *testing.T) {
	repoDir := initRepo(t)
	chdirForTest(t, repoDir)
	writeRepoFile(t, repoDir, "large.txt", strings.Repeat("a", 60_000))

	srv, recorder := newTestOllamaServer(t, "chore: add large fixture")
	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"--all", "--dry-run", "--model", "test-model", "--url", srv.URL}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", exitCode, stderr.String())
	}

	count, _, _ := recorderSnapshot(recorder)
	if count != 1 {
		t.Fatalf("generate requests = %d, want 1", count)
	}
	if !strings.Contains(stdout.String(), "warning: diff is large (>50 KB)") {
		t.Fatalf("stdout missing diff warning:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Review summary:") {
		t.Fatalf("stdout missing staged review summary:\n%s", stdout.String())
	}
}

func TestRun_rejectsOversizedDiffBeforeGeneration(t *testing.T) {
	repoDir := initRepo(t)
	chdirForTest(t, repoDir)
	writeRepoFile(t, repoDir, "huge.txt", strings.Repeat("b", 210_000))

	srv, recorder := newTestOllamaServer(t, "unused")
	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"--all", "--dry-run", "--model", "test-model", "--url", srv.URL}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}

	count, _, _ := recorderSnapshot(recorder)
	if count != 0 {
		t.Fatalf("generate requests = %d, want 0", count)
	}
	if !strings.Contains(stderr.String(), "staged diff is too large") {
		t.Fatalf("stderr missing oversize error:\n%s", stderr.String())
	}
}

func TestRun_dryRunDoesNotCreateCommit(t *testing.T) {
	repoDir := initRepo(t)
	chdirForTest(t, repoDir)
	writeRepoFile(t, repoDir, "dryrun.go", "package main\n")

	srv, _ := newTestOllamaServer(t, "feat: preview commit")
	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"--all", "--dry-run", "--model", "test-model", "--url", srv.URL}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", exitCode, stderr.String())
	}

	countOutput := strings.TrimSpace(runGitCmd(t, repoDir, "rev-list", "--count", "--all"))
	if countOutput != "0" {
		t.Fatalf("commit count = %q, want 0", countOutput)
	}
}

func TestBuildPrompt_includesStructuredSummaryBeforeDiff(t *testing.T) {
	summary := review.Summary{
		TotalFiles:    1,
		ModifiedFiles: 1,
		DiffBytes:     123,
		DiffSizeLabel: "small",
		Detailed:      true,
		Files: []review.FileSummary{
			{Path: "cmd/root.go", Status: "M", Notes: []string{"existing file updated (+2/-1)", "moderate code edit"}},
		},
	}

	prompt := buildPrompt("Custom prompt", summary, "diff --git a/cmd/root.go b/cmd/root.go\n")

	summaryIndex := strings.Index(prompt.System, "STAGED REVIEW SUMMARY")
	safetyIndex := strings.Index(prompt.System, promptSafetyPreamble)
	diffIndex := strings.Index(prompt.User, "BEGIN UNTRUSTED GIT DIFF")
	if summaryIndex == -1 || safetyIndex == -1 || diffIndex == -1 {
		t.Fatalf("prompt missing required sections:\n%#v", prompt)
	}
	if !(summaryIndex < safetyIndex) {
		t.Fatalf("prompt system sections out of order:\n%#v", prompt)
	}
}

func TestWithRuntimeHint_appendsDetectionMessageForUnreachableBackend(t *testing.T) {
	prev := detectRuntimes
	detectRuntimes = func() []runtimecheck.Runtime {
		return nil
	}
	t.Cleanup(func() {
		detectRuntimes = prev
	})

	err := withRuntimeHint(fmt.Errorf("generation failed: ollama unreachable: dial tcp 127.0.0.1:11434: connect: connection refused"), providerOllama)
	got := err.Error()
	want := runtimecheck.UnavailableHint(nil, providerOllama)
	if !strings.Contains(got, "ollama unreachable") {
		t.Fatalf("error missing original message: %q", got)
	}
	if !strings.Contains(got, want) {
		t.Fatalf("error missing runtime hint: %q", got)
	}
}

func TestRun_providerOverrideUsesLMStudio(t *testing.T) {
	repoDir := initRepo(t)
	chdirForTest(t, repoDir)
	writeRepoFile(t, repoDir, "feature.go", "package main\n")

	srv, recorder := newTestLMStudioServer(t, "feat: use lm studio")
	var stdout, stderr bytes.Buffer

	exitCode := Run([]string{"--all", "--dry-run", "--provider", providerLMStudio, "--model", "custom-model", "--url", srv.URL}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", exitCode, stderr.String())
	}

	count, model, systemPrompt, userPrompt := lmStudioSnapshot(recorder)
	if count != 1 {
		t.Fatalf("generate requests = %d, want 1", count)
	}
	if model != "custom-model" {
		t.Fatalf("model = %q, want custom-model", model)
	}
	if !strings.Contains(systemPrompt, "STAGED REVIEW SUMMARY") || !strings.Contains(systemPrompt, promptSafetyPreamble) {
		t.Fatalf("system prompt missing shared context:\n%s", systemPrompt)
	}
	if !strings.Contains(userPrompt, "BEGIN UNTRUSTED GIT DIFF") {
		t.Fatalf("user prompt missing diff wrapper:\n%s", userPrompt)
	}
}

func TestResolveClientPlan_autoFallsBackToLMStudioWhenOllamaMissing(t *testing.T) {
	prevDetect := detectRuntimes
	prevOllama := newOllamaClient
	prevLMStudio := newLMStudioClient
	detectRuntimes = func() []runtimecheck.Runtime {
		return []runtimecheck.Runtime{{Name: runtimecheck.RuntimeLMStudio, Installed: true}}
	}
	newOllamaClient = func(baseURL string) generationClient {
		t.Fatalf("unexpected ollama client creation for LM Studio-only environment")
		return nil
	}
	newLMStudioClient = func(baseURL string) generationClient {
		return lmStudioGenerationClient{inner: lmstudio.NewClient(baseURL)}
	}
	t.Cleanup(func() {
		detectRuntimes = prevDetect
		newOllamaClient = prevOllama
		newLMStudioClient = prevLMStudio
	})

	plan, err := resolveClientPlan("", "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.primary.provider != providerLMStudio {
		t.Fatalf("primary provider = %q, want %q", plan.primary.provider, providerLMStudio)
	}
	if plan.fallback != nil {
		t.Fatalf("fallback = %#v, want nil", plan.fallback)
	}
}
