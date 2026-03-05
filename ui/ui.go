// Package ui provides terminal output helpers and the interactive user prompt
// for aigit's commit-message review flow.
//
// It handles:
//   - ANSI color output (respects NO_COLOR and non-TTY environments)
//   - The [C]ommit / [E]dit / [R]etry / [A]bort prompt loop
//   - Opening the user's $EDITOR with a temp file for message editing
package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Choice represents the user's selection from the interactive prompt.
type Choice int

const (
	ChoiceCommit Choice = iota // Commit with the generated message as-is.
	ChoiceEdit                 // Open $EDITOR so the user can refine the message.
	ChoiceRetry                // Re-generate a new message from the same diff.
	ChoiceAbort                // Exit without committing.
)

// ColorWriter wraps an io.Writer and optionally applies ANSI color codes.
// Colors are disabled when the output is not a TTY or when NO_COLOR is set.
type ColorWriter struct {
	w       io.Writer
	enabled bool
}

// NewColorWriter creates a ColorWriter targeting w.
// isTTY should be true when the output file descriptor is connected to a terminal.
// Colors are automatically disabled if the NO_COLOR environment variable is set.
func NewColorWriter(w io.Writer, isTTY bool) *ColorWriter {
	return &ColorWriter{
		w:       w,
		enabled: isTTY && os.Getenv("NO_COLOR") == "",
	}
}

// colorize wraps s in the given ANSI SGR code, or returns s unchanged if
// color output is disabled.
func (c *ColorWriter) colorize(code, s string) string {
	if !c.enabled {
		return s
	}
	return fmt.Sprintf("\033[%sm%s\033[0m", code, s)
}

// Green returns s formatted in green.
func (c *ColorWriter) Green(s string) string { return c.colorize("32", s) }

// Yellow returns s formatted in yellow.
func (c *ColorWriter) Yellow(s string) string { return c.colorize("33", s) }

// Cyan returns s formatted in cyan.
func (c *ColorWriter) Cyan(s string) string { return c.colorize("36", s) }

// Bold returns s formatted in bold.
func (c *ColorWriter) Bold(s string) string { return c.colorize("1", s) }

// Printf writes a formatted string to the underlying writer.
func (c *ColorWriter) Printf(format string, args ...any) {
	fmt.Fprintf(c.w, format, args...)
}

// Println writes s followed by a newline to the underlying writer.
func (c *ColorWriter) Println(s string) {
	fmt.Fprintln(c.w, s)
}

// PrintStagedFiles prints the list of staged files with a bold header and
// green "+" prefix for each entry.
func PrintStagedFiles(cw *ColorWriter, files []string) {
	cw.Println(cw.Bold("Staged files:"))
	for _, f := range files {
		cw.Printf("  %s %s\n", cw.Green("+"), f)
	}
	cw.Println("")
}

// PromptUser displays the [C]ommit / [E]dit / [R]etry / [A]bort menu and
// reads from in until the user enters a recognised key. Invalid input causes
// the prompt to repeat. Returns an error only if reading from in fails.
func PromptUser(in io.Reader, out io.Writer) (Choice, error) {
	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, "\n[C]ommit  [E]dit  [R]etry  [A]bort > ")
		if !scanner.Scan() {
			// EOF or read error — treat as abort so the caller can exit cleanly.
			return ChoiceAbort, scanner.Err()
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "c":
			return ChoiceCommit, nil
		case "e":
			return ChoiceEdit, nil
		case "r":
			return ChoiceRetry, nil
		case "a":
			return ChoiceAbort, nil
		default:
			fmt.Fprintln(out, "Invalid choice. Enter c, e, r, or a.")
		}
	}
}

// WriteTempMessage writes msg to a new temporary file and returns its path.
// The caller is responsible for removing the file with os.Remove when done.
func WriteTempMessage(msg string) (string, error) {
	f, err := os.CreateTemp("", "aigit-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.WriteString(msg)
	return f.Name(), err
}

// OpenEditor opens the file at path in the user's preferred editor.
// The editor is taken from $EDITOR; if unset, vi is used as a fallback.
// The editor process inherits the current stdin/stdout/stderr so the user
// gets a full interactive terminal session.
func OpenEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	// $EDITOR may contain flags (e.g. "code --wait"), so split on whitespace.
	parts := strings.Fields(editor)
	args := append(parts[1:], path)

	cmd := newEditorCmd(parts[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// newEditorCmd constructs the exec.Cmd for the editor. It is a variable so
// tests can replace it with a no-op to avoid launching a real editor process.
var newEditorCmd = func(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
