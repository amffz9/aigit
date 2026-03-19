// Package ui provides terminal output helpers and the interactive user prompt
// for aigit's commit-message review flow.
//
// It handles:
//   - ANSI color output (respects NO_COLOR and non-TTY environments)
//   - The [C]ommit / [E]dit / [R]etry / [A]bort prompt loop
//   - Opening the user's $EDITOR with a temp file for message editing
package ui

import (
	"aigit/git"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
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

// Red returns s formatted in red.
func (c *ColorWriter) Red(s string) string { return c.colorize("31", s) }

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

// PrintStagedFiles prints the list of staged files with status indicators.
// Added files show a green +, modified a yellow ~, deleted a red -, renamed
// a cyan →, and unknown statuses a white ?.
func PrintStagedFiles(cw *ColorWriter, files []git.FileStatus) {
	cw.Println(cw.Bold("Staged files:"))
	for _, f := range files {
		var indicator string
		switch f.Status {
		case "A":
			indicator = cw.Green("+")
		case "M":
			indicator = cw.Yellow("~")
		case "D":
			indicator = cw.Red("-")
		case "R":
			indicator = cw.Cyan("→")
		default:
			indicator = "?"
		}
		cw.Printf("  %s %s\n", indicator, f.Path)
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
	f, err := os.CreateTemp("", "aigit-*.gitcommit")
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.WriteString(msg)
	return f.Name(), err
}

// OpenEditor opens the file at path in the user's preferred editor.
// The editor is taken from $VISUAL, then $EDITOR; if neither is set, vi is used.
// The editor process inherits the current stdin/stdout/stderr so the user
// gets a full interactive terminal session.
func OpenEditor(path string) error {
	parts, err := editorCommandParts()
	if err != nil {
		return err
	}
	args := append(parts[1:], path)

	cmd := newEditorCmd(parts[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func editorCommandParts() ([]string, error) {
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		return []string{"vi"}, nil
	}

	parts, err := splitCommandLine(editor)
	if err != nil {
		return nil, fmt.Errorf("invalid editor command: %w", err)
	}
	if len(parts) == 0 {
		return []string{"vi"}, nil
	}
	if strings.TrimSpace(parts[0]) == "" || filepath.Base(parts[0]) == "." {
		return nil, errors.New("empty editor executable")
	}
	return parts, nil
}

func splitCommandLine(s string) ([]string, error) {
	var (
		args        []string
		current     strings.Builder
		inSingle    bool
		inDouble    bool
		escaping    bool
		tokenActive bool
	)

	flush := func() {
		if tokenActive {
			args = append(args, current.String())
			current.Reset()
			tokenActive = false
		}
	}

	for _, r := range s {
		switch {
		case escaping:
			current.WriteRune(r)
			tokenActive = true
			escaping = false

		case inSingle:
			if r == '\'' {
				inSingle = false
				continue
			}
			current.WriteRune(r)
			tokenActive = true

		case inDouble:
			switch r {
			case '"':
				inDouble = false
			case '\\':
				escaping = true
			default:
				current.WriteRune(r)
				tokenActive = true
			}

		default:
			switch {
			case unicode.IsSpace(r):
				flush()
			case r == '\'':
				inSingle = true
				tokenActive = true
			case r == '"':
				inDouble = true
				tokenActive = true
			case r == '\\':
				escaping = true
				tokenActive = true
			default:
				current.WriteRune(r)
				tokenActive = true
			}
		}
	}

	if escaping {
		return nil, errors.New("unterminated escape")
	}
	if inSingle || inDouble {
		return nil, errors.New("unterminated quote")
	}

	flush()
	return args, nil
}

// newEditorCmd constructs the exec.Cmd for the editor. It is a variable so
// tests can replace it with a no-op to avoid launching a real editor process.
var newEditorCmd = func(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
