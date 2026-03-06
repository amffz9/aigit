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
	if cfg.URL != "http://localhost:11434" {
		t.Errorf("default url: got %q", cfg.URL)
	}
}

func TestEnvVarOverridesDefault(t *testing.T) {
	t.Setenv("AIGIT_MODEL", "llama3.2")
	cfg, _ := config.Load(config.Overrides{}, "")
	if cfg.Model != "llama3.2" {
		t.Errorf("env override failed: got %q", cfg.Model)
	}
}

func TestCLIOverridesEnv(t *testing.T) {
	t.Setenv("AIGIT_MODEL", "llama3.2")
	cfg, _ := config.Load(config.Overrides{Model: "gemma3"}, "")
	if cfg.Model != "gemma3" {
		t.Errorf("cli override failed: got %q", cfg.Model)
	}
}

func TestFileOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]string{"model": "phi3"})
	os.WriteFile(cfgFile, data, 0644)

	cfg, _ := config.Load(config.Overrides{}, cfgFile)
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
