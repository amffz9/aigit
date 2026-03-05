package ui_test

import (
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
