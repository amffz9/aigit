package review

import (
	"aigit/git"
	"fmt"
	"path/filepath"
	"strings"
)

const detailedFileThreshold = 5

// Summary captures a structured view of the staged change set for both
// terminal review and prompt construction.
type Summary struct {
	TotalFiles    int
	AddedFiles    int
	ModifiedFiles int
	DeletedFiles  int
	RenamedFiles  int
	DiffBytes     int
	DiffSizeLabel string
	Detailed      bool
	Files         []FileSummary
	Groups        []GroupSummary
	Highlights    []string
	Warnings      []string
}

// FileSummary describes a single staged file in the review output.
type FileSummary struct {
	Path         string
	Status       string
	Category     string
	AddedLines   int
	RemovedLines int
	Notes        []string
}

// GroupSummary collapses multiple file summaries into a single logical area.
type GroupSummary struct {
	Label       string
	Count       int
	SamplePaths []string
}

type diffSection struct {
	OldPath      string
	NewPath      string
	AddedLines   int
	RemovedLines int
	NewFile      bool
	DeletedFile  bool
	Renamed      bool
}

// Analyze builds a Summary from staged git statuses and the staged diff.
func Analyze(statuses []git.FileStatus, diff string) Summary {
	sections := parseDiffSections(diff)
	summary := Summary{
		TotalFiles:    len(statuses),
		DiffBytes:     len(diff),
		DiffSizeLabel: diffSizeLabel(len(diff)),
		Detailed:      len(statuses) <= detailedFileThreshold,
		Files:         make([]FileSummary, 0, len(statuses)),
	}

	categorySeen := make(map[string]bool)
	totalAdded := 0
	totalRemoved := 0

	for _, status := range statuses {
		section := findSection(status, sections)
		fileSummary := buildFileSummary(status, section)
		summary.Files = append(summary.Files, fileSummary)
		totalAdded += fileSummary.AddedLines
		totalRemoved += fileSummary.RemovedLines
		categorySeen[fileSummary.Category] = true

		switch normalizeStatus(status.Status) {
		case "A":
			summary.AddedFiles++
		case "M":
			summary.ModifiedFiles++
		case "D":
			summary.DeletedFiles++
		case "R":
			summary.RenamedFiles++
		}
	}

	summary.Highlights = buildHighlights(summary, categorySeen)
	summary.Warnings = buildWarnings(summary, categorySeen, totalAdded, totalRemoved)
	if summary.Detailed {
		return summary
	}
	summary.Groups = buildGroups(summary.Files)
	return summary
}

