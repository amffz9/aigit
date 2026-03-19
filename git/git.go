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

// FileStatus represents a staged file and its git status letter.
type FileStatus struct {
	Status string // A, M, D, R, etc.
	Path   string // file path (for renames, "old → new")
}

// StagedFileStatuses returns the status and path of each staged file.
func StagedFileStatuses(repoDir string) ([]FileStatus, error) {
	out, err := runGit(repoDir, "diff", "--cached", "--name-status")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return []FileStatus{}, nil
	}
	lines := strings.Split(out, "\n")
	result := make([]FileStatus, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		path := parts[1]
		// Renames have status like R100 and three tab-separated fields
		if strings.HasPrefix(status, "R") && len(parts) >= 3 {
			path = parts[1] + " → " + parts[2]
			status = "R"
		}
		result = append(result, FileStatus{Status: status, Path: path})
	}
	return result, nil
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

// StageAll stages tracked and untracked changes (git add -A).
func StageAll(repoDir string) error {
	_, err := runGit(repoDir, "add", "-A")
	return err
}

// Commit creates a commit with the given message.
func Commit(repoDir string, message string) error {
	_, err := runGit(repoDir, "commit", "-m", message)
	return err
}
