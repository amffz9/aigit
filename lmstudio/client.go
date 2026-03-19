package lmstudio

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

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type listModelsResponse struct {
	Data []modelInfo `json:"data"`
}

type modelInfo struct {
	ID string `json:"id"`
}

type streamResponse struct {
	Choices []streamChoice `json:"choices"`
	Error   *apiError      `json:"error,omitempty"`
}

type streamChoice struct {
	Delta streamDelta `json:"delta"`
}

type streamDelta struct {
	Content any `json:"content"`
}

type apiError struct {
	Message string `json:"message"`
}

// Client calls a local LM Studio server via its OpenAI-compatible API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
	}
}

func (c *Client) Generate(ctx context.Context, model, systemPrompt, userPrompt string) (io.ReadCloser, error) {
	reqBody := chatCompletionRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: true,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lm studio unreachable: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("lm studio returned %d: %s", resp.StatusCode, body)
	}
	return resp.Body, nil
}

func (c *Client) CurrentModel(ctx context.Context) (string, error) {
	var installed listModelsResponse
	if err := c.getJSON(ctx, c.BaseURL+"/v1/models", &installed); err != nil {
		return "", err
	}
	for _, model := range installed.Data {
		if strings.TrimSpace(model.ID) != "" {
			return model.ID, nil
		}
	}
	return "", fmt.Errorf("no LM Studio models found; load a model in LM Studio or pass --model")
}

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("lm studio unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("lm studio returned %d: %s", resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	return nil
}

// StreamTokens reads SSE chat-completion chunks from LM Studio.
func StreamTokens(r io.Reader, onToken func(string), onThinking func()) (string, error) {
	_ = onThinking
	reader := bufio.NewReader(r)
	var sb strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, ":") {
				if err == io.EOF {
					break
				}
				if err != nil {
					return "", err
				}
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				if err == io.EOF {
					break
				}
				if err != nil {
					return "", err
				}
				continue
			}

			payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if payload == "[DONE]" {
				break
			}

			var resp streamResponse
			if jsonErr := json.Unmarshal([]byte(payload), &resp); jsonErr != nil {
				return "", fmt.Errorf("parse error: %w", jsonErr)
			}
			if resp.Error != nil && resp.Error.Message != "" {
				return "", fmt.Errorf("lm studio: %s", resp.Error.Message)
			}
			for _, choice := range resp.Choices {
				content := extractContent(choice.Delta.Content)
				if content == "" {
					continue
				}
				onToken(content)
				sb.WriteString(content)
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

func extractContent(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, item := range v {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				b.WriteString(text)
			}
		}
		return b.String()
	default:
		return ""
	}
}