// FormatForTerminal renders a scan-friendly review block for the CLI.
func FormatForTerminal(summary Summary) string {
	var b strings.Builder
	b.WriteString("Review summary:\n")
	b.WriteString(fmt.Sprintf("  Files changed: %d (%s)\n", summary.TotalFiles, formatStatusBreakdown(summary)))
	b.WriteString(fmt.Sprintf("  Diff size: %s (%s)\n", humanBytes(summary.DiffBytes), summary.DiffSizeLabel))

	if len(summary.Highlights) > 0 {
		b.WriteString("\nKey signals:\n")
		for _, item := range summary.Highlights {
			b.WriteString("  - " + item + "\n")
		}
	}

	if len(summary.Warnings) > 0 {
		b.WriteString("\nWarnings:\n")
		for _, item := range summary.Warnings {
			b.WriteString("  - " + item + "\n")
		}
	}

	if summary.Detailed {
		b.WriteString("\nPer-file review:\n")
		for _, file := range summary.Files {
			b.WriteString(fmt.Sprintf("  - [%s] %s\n", file.Status, file.Path))
			for _, note := range file.Notes {
				b.WriteString("      " + note + "\n")
			}
		}
	} else {
		b.WriteString("\nGrouped review:\n")
		for _, group := range summary.Groups {
			line := fmt.Sprintf("  - %s (%d): %s\n", group.Label, group.Count, strings.Join(group.SamplePaths, ", "))
			b.WriteString(line)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// FormatForPrompt renders the structured review context that precedes the raw
// diff in the model prompt.
func FormatForPrompt(summary Summary) string {
	var b strings.Builder
	b.WriteString("STAGED REVIEW SUMMARY\n")
	b.WriteString(fmt.Sprintf("- Files changed: %d\n", summary.TotalFiles))
	b.WriteString(fmt.Sprintf("- Status breakdown: %s\n", formatStatusBreakdown(summary)))
	b.WriteString(fmt.Sprintf("- Diff size: %s (%s)\n", humanBytes(summary.DiffBytes), summary.DiffSizeLabel))

	if len(summary.Highlights) > 0 {
		b.WriteString("\nKEY SIGNALS\n")
		for _, item := range summary.Highlights {
			b.WriteString("- " + item + "\n")
		}
	}

	if len(summary.Warnings) > 0 {
		b.WriteString("\nWARNINGS\n")
		for _, item := range summary.Warnings {
			b.WriteString("- " + item + "\n")
		}
	}

	b.WriteString("\nSTAGED FILES\n")
	for _, file := range summary.Files {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", file.Status, file.Path))
	}

	if summary.Detailed {
		b.WriteString("\nINFERRED FILE NOTES\n")
		for _, file := range summary.Files {
			b.WriteString(fmt.Sprintf("- [%s] %s: %s\n", file.Status, file.Path, strings.Join(file.Notes, "; ")))
		}
	} else {
		b.WriteString("\nGROUPED CHANGE NOTES\n")
		for _, group := range summary.Groups {
			b.WriteString(fmt.Sprintf("- %s (%d): %s\n", group.Label, group.Count, strings.Join(group.SamplePaths, ", ")))
		}
	}

	b.WriteString("\nUse this staged review as supporting context.")
	b.WriteString("\nBase the final commit message on the overall intent of the whole change, not a file-by-file changelog.")
	return b.String()
}

func buildFileSummary(status git.FileStatus, section diffSection) FileSummary {
	category := categorizePath(primaryPath(status), section)
	summary := FileSummary{
		Path:         status.Path,
		Status:       normalizeStatus(status.Status),
		Category:     category,
		AddedLines:   section.AddedLines,
		RemovedLines: section.RemovedLines,
	}

	summary.Notes = append(summary.Notes, describeStatus(status, section))
	if note := describeCategory(category); note != "" {
		summary.Notes = append(summary.Notes, note)
	} else if note := describeMagnitude(summary.AddedLines, summary.RemovedLines); note != "" {
		summary.Notes = append(summary.Notes, note)
	}
	return summary
}

func buildGroups(files []FileSummary) []GroupSummary {
	order := []struct {
		key   string
		label string
	}{
		{"code", "Application code"},
		{"tests", "Tests"},
		{"docs", "Documentation"},
		{"config", "Config and build"},
		{"dependency", "Dependencies and lockfiles"},
		{"migration", "Migrations"},
		{"ci", "CI and automation"},
		{"generated", "Generated or vendored"},
		{"other", "Other files"},
	}

	type aggregate struct {
		label string
		paths []string
	}

	groups := make(map[string]*aggregate)
	for _, file := range files {
		group := groups[file.Category]
		if group == nil {
			group = &aggregate{label: labelForCategory(file.Category)}
			groups[file.Category] = group
		}
		group.paths = append(group.paths, file.Path)
	}

	result := make([]GroupSummary, 0, len(groups))
	for _, item := range order {
		group := groups[item.key]
		if group == nil {
			continue
		}
		result = append(result, GroupSummary{
			Label:       item.label,
			Count:       len(group.paths),
			SamplePaths: samplePaths(group.paths),
		})
	}
	return result
}

func buildHighlights(summary Summary, categorySeen map[string]bool) []string {
	var highlights []string
	if docsOnly(summary.Files) {
		highlights = append(highlights, "Docs-only change set")
	}
	if categorySeen["tests"] {
		highlights = append(highlights, "Includes test changes")
	}
	if categorySeen["docs"] && !docsOnly(summary.Files) {
		highlights = append(highlights, "Includes documentation changes")
	}
	if categorySeen["config"] {
		highlights = append(highlights, "Includes config or build changes")
	}
	if categorySeen["dependency"] {
		highlights = append(highlights, "Includes dependency metadata or lockfiles")
	}
	if categorySeen["migration"] {
		highlights = append(highlights, "Includes database or schema migrations")
	}
	if categorySeen["ci"] {
		highlights = append(highlights, "Includes CI or automation changes")
	}
	return highlights
}

func buildWarnings(summary Summary, categorySeen map[string]bool, totalAdded, totalRemoved int) []string {
	var warnings []string
	if summary.DiffBytes >= 50_000 {
		warnings = append(warnings, "Large staged diff; grouped review may hide some file-level detail")
	}
	if categorySeen["generated"] {
		warnings = append(warnings, "Includes generated or vendored artifacts")
	}
	if totalRemoved >= 20 && totalRemoved > totalAdded*2 {
		warnings = append(warnings, "Deletion-heavy change set")
	}
	if summary.RenamedFiles >= 2 {
		warnings = append(warnings, "Multiple renames in the staged set")
	}
	if !summary.Detailed {
		warnings = append(warnings, fmt.Sprintf("More than %d files staged; showing grouped notes instead of per-file bullets", detailedFileThreshold))
	}
	return warnings
}

func docsOnly(files []FileSummary) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if file.Category != "docs" {
			return false
		}
	}
	return true
}

func describeStatus(status git.FileStatus, section diffSection) string {
	changeSize := lineChangeSummary(section.AddedLines, section.RemovedLines)
	switch normalizeStatus(status.Status) {
	case "A":
		return "new file staged" + changeSize
	case "M":
		return "existing file updated" + changeSize
	case "D":
		return "file removed from the repository" + changeSize
	case "R":
		oldPath, newPath := renameParts(status.Path)
		if section.AddedLines == 0 && section.RemovedLines == 0 {
			return fmt.Sprintf("renamed from %s to %s without content changes", oldPath, newPath)
		}
		return fmt.Sprintf("renamed from %s to %s with follow-up edits%s", oldPath, newPath, changeSize)
	default:
		return "staged file updated" + changeSize
	}
}

func describeCategory(category string) string {
	switch category {
	case "tests":
		return "touches tests or assertions"
	case "docs":
		return "touches documentation or examples"
	case "config":
		return "changes configuration or build behavior"
	case "dependency":
		return "updates dependency metadata or lockfiles"
	case "migration":
		return "changes migration or schema files"
	case "ci":
		return "changes CI or automation files"
	case "generated":
		return "touches generated or vendored artifacts"
	default:
		return ""
	}
}

func describeMagnitude(added, removed int) string {
	total := added + removed
	switch {
	case total >= 120:
		return "large code edit"
	case total >= 40:
		return "moderate code edit"
	case added > 0 && removed == 0:
		return "mostly additive change"
	case removed > 0 && added == 0:
		return "mostly subtractive change"
	default:
		return ""
	}
}

func parseDiffSections(diff string) []diffSection {
	lines := strings.Split(diff, "\n")
	sections := make([]diffSection, 0)
	var current *diffSection

	flush := func() {
		if current != nil {
			sections = append(sections, *current)
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			oldPath, newPath := parseDiffHeader(line)
			current = &diffSection{OldPath: oldPath, NewPath: newPath, Renamed: oldPath != "" && newPath != "" && oldPath != newPath}
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "new file mode "):
			current.NewFile = true
		case strings.HasPrefix(line, "deleted file mode "):
			current.DeletedFile = true
		case strings.HasPrefix(line, "rename from "):
			current.OldPath = strings.TrimSpace(strings.TrimPrefix(line, "rename from "))
			current.Renamed = true
		case strings.HasPrefix(line, "rename to "):
			current.NewPath = strings.TrimSpace(strings.TrimPrefix(line, "rename to "))
			current.Renamed = true
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ "):
			current.AddedLines++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--- "):
			current.RemovedLines++
		}
	}
	flush()
	return sections
}

