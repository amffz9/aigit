package review_test

import (
	"aigit/git"
	"aigit/review"
	"strings"
	"testing"
)

func TestAnalyze_buildsDetailedPerFileSummary(t *testing.T) {
	statuses := []git.FileStatus{
		{Status: "M", Path: "cmd/root.go"},
		{Status: "A", Path: "review/review_test.go"},
		{Status: "M", Path: "README.md"},
	}
	diff := strings.Join([]string{
		"diff --git a/cmd/root.go b/cmd/root.go",
		"--- a/cmd/root.go",
		"+++ b/cmd/root.go",
		"@@",
		"+new logic",
		"-old logic",
		"diff --git a/review/review_test.go b/review/review_test.go",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/review/review_test.go",
		"@@",
		"+package review_test",
		"+func TestSomething(t *testing.T) {}",
		"diff --git a/README.md b/README.md",
		"--- a/README.md",
		"+++ b/README.md",
		"@@",
		"+New docs",
	}, "\n")

	summary := review.Analyze(statuses, diff)

	if !summary.Detailed {
		t.Fatal("expected detailed summary for small changeset")
	}
	if len(summary.Files) != 3 {
		t.Fatalf("got %d file summaries, want 3", len(summary.Files))
	}
	if !contains(summary.Highlights, "Includes documentation changes") {
		t.Fatalf("highlights = %#v, want docs highlight", summary.Highlights)
	}

	prompt := review.FormatForPrompt(summary)
	if !strings.Contains(prompt, "INFERRED FILE NOTES") {
		t.Fatalf("prompt missing detailed file notes:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[A] review/review_test.go") {
		t.Fatalf("prompt missing staged file list entry:\n%s", prompt)
	}

	terminal := review.FormatForTerminal(summary)
	if !strings.Contains(terminal, "Per-file review:") {
		t.Fatalf("terminal summary missing per-file section:\n%s", terminal)
	}
	if !strings.Contains(terminal, "touches tests or assertions") {
		t.Fatalf("terminal summary missing inferred test note:\n%s", terminal)
	}
}

func TestAnalyze_groupsLargeChangesets(t *testing.T) {
	statuses := []git.FileStatus{
		{Status: "M", Path: "cmd/root.go"},
		{Status: "M", Path: "ui/ui.go"},
		{Status: "A", Path: "review/review.go"},
		{Status: "M", Path: "config/config.go"},
		{Status: "M", Path: "README.md"},
		{Status: "M", Path: ".github/workflows/test.yml"},
	}
	diff := strings.Join([]string{
		"diff --git a/cmd/root.go b/cmd/root.go",
		"--- a/cmd/root.go",
		"+++ b/cmd/root.go",
		"@@",
		"+line",
		"diff --git a/ui/ui.go b/ui/ui.go",
		"--- a/ui/ui.go",
		"+++ b/ui/ui.go",
		"@@",
		"+line",
		"diff --git a/review/review.go b/review/review.go",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/review/review.go",
		"@@",
		"+package review",
		"diff --git a/config/config.go b/config/config.go",
		"--- a/config/config.go",
		"+++ b/config/config.go",
		"@@",
		"+line",
		"diff --git a/README.md b/README.md",
		"--- a/README.md",
		"+++ b/README.md",
		"@@",
		"+docs",
		"diff --git a/.github/workflows/test.yml b/.github/workflows/test.yml",
		"--- a/.github/workflows/test.yml",
		"+++ b/.github/workflows/test.yml",
		"@@",
		"+name: test",
	}, "\n")

	summary := review.Analyze(statuses, diff)

	if summary.Detailed {
		t.Fatal("expected grouped summary for larger changeset")
	}
	if len(summary.Groups) == 0 {
		t.Fatal("expected grouped summaries")
	}
	if !contains(summary.Warnings, "More than 5 files staged; showing grouped notes instead of per-file bullets") {
		t.Fatalf("warnings = %#v, want grouped warning", summary.Warnings)
	}

	prompt := review.FormatForPrompt(summary)
	if !strings.Contains(prompt, "GROUPED CHANGE NOTES") {
		t.Fatalf("prompt missing grouped notes:\n%s", prompt)
	}
	if !strings.Contains(prompt, "CI and automation") {
		t.Fatalf("prompt missing CI grouping:\n%s", prompt)
	}
}

func TestAnalyze_detectsDocsOnlyChange(t *testing.T) {
	statuses := []git.FileStatus{{Status: "M", Path: "README.md"}}
	diff := strings.Join([]string{
		"diff --git a/README.md b/README.md",
		"--- a/README.md",
		"+++ b/README.md",
		"@@",
		"+More docs",
	}, "\n")

	summary := review.Analyze(statuses, diff)
	if !contains(summary.Highlights, "Docs-only change set") {
		t.Fatalf("highlights = %#v, want docs-only signal", summary.Highlights)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
