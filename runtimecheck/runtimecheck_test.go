package runtimecheck

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectorFindsPathAndAppBundleInstallations(t *testing.T) {
	d := detector{
		goos: "darwin",
		lookPath: func(name string) (string, error) {
			if name == "ollama" {
				return "/usr/local/bin/ollama", nil
			}
			return "", errors.New("not found")
		},
		stat: func(path string) error {
			if path == filepath.Join("/Applications", "LM Studio.app") {
				return nil
			}
			return errors.New("missing")
		},
		userHomeDir: func() (string, error) { return "/Users/test", nil },
	}

	runtimes := d.detect()
	if len(runtimes) != 2 {
		t.Fatalf("got %d runtimes, want 2", len(runtimes))
	}
	if runtimes[0].Name != RuntimeOllama {
		t.Fatalf("first runtime = %#v, want Ollama", runtimes[0])
	}
	if runtimes[1].Name != RuntimeLMStudio {
		t.Fatalf("second runtime = %#v, want LM Studio", runtimes[1])
	}
}

func TestUnavailableHintVariants(t *testing.T) {
	tests := []struct {
		name     string
		runtimes []Runtime
		provider string
		want     string
	}{
		{
			name:     "no runtimes",
			runtimes: nil,
			provider: "ollama",
			want:     "No local Ollama or LM Studio installation was detected.",
		},
		{
			name:     "ollama only",
			runtimes: []Runtime{{Name: RuntimeOllama, Installed: true}},
			provider: "lmstudio",
			want:     "Detected Ollama on this machine.",
		},
		{
			name:     "lm studio only",
			runtimes: []Runtime{{Name: RuntimeLMStudio, Installed: true}},
			provider: "ollama",
			want:     "Detected LM Studio on this machine.",
		},
		{
			name:     "both",
			runtimes: []Runtime{{Name: RuntimeOllama, Installed: true}, {Name: RuntimeLMStudio, Installed: true}},
			provider: "lmstudio",
			want:     "Detected Ollama on this machine.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnavailableHint(tt.runtimes, tt.provider)
			if !strings.HasPrefix(got, tt.want) {
				t.Fatalf("hint = %q, want prefix %q", got, tt.want)
			}
		})
	}
}
