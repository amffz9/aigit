package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
	Error    string `json:"error,omitempty"`
}

type listModelsResponse struct {
	Models []modelInfo `json:"models"`
}

type modelInfo struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}

// Generator is the interface for commit message generation.
type Generator interface {
	Generate(ctx context.Context, model, prompt string) (io.ReadCloser, error)
}

// Client calls a real Ollama instance.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		// Avoid a hard end-to-end timeout for streaming generation; requests are
		// canceled via context (e.g. Ctrl-C) instead.
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
	}
}

func (c *Client) Generate(ctx context.Context, model, prompt string) (io.ReadCloser, error) {
	reqBody := generateRequest{
		Model:  model,
		Prompt: prompt,
		Stream: true,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/generate", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama unreachable: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, body)
	}
	return resp.Body, nil
}

// CurrentModel returns the best model choice for "auto" mode:
//  1. first currently loaded model from /api/ps
//  2. first installed model from /api/tags
func (c *Client) CurrentModel(ctx context.Context) (string, error) {
	var running listModelsResponse
	if err := c.getJSON(ctx, c.BaseURL+"/api/ps", &running); err == nil {
		if model := firstModelName(running.Models); model != "" {
			return model, nil
		}
	}

	var installed listModelsResponse
	if err := c.getJSON(ctx, c.BaseURL+"/api/tags", &installed); err != nil {
		return "", err
	}
	if model := firstModelName(installed.Models); model != "" {
		return model, nil
	}
	return "", fmt.Errorf("no Ollama models found; run `ollama pull <model>` or pass --model")
}

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama returned %d: %s", resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	return nil
}

func firstModelName(models []modelInfo) string {
	for _, m := range models {
		if m.Name != "" {
			return m.Name
		}
		if m.Model != "" {
			return m.Model
		}
	}
	return ""
}

// StreamTokens reads NDJSON tokens from r, calling onToken for each partial token.
// Returns the complete assembled message.
//
// We use bufio.Reader.ReadBytes instead of bufio.Scanner because Scanner has a
// hard 64 KB max token size. ReadBytes grows its internal buffer on demand, so
// it handles arbitrarily large NDJSON lines — important for large multi-file diffs
// where the model may produce long output in a single streaming chunk.
func StreamTokens(r io.Reader, onToken func(string)) (string, error) {
	reader := bufio.NewReader(r)
	var sb strings.Builder

	for {
		// ReadBytes reads until '\n', growing its buffer as needed — no size limit.
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			// Trim the trailing newline before unmarshalling.
			line = []byte(strings.TrimRight(string(line), "\n\r"))
			if len(line) == 0 {
				if err != nil {
					break
				}
				continue
			}

			var resp generateResponse
			if jsonErr := json.Unmarshal(line, &resp); jsonErr != nil {
				return "", fmt.Errorf("parse error: %w", jsonErr)
			}
			if resp.Error != "" {
				return "", fmt.Errorf("ollama: %s", resp.Error)
			}
			if resp.Response != "" {
				onToken(resp.Response)
				sb.WriteString(resp.Response)
			}
			if resp.Done {
				break
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}

	return strings.TrimSpace(sb.String()), nil
}
