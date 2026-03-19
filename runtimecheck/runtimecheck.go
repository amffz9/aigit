package runtimecheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	RuntimeOllama   = "Ollama"
	RuntimeLMStudio = "LM Studio"
)

// Runtime describes a detected local AI runtime installation.
type Runtime struct {
	Name      string
	Path      string
	Source    string
	Installed bool
}

type detector struct {
	goos        string
	lookPath    func(string) (string, error)
	stat        func(string) error
	userHomeDir func() (string, error)
}

// Detect returns the local runtimes that appear to be installed.
func Detect() []Runtime {
	d := detector{
		goos:        runtime.GOOS,
		lookPath:    exec.LookPath,
		stat:        statExists,
		userHomeDir: os.UserHomeDir,
	}
	return d.detect()
}

// HasRuntime reports whether a specific runtime was detected.
func HasRuntime(runtimes []Runtime, name string) bool {
	for _, rt := range runtimes {
		if rt.Name == name {
			return true
		}
	}
	return false
}

// UnavailableHint formats a user-facing hint when the selected provider is
// unreachable.
func UnavailableHint(runtimes []Runtime, provider string) string {
	hasOllama := HasRuntime(runtimes, RuntimeOllama)
	hasLMStudio := HasRuntime(runtimes, RuntimeLMStudio)

	switch {
	case provider == "ollama" && hasLMStudio:
		return "Detected LM Studio on this machine. Start Ollama or retry with --provider lmstudio."
	case provider == "lmstudio" && hasOllama:
		return "Detected Ollama on this machine. Start LM Studio's local server or retry with --provider ollama."
	case hasOllama && hasLMStudio:
		return "Detected local runtimes: Ollama and LM Studio. Start the selected provider or retry with --provider to switch."
	case hasOllama:
		return "Detected Ollama on this machine. It may be installed but not running. Start Ollama and retry."
	case hasLMStudio:
		return "Detected LM Studio on this machine. Start LM Studio's local server and retry."
	default:
		return "No local Ollama or LM Studio installation was detected. Install one of them, then retry."
	}
}

func (d detector) detect() []Runtime {
	var runtimes []Runtime
	if rt, ok := d.detectOllama(); ok {
		runtimes = append(runtimes, rt)
	}
	if rt, ok := d.detectLMStudio(); ok {
		runtimes = append(runtimes, rt)
	}
	return runtimes
}

func (d detector) detectOllama() (Runtime, bool) {
	if path, err := d.lookPath("ollama"); err == nil {
		return Runtime{Name: RuntimeOllama, Path: path, Source: "PATH", Installed: true}, true
	}
	for _, path := range d.appPaths("Ollama.app") {
		if err := d.stat(path); err == nil {
			return Runtime{Name: RuntimeOllama, Path: path, Source: "app bundle", Installed: true}, true
		}
	}
	return Runtime{}, false
}

func (d detector) detectLMStudio() (Runtime, bool) {
	for _, bin := range []string{"lms", "lmstudio", "lm-studio"} {
		if path, err := d.lookPath(bin); err == nil {
			return Runtime{Name: RuntimeLMStudio, Path: path, Source: "PATH", Installed: true}, true
		}
	}
	for _, path := range d.appPaths("LM Studio.app") {
		if err := d.stat(path); err == nil {
			return Runtime{Name: RuntimeLMStudio, Path: path, Source: "app bundle", Installed: true}, true
		}
	}
	return Runtime{}, false
}

func (d detector) appPaths(appName string) []string {
	switch d.goos {
	case "darwin":
		paths := []string{filepath.Join("/Applications", appName)}
		if home, err := d.userHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			paths = append(paths, filepath.Join(home, "Applications", appName))
		}
		return paths
	default:
		return nil
	}
}

func statExists(path string) error {
	_, err := os.Stat(path)
	return err
}

func (r Runtime) String() string {
	if r.Path == "" {
		return r.Name
	}
	return fmt.Sprintf("%s (%s)", r.Name, r.Path)
}