func parseDiffHeader(line string) (string, string) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return "", ""
	}
	return trimDiffPath(fields[2]), trimDiffPath(fields[3])
}

func trimDiffPath(value string) string {
	value = strings.Trim(value, "\"")
	value = strings.TrimPrefix(value, "a/")
	value = strings.TrimPrefix(value, "b/")
	return value
}

func findSection(status git.FileStatus, sections []diffSection) diffSection {
	path := primaryPath(status)
	if status.Status == "R" || strings.HasPrefix(status.Status, "R") {
		oldPath, newPath := renameParts(status.Path)
		for _, section := range sections {
			if section.OldPath == oldPath && section.NewPath == newPath {
				return section
			}
		}
	}
	for _, section := range sections {
		if section.NewPath == path || section.OldPath == path {
			return section
		}
	}
	return diffSection{NewPath: path}
}

func primaryPath(status git.FileStatus) string {
	if status.Status == "R" || strings.HasPrefix(status.Status, "R") {
		_, newPath := renameParts(status.Path)
		return newPath
	}
	return status.Path
}

func renameParts(path string) (string, string) {
	parts := strings.SplitN(path, " → ", 2)
	if len(parts) != 2 {
		return path, path
	}
	return parts[0], parts[1]
}

func categorizePath(path string, section diffSection) string {
	lowerPath := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))

	switch {
	case isDependencyFile(base):
		return "dependency"
	case isMigrationFile(lowerPath, base):
		return "migration"
	case isTestFile(lowerPath, base):
		return "tests"
	case isDocsFile(lowerPath, base):
		return "docs"
	case isCIFile(lowerPath, base):
		return "ci"
	case isGeneratedFile(lowerPath, base, section):
		return "generated"
	case isConfigFile(lowerPath, base):
		return "config"
	case base == "":
		return "other"
	default:
		return "code"
	}
}

