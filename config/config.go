// Package config resolves aigit's runtime configuration from multiple sources.
//
// Settings are applied in priority order (highest to lowest):
//
//  1. CLI flags (--model, --url)
//  2. Environment variables (AIGIT_MODEL, AIGIT_URL, AIGIT_PROMPT)
//  3. Config file (~/.config/aigit/config.json, or a custom path)
//  4. Built-in defaults (model: qwen3:4b, url: http://localhost:11434)
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Default values used when no override is provided.
const (
	DefaultModel = "qwen3:4b"
	DefaultURL   = "http://localhost:11434"
)

// DefaultPrompt is the system prompt sent to Ollama before the git diff.
// It instructs the model to produce a clean, conventional commit message
// with no extra commentary, markdown, or trailers.
const DefaultPrompt = `You are an expert software engineer writing git commit messages.

Given a git diff, write a commit message following these rules:

Subject line (first line):
- Imperative mood, 72 characters or fewer, no trailing period
- Summarise the overall intent of the change, not the mechanism
- Do NOT mention file names unless the file itself is the entire point
- Do NOT start with "This commit", "This change", "I", or similar phrases

Body (after a blank line separating it from the subject):
- Always include a body unless the diff is trivially small (e.g. a typo fix)
- Wrap lines at 72 characters
- Explain WHY the change was made and any important context or trade-offs
- For changes that touch multiple areas, use a short bullet list (- item) to
  describe each logical area of change — one bullet per concern, not per file
- Be as detailed as the diff warrants; do not truncate or summarise away
  important context just to keep the message short

Formatting rules:
- Do NOT add a sign-off, Co-authored-by, or any trailer lines
- Do NOT wrap your output in markdown code fences
- Do NOT add any explanation or preamble outside the commit message itself
- Output ONLY the raw commit message text, nothing else

Git diff to summarize:`

// Overrides carries values explicitly set via CLI flags.
// An empty string means "not set" — the next priority level will be used.
type Overrides struct {
	Model string
	URL   string
}

// Config is the fully resolved configuration ready for use by the CLI.
type Config struct {
	Model  string // Ollama model name (e.g. "qwen3:4b")
	URL    string // Ollama base URL (e.g. "http://localhost:11434")
	Prompt string // System prompt prepended to the git diff
}

// Load builds a Config by merging defaults, config file, environment variables,
// and CLI flag overrides — in that order (later sources win).
//
// cfgPath is the path to a JSON config file. Pass an empty string to use the
// default location (~/.config/aigit/config.json). A missing file is silently
// ignored; a malformed file returns an error.
func Load(overrides Overrides, cfgPath string) (Config, error) {
	cfg := defaultConfig()

	resolvedPath := resolveConfigPath(cfgPath)
	if err := applyFileConfig(resolvedPath, &cfg); err != nil {
		return Config{}, err
	}

	applyEnvVars(&cfg)
	applyOverrides(overrides, &cfg)

	return cfg, nil
}

// defaultConfig returns a Config populated with built-in defaults.
func defaultConfig() Config {
	return Config{
		Model:  DefaultModel,
		URL:    DefaultURL,
		Prompt: DefaultPrompt,
	}
}

// resolveConfigPath returns cfgPath if non-empty, otherwise the standard
// XDG-style path: ~/.config/aigit/config.json.
func resolveConfigPath(cfgPath string) string {
	if cfgPath != "" {
		return cfgPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "aigit", "config.json")
}

// applyFileConfig reads a JSON config file and merges non-empty values into cfg.
// A missing file is silently ignored (not an error).
func applyFileConfig(path string, cfg *Config) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return err
	}
	fc.applyTo(cfg)
	return nil
}

// applyEnvVars merges AIGIT_* environment variables into cfg, overwriting any
// value set by the config file.
func applyEnvVars(cfg *Config) {
	if v := os.Getenv("AIGIT_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("AIGIT_URL"); v != "" {
		cfg.URL = v
	}
	if v := os.Getenv("AIGIT_PROMPT"); v != "" {
		cfg.Prompt = v
	}
}

// applyOverrides merges CLI flag values into cfg, giving them the highest
// priority. Only non-empty override values take effect.
func applyOverrides(o Overrides, cfg *Config) {
	if o.Model != "" {
		cfg.Model = o.Model
	}
	if o.URL != "" {
		cfg.URL = o.URL
	}
}

// fileConfig mirrors the JSON structure of ~/.config/aigit/config.json.
type fileConfig struct {
	Model  string `json:"model"`
	URL    string `json:"url"`
	Prompt string `json:"prompt"`
}

// applyTo copies non-empty fileConfig fields into cfg.
func (fc fileConfig) applyTo(cfg *Config) {
	if fc.Model != "" {
		cfg.Model = fc.Model
	}
	if fc.URL != "" {
		cfg.URL = fc.URL
	}
	if fc.Prompt != "" {
		cfg.Prompt = fc.Prompt
	}
}
