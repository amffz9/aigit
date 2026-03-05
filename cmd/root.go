// Package cmd contains aigit's top-level CLI logic.
//
// Run is the single entry point. It parses flags, resolves configuration,
// stages files as requested, calls the Ollama API to generate a commit message,
// streams the result token-by-token, and then enters an interactive loop that
// lets the user commit, edit, retry, or abort.
package cmd

import (
	"aigit/config"
	"aigit/git"
	"aigit/ollama"
	"aigit/ui"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
)

// Run is the CLI entry point. It returns an exit code suitable for os.Exit.
// Accepting io.Reader/Writer parameters makes the function testable without
// real terminal I/O.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags, err := parseFlags(args, stderr)
	if err != nil {
		// flag package already printed the error message.
		return 2
	}

	cfg, err := config.Load(config.Overrides{Model: flags.model, URL: flags.url}, "")
	if err != nil {
		fmt.Fprintf(stderr, "aigit: config error: %v\n", err)
		return 1
	}

	cw := newColorWriter(stdout)

	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(stderr, "aigit: %v\n", err)
		return 1
	}

	if err := stageRequestedFiles(flags, repoRoot, stderr); err != nil {
		return 1
	}

	stagedFiles, err := git.StagedFiles(repoRoot)
	if err != nil {
		fmt.Fprintf(stderr, "aigit: %v\n", err)
		return 1
	}
	if len(stagedFiles) == 0 {
		fmt.Fprintln(stderr, "aigit: no staged changes. Stage files first, or use --dir / --all / [files...].")
		return 1
	}
	ui.PrintStagedFiles(cw, stagedFiles)

	diff, err := git.StagedDiff(repoRoot)
	if err != nil {
		fmt.Fprintf(stderr, "aigit: %v\n", err)
		return 1
	}
	warnIfDiffIsLarge(diff, cw)

	// Combine the system prompt with the diff so Ollama has full context.
	prompt := cfg.Prompt + "\n" + diff

	// Respect Ctrl-C: cancel the in-flight HTTP request gracefully.
	ctx, stopSignalHandler := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignalHandler()

	client := ollama.NewClient(cfg.URL)

	return runGenerateLoop(ctx, client, cfg.Model, prompt, flags.dryRun, repoRoot, stdin, stdout, stderr, cw)
}

// cliFlags holds the parsed command-line flags.
type cliFlags struct {
	dir    string
	all    bool
	dryRun bool
	model  string
	url    string
	files  []string // positional file arguments
}

// parseFlags parses os.Args-style arguments and returns a cliFlags struct.
// Errors are written to errOut (consistent with flag.FlagSet behaviour).
func parseFlags(args []string, errOut io.Writer) (cliFlags, error) {
	fs := flag.NewFlagSet("aigit", flag.ContinueOnError)
	fs.SetOutput(errOut)

	var f cliFlags
	fs.StringVar(&f.dir, "dir", "", "Stage all changes under `path` (relative to CWD)")
	fs.BoolVar(&f.all, "all", false, "Stage all tracked modified files (git add -u)")
	fs.BoolVar(&f.all, "a", false, "Shorthand for --all")
	fs.BoolVar(&f.dryRun, "dry-run", false, "Print the generated message but do not commit")
	fs.StringVar(&f.model, "model", "", "Ollama model to use (overrides config)")
	fs.StringVar(&f.url, "url", "", "Ollama base URL (overrides config)")

	if err := fs.Parse(args); err != nil {
		return cliFlags{}, err
	}
	f.files = fs.Args()
	return f, nil
}

// newColorWriter creates a ColorWriter for stdout, enabling ANSI colors only
// when stdout is a real terminal and NO_COLOR is not set.
func newColorWriter(stdout io.Writer) *ui.ColorWriter {
	f, isFile := stdout.(*os.File)
	isTTY := isFile && isTerminal(f)
	return ui.NewColorWriter(stdout, isTTY)
}

// findRepoRoot resolves the git repository root from the current working
// directory. This lets the tool be invoked from any subdirectory.
func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}
	return git.RepoRoot(cwd)
}

// stageRequestedFiles inspects the flags and stages the appropriate files.
// Exactly one of --dir, --all, or positional file args may be active; if
// none are set, we proceed with whatever is already staged.
func stageRequestedFiles(flags cliFlags, repoRoot string, stderr io.Writer) error {
	switch {
	case flags.dir != "":
		absDir, err := filepath.Abs(flags.dir)
		if err != nil {
			fmt.Fprintf(stderr, "aigit: invalid --dir %q: %v\n", flags.dir, err)
			return err
		}
		if err := git.StageDir(repoRoot, absDir); err != nil {
			fmt.Fprintf(stderr, "aigit: staging failed: %v\n", err)
			return err
		}

	case flags.all:
		if err := git.StageAll(repoRoot); err != nil {
			fmt.Fprintf(stderr, "aigit: staging failed: %v\n", err)
			return err
		}

	case len(flags.files) > 0:
		absPaths, err := toAbsPaths(flags.files)
		if err != nil {
			fmt.Fprintf(stderr, "aigit: %v\n", err)
			return err
		}
		if err := git.StageFiles(repoRoot, absPaths); err != nil {
			fmt.Fprintf(stderr, "aigit: staging failed: %v\n", err)
			return err
		}
	}
	return nil
}

