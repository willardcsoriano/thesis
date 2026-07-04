// Package ollama is a minimal client for the Ollama REST API (localhost:11434).
//
// It is decoupled from the rest of the runtime on purpose: everything the
// system knows about the inference engine lives here, so swapping the local
// SLM for a cloud OpenAI-compatible endpoint (decision D2) touches only this
// package. The walking-skeleton milestone uses Generate in non-streaming mode;
// a later milestone will add a streaming variant that feeds tokens into the TUI.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultBaseURL is the Ollama server address on the local machine.
const DefaultBaseURL = "http://localhost:11434"

// Client talks to a single Ollama server.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a Client for baseURL, falling back to DefaultBaseURL when empty.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: baseURL,
		// No overall timeout here: generation on CPU can take tens of seconds.
		// Callers control cancellation through the context they pass in.
		HTTP: &http.Client{},
	}
}

// generateRequest is the POST /api/generate body. Fields mirror the documented
// Ollama contract; stream is always false in this client for now.
type generateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	System  string         `json:"system,omitempty"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options,omitempty"`
}

// GenerateResponse is the single-object reply returned when stream=false.
// Only the fields the runtime and telemetry care about are decoded.
type GenerateResponse struct {
	Model           string `json:"model"`
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	TotalDuration   int64  `json:"total_duration"` // nanoseconds
	EvalDuration    int64  `json:"eval_duration"`  // nanoseconds
}

// Latency reports wall-clock generation time as a Duration.
func (r *GenerateResponse) Latency() time.Duration {
	return time.Duration(r.TotalDuration)
}

// Generate sends a single non-streaming completion request. system may be
// empty. options passes model parameters (e.g. {"temperature": 0}); pass nil
// for defaults.
func (c *Client) Generate(ctx context.Context, model, system, prompt string, options map[string]any) (*GenerateResponse, error) {
	body, err := json.Marshal(generateRequest{
		Model:   model,
		Prompt:  prompt,
		System:  system,
		Stream:  false,
		Options: options,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("ollama returned %s: %s", resp.Status, bytes.TrimSpace(snippet))
	}

	var out GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// Ping verifies the server is reachable by hitting /api/tags. It is the
// connectivity check the runtime runs at startup (scope.md).
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ollama not reachable at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health check at %s returned %s", c.BaseURL, resp.Status)
	}
	return nil
}
