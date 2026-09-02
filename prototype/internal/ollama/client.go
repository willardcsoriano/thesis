// Package ollama is a minimal client for the Ollama REST API (localhost:11434).
//
// It is decoupled from the rest of the runtime on purpose: everything the
// system knows about the inference engine lives here, so swapping the local
// SLM for a cloud OpenAI-compatible endpoint (decision D2) touches only this
// package. Generate is the non-streaming call CLI mode uses; GenerateStream
// is its token-by-token counterpart, added for TUI mode (M3b).
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
// Ollama contract; Stream selects between Generate and GenerateStream.
type generateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	System  string         `json:"system,omitempty"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options,omitempty"`
}

// GenerateResponse is the reply body. With stream=false it is the whole
// response; with stream=true the identical shape arrives once per token,
// with only the final object carrying done_reason and the eval/timing
// counters. Only the fields the runtime and telemetry care about are decoded.
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

// GenerateStream is Generate with stream: true — same endpoint, same
// request shape, same return type — invoking onToken with each fragment
// as it arrives so a caller can render generation progressively. Pass a
// nil onToken to stream without per-token side effects.
//
// The wire format is newline-delimited JSON: one object per line, each
// carrying the same fields GenerateResponse already decodes, with only
// the final line (done: true) populating done_reason and the eval/timing
// counters. A json.Decoder reads that sequence natively, so no separate
// line-splitting step is needed. The returned response is the final
// chunk with Response replaced by the full concatenated text, which
// makes it a drop-in substitute for Generate's return value.
//
// A stream that ends without ever reporting done: true is treated as an
// error rather than as a short result, and this is a safety property
// rather than strictness for its own sake: the caller turns this text
// into a shell command, and a truncated response is not merely
// incomplete but can be actively dangerous — a connection dropped
// partway through "rm -rf /home/user/tmp" yields a perfectly valid,
// catastrophically different command. Refusing to return partial text
// means a truncated generation can never reach the classifier or the
// executor at all.
func (c *Client) GenerateStream(ctx context.Context, model, system, prompt string, options map[string]any, onToken func(string)) (*GenerateResponse, error) {
	body, err := json.Marshal(generateRequest{
		Model:   model,
		Prompt:  prompt,
		System:  system,
		Stream:  true,
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

	var (
		final     GenerateResponse
		full      strings.Builder
		sawDone   bool
		dec       = json.NewDecoder(resp.Body)
		chunkSeen bool
	)
	for {
		var chunk GenerateResponse
		if err := dec.Decode(&chunk); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode stream: %w", err)
		}
		chunkSeen = true

		if chunk.Response != "" {
			full.WriteString(chunk.Response)
			if onToken != nil {
				onToken(chunk.Response)
			}
		}
		if chunk.Done {
			final, sawDone = chunk, true
			break
		}
	}

	if !sawDone {
		if !chunkSeen {
			return nil, fmt.Errorf("stream ended before any response was received")
		}
		return nil, fmt.Errorf("stream ended after %d bytes without a completion marker (truncated response)", full.Len())
	}

	final.Response = full.String()
	return &final, nil
}

// ChatMessage is one turn in a /api/chat conversation. ToolCalls is set by
// the model on an assistant message when it invokes one of the Tools passed
// to Chat; it is empty on every other message.
type ChatMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Tool describes one function the model may call via ToolCalls, in the
// shape Ollama's /api/chat endpoint expects (mirrors OpenAI's function-
// calling schema: JSON Schema for Parameters).
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the callable part of a Tool: its name, a natural-language
// description the model uses to decide when to call it, and a JSON Schema
// object describing its arguments.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall is one invocation the model made against a Tool it was offered.
// Arguments is decoded from whatever JSON the model produced for the
// function's parameters — callers must validate its shape themselves before
// trusting it, same as any other model output.
type ToolCall struct {
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction names which Tool the model invoked and with what
// arguments.
type ToolCallFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// chatRequest is the POST /api/chat body. stream is always false in this
// client, matching Generate.
type chatRequest struct {
	Model    string         `json:"model"`
	Messages []ChatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Tools    []Tool         `json:"tools,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

// ChatResponse is the single-object reply returned when stream=false.
type ChatResponse struct {
	Model      string      `json:"model"`
	Message    ChatMessage `json:"message"`
	Done       bool        `json:"done"`
	DoneReason string      `json:"done_reason"`
}

// Chat sends a single non-streaming /api/chat request, optionally offering
// tools the model may call. Unlike Generate (POST /api/generate, a single
// prompt string), Chat carries a role-tagged message list and is the only
// Ollama endpoint that supports native tool-calling — see
// cmd/synapse/layer4_test.go for why this repo also treats a populated
// ToolCalls field as something to verify empirically rather than assume.
func (c *Client) Chat(ctx context.Context, model string, messages []ChatMessage, tools []Tool, options map[string]any) (*ChatResponse, error) {
	body, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
		Tools:    tools,
		Options:  options,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
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

	var out ChatResponse
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
