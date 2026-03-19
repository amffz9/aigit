package ui_test

import (
	"aigit/git"
	"aigit/ui"
	"bytes"
	"strings"
	"testing"
)

func TestPromptUser_commit(t *testing.T) {
	choice, err := ui.PromptUser(strings.NewReader("c\n"), &bytes.Buffer{})
	if err != nil || choice != ui.ChoiceCommit {
		t.Errorf("got %v %v, want ChoiceCommit", choice, err)
	}
}

func TestPromptUser_edit_uppercase(t *testing.T) {
	choice, err := ui.PromptUser(strings.NewReader("E\n"), &bytes.Buffer{})
	if err != nil || choice != ui.ChoiceEdit {
		t.Errorf("got %v %v, want ChoiceEdit", choice, err)
	}
}

func TestPromptUser_retry(t *testing.T) {
	choice, err := ui.PromptUser(strings.NewReader("r\n"), &bytes.Buffer{})
	if err != nil || choice != ui.ChoiceRetry {
		t.Errorf("got %v %v, want ChoiceRetry", choice, err)
	}
}

func TestPromptUser_abort(t *testing.T) {
	choice, err := ui.PromptUser(strings.NewReader("a\n"), &bytes.Buffer{})
	if err != nil || choice != ui.ChoiceAbort {
		t.Errorf("got %v %v, want ChoiceAbort", choice, err)
	}
}

func TestPromptUser_invalid_then_valid(t *testing.T) {
	choice, err := ui.PromptUser(strings.NewReader("x\nz\nc\n"), &bytes.Buffer{})
	if err != nil || choice != ui.ChoiceCommit {
		t.Errorf("got %v %v, want ChoiceCommit after invalid inputs", choice, err)
	}
}

func TestPrintStagedFiles(t *testing.T) {
	var buf bytes.Buffer
	cw := ui.NewColorWriter(&buf, false) // no color for easy assertions

	files := []git.FileStatus{
		{Status: "A", Path: "new.go"},
		{Status: "M", Path: "changed.go"},
		{Status: "D", Path: "removed.go"},
		{Status: "R", Path: "old.go → new.go"},
		{Status: "C", Path: "copied.go"},
	}
	ui.PrintStagedFiles(cw, files)
	out := buf.String()

	expects := []struct {
		indicator string
		path      string
	}{
		{"+", "new.go"},
		{"~", "changed.go"},
		{"-", "removed.go"},
		{"→", "old.go → new.go"},
		{"?", "copied.go"},
	}
	for _, e := range expects {
		want := e.indicator + " " + e.path
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestColorWriter_noColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	cw := ui.NewColorWriter(&buf, false) // false = not a TTY
	result := cw.Green("hello")
	if strings.Contains(result, "\033[") {
		t.Errorf("expected no ANSI codes when NO_COLOR set, got %q", result)
	}
}

func TestColorWriter_withColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var buf bytes.Buffer
	cw := ui.NewColorWriter(&buf, true) // true = is a TTY
	result := cw.Green("hello")
	if !strings.Contains(result, "\033[") {
		t.Errorf("expected ANSI codes for TTY, got %q", result)
	}
}
