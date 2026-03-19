package cmd

import (
	"aigit/lmstudio"
	"aigit/ollama"
	"aigit/runtimecheck"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	providerAuto     = "auto"
	providerOllama   = "ollama"
	providerLMStudio = "lmstudio"

	defaultOllamaURL   = "http://localhost:11434"
	defaultLMStudioURL = "http://127.0.0.1:1234"
)

type generationClient interface {
	ProviderName() string
	DisplayName() string
	CurrentModel(context.Context) (string, error)
	Generate(context.Context, string, string, string) (io.ReadCloser, error)
	StreamTokens(io.Reader, func(string), func()) (string, error)
	IsUnavailable(error) bool
}

type clientCandidate struct {
	provider string
	url      string
	client   generationClient
}

type clientPlan struct {
	primary          clientCandidate
	fallback         *clientCandidate
	providerExplicit bool
}

type ollamaGenerationClient struct {
	inner *ollama.Client
}

type lmStudioGenerationClient struct {
	inner *lmstudio.Client
}

var (
	newOllamaClient   = func(baseURL string) generationClient { return ollamaGenerationClient{inner: ollama.NewClient(baseURL)} }
	newLMStudioClient = func(baseURL string) generationClient {
		return lmStudioGenerationClient{inner: lmstudio.NewClient(baseURL)}
	}
	detectProviderAtURL = inferProviderAtURL
)

func (c ollamaGenerationClient) ProviderName() string { return providerOllama }
func (c ollamaGenerationClient) DisplayName() string  { return "Ollama" }
func (c ollamaGenerationClient) CurrentModel(ctx context.Context) (string, error) {
	return c.inner.CurrentModel(ctx)
}
func (c ollamaGenerationClient) Generate(ctx context.Context, model, systemPrompt, userPrompt string) (io.ReadCloser, error) {
	return c.inner.Generate(ctx, model, systemPrompt+"\n\n"+userPrompt)
}
func (c ollamaGenerationClient) StreamTokens(r io.Reader, onToken func(string), onThinking func()) (string, error) {
	return ollama.StreamTokens(r, onToken, onThinking)
}
func (c ollamaGenerationClient) IsUnavailable(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "ollama unreachable")
}

func (c lmStudioGenerationClient) ProviderName() string { return providerLMStudio }
func (c lmStudioGenerationClient) DisplayName() string  { return "LM Studio" }
func (c lmStudioGenerationClient) CurrentModel(ctx context.Context) (string, error) {
	return c.inner.CurrentModel(ctx)
}
func (c lmStudioGenerationClient) Generate(ctx context.Context, model, systemPrompt, userPrompt string) (io.ReadCloser, error) {
	return c.inner.Generate(ctx, model, systemPrompt, userPrompt)
}
func (c lmStudioGenerationClient) StreamTokens(r io.Reader, onToken func(string), onThinking func()) (string, error) {
	return lmstudio.StreamTokens(r, onToken, onThinking)
}
func (c lmStudioGenerationClient) IsUnavailable(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "lm studio unreachable")
}

func resolveClientPlan(provider, rawURL string) (clientPlan, error) {
	normalizedProvider, explicitProvider, err := normalizeProvider(provider)
	if err != nil {
		return clientPlan{}, err
	}

	url := strings.TrimSpace(rawURL)
	if explicitProvider {
		return clientPlan{
			primary: clientCandidate{
				provider: normalizedProvider,
				url:      pickURL(normalizedProvider, url),
				client:   newClient(normalizedProvider, pickURL(normalizedProvider, url)),
			},
			providerExplicit: true,
		}, nil
	}

	if url != "" {
		primaryProvider := detectProviderAtURL(url)
		return clientPlan{
			primary: clientCandidate{
				provider: primaryProvider,
				url:      url,
				client:   newClient(primaryProvider, url),
			},
		}, nil
	}

	runtimes := detectRuntimes()
	if runtimecheck.HasRuntime(runtimes, runtimecheck.RuntimeOllama) {
		primary := clientCandidate{
			provider: providerOllama,
			url:      defaultOllamaURL,
			client:   newOllamaClient(defaultOllamaURL),
		}
		if runtimecheck.HasRuntime(runtimes, runtimecheck.RuntimeLMStudio) {
			fallback := clientCandidate{
				provider: providerLMStudio,
				url:      defaultLMStudioURL,
				client:   newLMStudioClient(defaultLMStudioURL),
			}
			return clientPlan{primary: primary, fallback: &fallback}, nil
		}
		return clientPlan{primary: primary}, nil
	}

	if runtimecheck.HasRuntime(runtimes, runtimecheck.RuntimeLMStudio) {
		return clientPlan{
			primary: clientCandidate{
				provider: providerLMStudio,
				url:      defaultLMStudioURL,
				client:   newLMStudioClient(defaultLMStudioURL),
			},
		}, nil
	}

	return clientPlan{
		primary: clientCandidate{
			provider: providerOllama,
			url:      defaultOllamaURL,
			client:   newOllamaClient(defaultOllamaURL),
		},
	}, nil
}

func normalizeProvider(provider string) (string, bool, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", providerAuto:
		return providerAuto, false, nil
	case providerOllama:
		return providerOllama, true, nil
	case providerLMStudio, "lm-studio", "lm_studio":
		return providerLMStudio, true, nil
	default:
		return "", false, fmt.Errorf("unsupported provider %q (supported: auto, ollama, lmstudio)", provider)
	}
}

func preferredAutoProvider(runtimes []runtimecheck.Runtime) string {
	if runtimecheck.HasRuntime(runtimes, runtimecheck.RuntimeOllama) {
		return providerOllama
	}
	if runtimecheck.HasRuntime(runtimes, runtimecheck.RuntimeLMStudio) {
		return providerLMStudio
	}
	return providerOllama
}

func pickURL(provider, url string) string {
	if strings.TrimSpace(url) != "" {
		return strings.TrimSpace(url)
	}
	if provider == providerLMStudio {
		return defaultLMStudioURL
	}
	return defaultOllamaURL
}

func newClient(provider, url string) generationClient {
	if provider == providerLMStudio {
		return newLMStudioClient(url)
	}
	return newOllamaClient(url)
}

func inferProviderAtURL(rawURL string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if baseURL == "" {
		return providerOllama
	}

	httpClient := &http.Client{Timeout: 1 * time.Second}
	probes := []struct {
		provider string
		path     string
	}{
		{provider: providerOllama, path: "/api/tags"},
		{provider: providerLMStudio, path: "/v1/models"},
	}

	for _, probe := range probes {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+probe.path, nil)
		if err != nil {
			continue
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return probe.provider
		}
	}

	return preferredAutoProvider(detectRuntimes())
}
