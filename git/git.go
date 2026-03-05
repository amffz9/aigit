package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// runGit runs a git command in repoDir and returns combined output.
func runGit(repoDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// RepoRoot returns the absolute path of the repo root from any working dir.
func RepoRoot(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(out), nil
}

// StagedFiles returns the list of files currently staged (relative to repo root).
func StagedFiles(repoDir string) ([]string, error) {
	out, err := runGit(repoDir, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return []string{}, nil
	}
	return strings.Split(out, "\n"), nil
}

// StagedDiff returns the full text of git diff --cached.
func StagedDiff(repoDir string) (string, error) {
	return runGit(repoDir, "diff", "--cached")
}

// StageDir stages all changes under an absolute directory path.
func StageDir(repoDir string, absDir string) error {
	_, err := runGit(repoDir, "add", absDir)
	return err
}

// StageFiles stages specific files given as absolute paths.
func StageFiles(repoDir string, absPaths []string) error {
	args := append([]string{"add", "--"}, absPaths...)
	_, err := runGit(repoDir, args...)
	return err
}

// StageAll stages all tracked modified files (git add -u).
func StageAll(repoDir string) error {
	_, err := runGit(repoDir, "add", "-u")
	return err
}

// Commit creates a commit with the given message.
func Commit(repoDir string, message string) error {
	_, err := runGit(repoDir, "commit", "-m", message)
	return err
}
