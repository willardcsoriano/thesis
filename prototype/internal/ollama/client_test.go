package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewBaseURL(t *testing.T) {
	if got := New("").BaseURL; got != DefaultBaseURL {
		t.Errorf(`New("").BaseURL = %q, want %q`, got, DefaultBaseURL)
	}
	if got := New("http://example:1234").BaseURL; got != "http://example:1234" {
		t.Errorf("New(url).BaseURL = %q, want the url unchanged", got)
	}
}

func TestGenerateSendsExpectedRequestAndDecodesResponse(t *testing.T) {
	var gotBody generateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/generate" {
			t.Errorf("path = %s, want /api/generate", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(GenerateResponse{
			Model:           "qwen2.5-coder:3b",
			Response:        "ls -la",
			Done:            true,
			DoneReason:      "stop",
			PromptEvalCount: 5,
			EvalCount:       3,
			TotalDuration:   int64(250 * time.Millisecond),
		})
	}))
	defer server.Close()

	resp, err := New(server.URL).Generate(context.Background(), "qwen2.5-coder:3b", "sys prompt", "find pdfs", map[string]any{"temperature": 0})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if gotBody.Model != "qwen2.5-coder:3b" || gotBody.Prompt != "find pdfs" || gotBody.System != "sys prompt" {
		t.Errorf("request body = %+v, missing expected fields", gotBody)
	}
	if gotBody.Stream {
		t.Error("request body Stream = true, want false (this client never streams)")
	}
	if temp, ok := gotBody.Options["temperature"]; !ok || temp != float64(0) {
		t.Errorf("request body Options[\"temperature\"] = %v (present=%v), want 0", temp, ok)
	}

	if resp.Response != "ls -la" {
		t.Errorf("Response = %q, want %q", resp.Response, "ls -la")
	}
	if resp.EvalCount != 3 {
		t.Errorf("EvalCount = %d, want 3", resp.EvalCount)
	}
	if got, want := resp.Latency(), 250*time.Millisecond; got != want {
		t.Errorf("Latency() = %v, want %v", got, want)
	}
}

func TestGenerateOmitsOptionsWhenNil(t *testing.T) {
	var raw map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(GenerateResponse{Response: "ok"})
	}))
	defer server.Close()

	if _, err := New(server.URL).Generate(context.Background(), "m", "", "prompt", nil); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if _, present := raw["options"]; present {
		t.Error(`request body included "options" even though nil was passed (omitempty broken)`)
	}
}

func TestGenerateNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("model not found"))
	}))
	defer server.Close()

	_, err := New(server.URL).Generate(context.Background(), "m", "", "p", nil)
	if err == nil {
		t.Fatal("expected an error for a non-200 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error = %q, want it to mention the status and body", err.Error())
	}
}

func TestGenerateMalformedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	if _, err := New(server.URL).Generate(context.Background(), "m", "", "p", nil); err == nil {
		t.Fatal("expected a decode error for a malformed response body, got nil")
	}
}

func TestGenerateRespectsCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(GenerateResponse{Response: "ok"})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(server.URL).Generate(ctx, "m", "", "p", nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Generate with a cancelled context: err = %v, want it to wrap context.Canceled", err)
	}
}

func TestPingSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("path = %s, want /api/tags", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := New(server.URL).Ping(context.Background()); err != nil {
		t.Errorf("Ping() = %v, want nil", err)
	}
}

func TestPingNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := New(server.URL).Ping(context.Background())
	if err == nil {
		t.Fatal("expected an error for a non-200 /api/tags response, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %q, want it to mention the status", err.Error())
	}
}

func TestPingUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // closed before use: connecting now must fail like a downed Ollama server would

	err := New(server.URL).Ping(context.Background())
	if err == nil {
		t.Fatal("expected an error when the server is unreachable, got nil")
	}
	if !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("error = %q, want it to say the server isn't reachable", err.Error())
	}
}
