package git_test

import (
	"aigit/git"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "config", "user.email", "test@test.com")
	mustRun(t, dir, "git", "config", "user.name", "Test")
	return dir
}

func mustRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %v failed: %s", args, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRepoRoot(t *testing.T) {
	dir := initRepo(t)
	subdir := filepath.Join(dir, "src", "auth")
	os.MkdirAll(subdir, 0755)
	root, err := git.RepoRoot(subdir)
	if err != nil {
		t.Fatal(err)
	}
	// On macOS, TempDir may be under a symlink — resolve both
	wantEval, _ := filepath.EvalSymlinks(dir)
	gotEval, _ := filepath.EvalSymlinks(root)
	if gotEval != wantEval {
		t.Errorf("got %q want %q", root, dir)
	}
}

func TestStagedFiles_empty(t *testing.T) {
	dir := initRepo(t)
	files, err := git.StagedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty, got %v", files)
	}
}

func TestStagedFiles_afterAdd(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "foo.go", "package main")
	mustRun(t, dir, "git", "add", "foo.go")
	files, err := git.StagedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "foo.go" {
		t.Errorf("unexpected files: %v", files)
	}
}

func TestStagedDiff(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "foo.go", "package main\n")
	mustRun(t, dir, "git", "add", "foo.go")
	diff, err := git.StagedDiff(dir)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("expected non-empty diff")
	}
	if !contains(diff, "package main") {
		t.Errorf("diff missing content: %s", diff)
	}
}

func TestStageDir(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "src/a.go", "package a")
	writeFile(t, dir, "src/b.go", "package b")
	writeFile(t, dir, "other.go", "package main")
	if err := git.StageDir(dir, filepath.Join(dir, "src")); err != nil {
		t.Fatal(err)
	}
	files, _ := git.StagedFiles(dir)
	if len(files) != 2 {
		t.Errorf("expected 2 staged files, got %v", files)
	}
}

func TestStageFiles(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "a.go", "package a")
	writeFile(t, dir, "b.go", "package b")
	if err := git.StageFiles(dir, []string{filepath.Join(dir, "a.go")}); err != nil {
		t.Fatal(err)
	}
	files, _ := git.StagedFiles(dir)
	if len(files) != 1 || files[0] != "a.go" {
		t.Errorf("unexpected staged files: %v", files)
	}
}

func TestCommit(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "foo.go", "package main\n")
	mustRun(t, dir, "git", "add", "foo.go")
	if err := git.Commit(dir, "feat: add foo"); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("git", "-C", dir, "log", "--oneline").Output()
	if !contains(string(out), "feat: add foo") {
		t.Errorf("commit message not found in log: %s", out)
	}
}

func TestStagedFileStatuses(t *testing.T) {
	dir := initRepo(t)

	// Create initial commit so we can test modify/delete/rename
	writeFile(t, dir, "existing.go", "package main\n")
	writeFile(t, dir, "toremove.go", "package rm\n")
	writeFile(t, dir, "torename.go", "package mv\n")
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-m", "init")

	// Added file
	writeFile(t, dir, "new.go", "package new\n")
	mustRun(t, dir, "git", "add", "new.go")

	// Modified file
	writeFile(t, dir, "existing.go", "package main\n// changed\n")
	mustRun(t, dir, "git", "add", "existing.go")

	// Deleted file
	os.Remove(filepath.Join(dir, "toremove.go"))
	mustRun(t, dir, "git", "add", "toremove.go")

	// Renamed file
	mustRun(t, dir, "git", "mv", "torename.go", "renamed.go")

	statuses, err := git.StagedFileStatuses(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]string) // path -> status
	for _, s := range statuses {
		got[s.Path] = s.Status
	}

	if got["existing.go"] != "M" {
		t.Errorf("existing.go: want M, got %q", got["existing.go"])
	}
	if got["new.go"] != "A" {
		t.Errorf("new.go: want A, got %q", got["new.go"])
	}
	if got["toremove.go"] != "D" {
		t.Errorf("toremove.go: want D, got %q", got["toremove.go"])
	}

	// Rename path should be "torename.go → renamed.go"
	var foundRename bool
	for _, s := range statuses {
		if s.Status == "R" && strings.Contains(s.Path, "torename.go") && strings.Contains(s.Path, "renamed.go") {
			foundRename = true
			break
		}
	}
	if !foundRename {
		t.Errorf("expected rename entry for torename.go → renamed.go, got %v", statuses)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
