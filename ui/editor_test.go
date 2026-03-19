package ui

import (
	"reflect"
	"testing"
)

func TestEditorCommandParts_prefersVisual(t *testing.T) {
	t.Setenv("VISUAL", "code --wait")
	t.Setenv("EDITOR", "vim")

	parts, err := editorCommandParts()
	if err != nil {
		t.Fatalf("editorCommandParts returned error: %v", err)
	}

	want := []string{"code", "--wait"}
	if !reflect.DeepEqual(parts, want) {
		t.Fatalf("parts = %#v, want %#v", parts, want)
	}
}

func TestSplitCommandLine_handlesQuotedArgs(t *testing.T) {
	line := "\"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code\" --wait"

	parts, err := splitCommandLine(line)
	if err != nil {
		t.Fatalf("splitCommandLine returned error: %v", err)
	}

	want := []string{"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code", "--wait"}
	if !reflect.DeepEqual(parts, want) {
		t.Fatalf("parts = %#v, want %#v", parts, want)
	}
}

func TestSplitCommandLine_rejectsUnterminatedQuote(t *testing.T) {
	_, err := splitCommandLine("\"code --wait")
	if err == nil {
		t.Fatal("expected unterminated quote error")
	}
}

func TestEditorCommandParts_rejectsEmptyExecutable(t *testing.T) {
	t.Setenv("VISUAL", "\"\"")

	_, err := editorCommandParts()
	if err == nil {
		t.Fatal("expected empty executable error")
	}
}