func isDependencyFile(base string) bool {
	switch base {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "cargo.lock", "pipfile.lock", "poetry.lock", "composer.lock", "gemfile.lock", "bun.lockb":
		return true
	default:
		return false
	}
}

func isMigrationFile(lowerPath, base string) bool {
	return strings.Contains(lowerPath, "/migrations/") || strings.Contains(lowerPath, "/migration/") || strings.HasSuffix(base, ".sql") && strings.Contains(lowerPath, "migr")
}

func isTestFile(lowerPath, base string) bool {
	return strings.Contains(lowerPath, "/test/") ||
		strings.Contains(lowerPath, "/tests/") ||
		strings.Contains(lowerPath, "__tests__/") ||
		strings.HasSuffix(base, "_test.go") ||
		strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.")
}

func isDocsFile(lowerPath, base string) bool {
	ext := strings.ToLower(filepath.Ext(base))
	if base == "readme" || strings.HasPrefix(base, "readme.") || strings.HasPrefix(base, "changelog") || strings.HasPrefix(base, "license") {
		return true
	}
	return strings.HasPrefix(lowerPath, "docs/") || strings.Contains(lowerPath, "/docs/") || ext == ".md" || ext == ".rst" || ext == ".txt"
}

func isCIFile(lowerPath, base string) bool {
	return strings.HasPrefix(lowerPath, ".github/workflows/") ||
		strings.Contains(lowerPath, "/.github/workflows/") ||
		strings.Contains(lowerPath, "/.circleci/") ||
		strings.Contains(lowerPath, "/ci/") ||
		base == "jenkinsfile"
}

func isGeneratedFile(lowerPath, base string, section diffSection) bool {
	if strings.Contains(lowerPath, "/vendor/") || strings.Contains(lowerPath, "/dist/") || strings.Contains(lowerPath, "/generated/") || strings.HasSuffix(base, ".pb.go") {
		return true
	}
	return section.NewFile && strings.Contains(lowerPath, "vendor")
}

func isConfigFile(lowerPath, base string) bool {
	if strings.HasPrefix(base, ".env") || base == ".gitignore" || base == "dockerfile" || base == "makefile" {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".json", ".yaml", ".yml", ".toml", ".ini":
		return !strings.Contains(lowerPath, "/testdata/")
	default:
		return false
	}
}

func normalizeStatus(status string) string {
	if status == "" {
		return "?"
	}
	return strings.ToUpper(status[:1])
}

func lineChangeSummary(added, removed int) string {
	if added == 0 && removed == 0 {
		return ""
	}
	return fmt.Sprintf(" (+%d/-%d)", added, removed)
}

func diffSizeLabel(size int) string {
	switch {
	case size < 5_000:
		return "small"
	case size < 20_000:
		return "medium"
	case size < 50_000:
		return "large"
	default:
		return "very large"
	}
}

func formatStatusBreakdown(summary Summary) string {
	var parts []string
	if summary.AddedFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d added", summary.AddedFiles))
	}
	if summary.ModifiedFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", summary.ModifiedFiles))
	}
	if summary.DeletedFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", summary.DeletedFiles))
	}
	if summary.RenamedFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d renamed", summary.RenamedFiles))
	}
	if len(parts) == 0 {
		return "no changes detected"
	}
	return strings.Join(parts, ", ")
}

func humanBytes(size int) string {
	switch {
	case size >= 1_000_000:
		return fmt.Sprintf("%.1f MB", float64(size)/1_000_000)
	case size >= 1_000:
		return fmt.Sprintf("%.1f KB", float64(size)/1_000)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func samplePaths(paths []string) []string {
	if len(paths) <= 3 {
		return paths
	}
	samples := append([]string{}, paths[:3]...)
	samples = append(samples, fmt.Sprintf("+%d more", len(paths)-3))
	return samples
}

func labelForCategory(category string) string {
	switch category {
	case "tests":
		return "Tests"
	case "docs":
		return "Documentation"
	case "config":
		return "Config and build"
	case "dependency":
		return "Dependencies and lockfiles"
	case "migration":
		return "Migrations"
	case "ci":
		return "CI and automation"
	case "generated":
		return "Generated or vendored"
	case "other":
		return "Other files"
	default:
		return "Application code"
	}
}
