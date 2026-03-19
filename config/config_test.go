package config_test

import (
	"aigit/config"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg, err := config.Load(config.Overrides{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "auto" {
		t.Errorf("default model: got %q", cfg.Model)
	}
	if cfg.Provider != "auto" {
		t.Errorf("default provider: got %q", cfg.Provider)
	}
	if cfg.URL != "" {
		t.Errorf("default url: got %q", cfg.URL)
	}
}

func TestEnvVarOverridesDefault(t *testing.T) {
	t.Setenv("AIGIT_PROVIDER", "lmstudio")
	t.Setenv("AIGIT_MODEL", "llama3.2")
	cfg, _ := config.Load(config.Overrides{}, "")
	if cfg.Provider != "lmstudio" {
		t.Errorf("env provider override failed: got %q", cfg.Provider)
	}
	if cfg.Model != "llama3.2" {
		t.Errorf("env override failed: got %q", cfg.Model)
	}
}

func TestCLIOverridesEnv(t *testing.T) {
	t.Setenv("AIGIT_PROVIDER", "ollama")
	t.Setenv("AIGIT_MODEL", "llama3.2")
	cfg, _ := config.Load(config.Overrides{Provider: "lmstudio", Model: "gemma3"}, "")
	if cfg.Provider != "lmstudio" {
		t.Errorf("cli provider override failed: got %q", cfg.Provider)
	}
	if cfg.Model != "gemma3" {
		t.Errorf("cli override failed: got %q", cfg.Model)
	}
}

func TestFileOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]string{"provider": "lmstudio", "model": "phi3"})
	os.WriteFile(cfgFile, data, 0644)

	cfg, _ := config.Load(config.Overrides{}, cfgFile)
	if cfg.Provider != "lmstudio" {
		t.Errorf("file provider override failed: got %q", cfg.Provider)
	}
	if cfg.Model != "phi3" {
		t.Errorf("file override failed: got %q", cfg.Model)
	}
}

func TestMissingFileIsNotError(t *testing.T) {
	_, err := config.Load(config.Overrides{}, "/nonexistent/config.json")
	if err != nil {
		t.Errorf("missing config file should not error: %v", err)
	}
}
