# aigit

`aigit` generates git commit messages from your staged diff using a local
LLM provider. It prefers [Ollama](https://ollama.com) by default, falls back
to [LM Studio](https://lmstudio.ai) when Ollama is not detected, and then lets
you commit, edit, retry, or abort — all from the terminal.

```
$ aigit --dir src/auth

Staged files:
  + jwt.go
  + middleware.go

Generating commit message...
feat(auth): add JWT validation middleware

[C]ommit  [E]dit  [R]etry  [A]bort > c
✓ Committed: feat(auth): add JWT validation middleware
```

---

## Requirements

| Requirement | Version |
|---|---|
| Go | 1.22 or later |
| Ollama or LM Studio | any recent version |
| A running local model | default: auto-select first listed model |

No external Go dependencies — stdlib only.

---

## Installation

From a local checkout of this repository:

```bash
go build -ldflags="-s -w" -o aigit .

# Move the binary somewhere on your PATH
mv aigit /usr/local/bin/
```

---

## Quick Start

1. **Start your local provider** with a chosen model:
   ```bash
   ollama run qwen3:4b
   ```

2. **Stage some files** as you normally would, then run `aigit`:
   ```bash
   git add src/auth.go
   aigit
   ```

3. **Review the generated message.** At the prompt, press:
   - `c` — commit with the message as shown
   - `e` — open `$EDITOR` to refine the message, then commit
   - `r` — discard and generate a new message from the same diff
   - `a` — abort; nothing is committed

---

## Usage

```
aigit [flags] [files...]
```

### Flags

| Flag | Description |
|---|---|
| `--dir <path>` | Stage all changes under `<path>` (relative to CWD) |
| `--all` / `-a` | Stage tracked and untracked changes (`git add -A`) |
| `--dry-run` | Print the generated message but do not commit |
| `--config <path>` | Read config from a custom JSON file path |
| `--provider <provider>` | Provider to use: `auto`, `ollama`, or `lmstudio` |
| `--model <model>` | Model to use (overrides config; `auto` picks the provider default/first listed model) |
| `--url <url>` | Provider base URL (overrides config) |
| `[files...]` | Stage these specific files, then generate |

Ollama reasoning-capable models are supported, and `aigit` suppresses hidden
reasoning text from the terminal and the final commit message. If the selected
provider is unreachable, `aigit` checks whether Ollama or LM Studio appears to
be installed locally and prints a more specific setup hint.

### Examples

```bash
# Use whatever is already staged
aigit

# Stage an entire directory relative to CWD
aigit --dir src/

# Stage specific files
aigit src/auth.go src/middleware.go

# Stage everything, including untracked files
aigit --all
aigit -a

# Preview without committing
aigit --dry-run

# Use a different model for this run
aigit --model llama3.2

# Force LM Studio for this run
aigit --provider lmstudio

# Override the provider URL
aigit --url http://192.168.1.10:11434

# Run from a subdirectory — paths are always relative to CWD
cd src/auth
aigit --dir .
```

---

## Configuration

Settings are resolved in priority order (highest first):

1. **CLI flags** (`--config`, `--provider`, `--model`, `--url`)
2. **Environment variables** (`AIGIT_PROVIDER`, `AIGIT_MODEL`, `AIGIT_URL`, `AIGIT_PROMPT`)
3. **Config file** (`--config <path>` or `~/.config/aigit/config.json`)
4. **Defaults** (provider: `auto`, model: `auto`)

### Config file

Create `~/.config/aigit/config.json`:

```json
{
  "provider": "auto",
  "model": "llama3.2",
  "url": "http://localhost:11434",
  "prompt": "Optional custom system prompt..."
}
```

All fields are optional. Omitted fields fall back to the defaults or env vars.

### Environment variables

| Variable | Description |
|---|---|
| `AIGIT_PROVIDER` | Provider name: `auto`, `ollama`, or `lmstudio` |
| `AIGIT_MODEL` | Provider model name (or `auto`) |
| `AIGIT_URL` | Provider base URL |
| `AIGIT_PROMPT` | Custom prompt content; `aigit` still wraps the diff with its safety preamble |

When `provider` is `auto` and no custom URL is set, `aigit` prefers Ollama if
it is installed, otherwise it uses LM Studio if detected. If both are
installed and Ollama is unavailable at runtime, `aigit` retries once with
LM Studio automatically.

### NO_COLOR

`aigit` respects the [`NO_COLOR`](https://no-color.org) convention. Set
`NO_COLOR=1` to disable all ANSI colour output. Colours are also automatically
disabled when stdout is not a terminal (e.g. when piping output).

---

## Editing messages

When you press `e` at the prompt, `aigit` writes the generated message to a
temporary file and opens it in your `$EDITOR`. Save and quit to continue; an
empty file aborts the commit.

Set your preferred editor in your shell profile:

```bash
export EDITOR="code --wait"   # VS Code
export EDITOR="nano"
export EDITOR="vim"
```

---

## Subdirectory support

`aigit` detects the repository root by walking up from your current working
directory, so it works correctly no matter where you are in the repo. All
path arguments (`--dir`, positional files) are resolved relative to CWD.

```bash
cd deeply/nested/package
aigit --dir .          # stages everything in this directory
aigit auth.go utils.go # stages these two files
```

---

## Large diffs

If your staged diff exceeds 50 KB, `aigit` prints a warning and continues.
If it exceeds 200 KB, `aigit` aborts before contacting Ollama so you can
narrow the scope first. Very large diffs can reduce generation quality.
Consider narrowing the scope:

```bash
aigit --dir src/specific-package/
aigit src/changed-file.go
```

---

## Project structure

```
aigit/
├── main.go          # Entry point
├── cmd/root.go      # Flag parsing, retry loop, orchestration
├── git/git.go       # Git operations via os/exec
├── ollama/client.go # Streaming HTTP client for Ollama
├── config/config.go # Priority-based config resolution
├── ui/ui.go         # Color output, interactive prompt, editor launch
├── go.mod
└── Makefile
```

---

## Development

```bash
# Run all tests
go test ./... -v

# Build
make build

# Run tests via make
make test
```

---

## License

MIT
