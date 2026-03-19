package cmd

import (
	"bytes"
	"encoding/json"
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

func newTestOllamaServer(t *testing.T, response string) (*httptest.Server, *generateRecorder) {
	t.Helper()

	recorder := &generateRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
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