// toAbsPaths converts a slice of relative or absolute paths to absolute paths.
func toAbsPaths(paths []string) ([]string, error) {
	abs := make([]string, len(paths))
	for i, p := range paths {
		a, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("invalid path %q: %w", p, err)
		}
		abs[i] = a
	}
	return abs, nil
}

// warnIfDiffIsLarge prints a yellow warning when the diff exceeds 50 KB.
// Very large diffs can degrade generation quality and slow the model down.
func warnIfDiffIsLarge(diff string, cw *ui.ColorWriter) {
	const warnThreshold = 50_000
	if len(diff) > warnThreshold {
		cw.Println(cw.Yellow("warning: diff is large (>50 KB). Consider narrowing scope with --dir or specific files."))
	}
}

// runGenerateLoop is the core retry loop. It calls Ollama, streams tokens to
// stdout, then prompts the user for [C]ommit / [E]dit / [R]etry / [A]bort.
// On Retry it loops back and calls Ollama again without re-staging.
func runGenerateLoop(
	ctx context.Context,
	client *ollama.Client,
	model, prompt string,
	dryRun bool,
	repoRoot string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	cw *ui.ColorWriter,
) int {
	for {
		cw.Printf("\n%s\n", cw.Cyan("Generating commit message..."))

		commitMsg, err := generateAndStream(ctx, client, model, prompt, stdout, cw)
		if err != nil {
			fmt.Fprintf(stderr, "aigit: %v\n", err)
			return 1
		}

		if dryRun {
			// --dry-run: message has been printed; exit without committing.
			return 0
		}

		choice, err := ui.PromptUser(stdin, stdout)
		if err != nil {
			fmt.Fprintf(stderr, "aigit: prompt error: %v\n", err)
			return 1
		}

		switch choice {
		case ui.ChoiceCommit:
			return commitWithMessage(commitMsg, repoRoot, stdout, stderr, cw)

		case ui.ChoiceEdit:
			return editAndCommit(commitMsg, repoRoot, stdout, stderr, cw)

		case ui.ChoiceRetry:
			continue // re-enter the loop; same diff, fresh generation

		case ui.ChoiceAbort:
			fmt.Fprintln(stdout, "Aborted.")
			return 1
		}
	}
}

// generateAndStream calls the Ollama API and writes each token to stdout in
// yellow as it arrives. Returns the complete, trimmed commit message.
func generateAndStream(
	ctx context.Context,
	client *ollama.Client,
	model, prompt string,
	stdout io.Writer,
	cw *ui.ColorWriter,
) (string, error) {
	body, err := client.Generate(ctx, model, prompt)
	if err != nil {
		return "", fmt.Errorf("generation failed: %w", err)
	}
	defer body.Close()

	msg, err := ollama.StreamTokens(body, func(tok string) {
		fmt.Fprint(stdout, cw.Yellow(tok))
	})
	fmt.Fprintln(stdout) // ensure the cursor moves to a new line after streaming
	return msg, err
}

// commitWithMessage runs git commit -m with the provided message and prints a
// success line showing just the subject (first line of the message).
func commitWithMessage(msg, repoRoot string, stdout, stderr io.Writer, cw *ui.ColorWriter) int {
	if err := git.Commit(repoRoot, msg); err != nil {
		fmt.Fprintf(stderr, "aigit: commit failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s %s\n", cw.Green("✓ Committed:"), subjectLine(msg))
	return 0
}

// editAndCommit opens the user's $EDITOR with the current message, waits for
// the editor to exit, then commits the edited text. An empty file aborts.
func editAndCommit(msg, repoRoot string, stdout, stderr io.Writer, cw *ui.ColorWriter) int {
	path, err := ui.WriteTempMessage(msg)
	if err != nil {
		fmt.Fprintf(stderr, "aigit: could not create temp file: %v\n", err)
		return 1
	}
	defer os.Remove(path)

	if err := ui.OpenEditor(path); err != nil {
		fmt.Fprintf(stderr, "aigit: editor error: %v\n", err)
		return 1
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "aigit: could not read edited message: %v\n", err)
		return 1
	}

	edited := strings.TrimSpace(string(data))
	if edited == "" {
		fmt.Fprintln(stdout, cw.Yellow("Empty message — aborting."))
		return 1
	}

	if err := git.Commit(repoRoot, edited); err != nil {
		fmt.Fprintf(stderr, "aigit: commit failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s %s\n", cw.Green("✓ Committed:"), subjectLine(edited))
	return 0
}

// subjectLine returns the first line of a commit message for display purposes.
func subjectLine(msg string) string {
	if idx := strings.Index(msg, "\n"); idx != -1 {
		return msg[:idx]
	}
	return msg
}

// isTerminal reports whether f is connected to an interactive terminal.
// This is used to decide whether to enable ANSI color output.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	// ModeCharDevice is set for TTYs and pseudo-TTYs; it is not set for
	// pipes, redirected files, or other non-interactive streams.
	return (info.Mode() & os.ModeCharDevice) != 0
}
