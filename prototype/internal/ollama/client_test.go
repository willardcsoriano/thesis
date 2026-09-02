package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestChatSendsExpectedRequestAndDecodesToolCalls(t *testing.T) {
	var gotBody chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %s, want /api/chat", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(ChatResponse{
			Model: "qwen2.5-coder:7b",
			Message: ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{Function: ToolCallFunction{
						Name:      "move_files",
						Arguments: map[string]any{"source_dir": "/home/u/Desktop", "dest_dir": "Screenshots"},
					}},
				},
			},
			Done:       true,
			DoneReason: "stop",
		})
	}))
	defer server.Close()

	tools := []Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "move_files",
			Description: "Move files matching a pattern into a destination directory.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"source_dir": map[string]any{"type": "string"}},
			},
		},
	}}
	messages := []ChatMessage{{Role: "user", Content: "move my screenshots"}}

	resp, err := New(server.URL).Chat(context.Background(), "qwen2.5-coder:7b", messages, tools, map[string]any{"temperature": 0})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}

	if gotBody.Model != "qwen2.5-coder:7b" || len(gotBody.Messages) != 1 || gotBody.Messages[0].Content != "move my screenshots" {
		t.Errorf("request body = %+v, missing expected fields", gotBody)
	}
	if gotBody.Stream {
		t.Error("request body Stream = true, want false (this client never streams)")
	}
	if len(gotBody.Tools) != 1 || gotBody.Tools[0].Function.Name != "move_files" {
		t.Errorf("request body Tools = %+v, want the move_files tool", gotBody.Tools)
	}

	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("Message.ToolCalls = %d entries, want 1", len(resp.Message.ToolCalls))
	}
	call := resp.Message.ToolCalls[0]
	if call.Function.Name != "move_files" {
		t.Errorf("ToolCalls[0].Function.Name = %q, want move_files", call.Function.Name)
	}
	if call.Function.Arguments["dest_dir"] != "Screenshots" {
		t.Errorf("ToolCalls[0].Function.Arguments[dest_dir] = %v, want Screenshots", call.Function.Arguments["dest_dir"])
	}
}

func TestChatOmitsToolsWhenNil(t *testing.T) {
	var raw map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(ChatResponse{Message: ChatMessage{Content: "ok"}})
	}))
	defer server.Close()

	messages := []ChatMessage{{Role: "user", Content: "hi"}}
	if _, err := New(server.URL).Chat(context.Background(), "m", messages, nil, nil); err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if _, present := raw["tools"]; present {
		t.Error(`request body included "tools" even though nil was passed (omitempty broken)`)
	}
}

func TestChatDecodesPlainContentWithNoToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{Message: ChatMessage{Role: "assistant", Content: `{"op":"move_files"}`}})
	}))
	defer server.Close()

	resp, err := New(server.URL).Chat(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil, nil)
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if len(resp.Message.ToolCalls) != 0 {
		t.Errorf("ToolCalls = %+v, want none — this is the documented qwen2.5-coder shape (freeform JSON in Content, not a populated ToolCalls field)", resp.Message.ToolCalls)
	}
	if resp.Message.Content != `{"op":"move_files"}` {
		t.Errorf("Content = %q, want the raw freeform JSON preserved", resp.Message.Content)
	}
}

func TestChatNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("model not found"))
	}))
	defer server.Close()

	_, err := New(server.URL).Chat(context.Background(), "m", nil, nil, nil)
	if err == nil {
		t.Fatal("expected an error for a non-200 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error = %q, want it to mention the status and body", err.Error())
	}
}

func TestChatRespectsCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{Message: ChatMessage{Content: "ok"}})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(server.URL).Chat(ctx, "m", nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Chat with a cancelled context: err = %v, want it to wrap context.Canceled", err)
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

// --- GenerateStream (M3b step 4) -------------------------------------

// streamingServer returns a server that writes each supplied line as its
// own NDJSON chunk and flushes between them, so the client genuinely sees
// a progressive stream rather than one buffered blob — otherwise a test
// could pass against an implementation that only works on whole responses.
func streamingServer(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server response writer does not support flushing")
		}
		for _, c := range chunks {
			if _, err := io.WriteString(w, c+"\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}))
}

func TestGenerateStreamAssemblesTokensAndReportsStats(t *testing.T) {
	server := streamingServer(t, []string{
		`{"model":"m","response":"ls ","done":false}`,
		`{"model":"m","response":"-la","done":false}`,
		`{"model":"m","response":"","done":true,"done_reason":"stop","eval_count":7,"total_duration":1500000000}`,
	})
	defer server.Close()

	var tokens []string
	resp, err := New(server.URL).GenerateStream(context.Background(), "m", "sys", "prompt", nil, func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatalf("GenerateStream error: %v", err)
	}

	if got, want := resp.Response, "ls -la"; got != want {
		t.Errorf("assembled response = %q, want %q", got, want)
	}
	if got := strings.Join(tokens, "|"); got != "ls |-la" {
		t.Errorf("onToken saw %q, want %q — tokens must arrive individually, not as one blob", got, "ls |-la")
	}
	if resp.EvalCount != 7 {
		t.Errorf("EvalCount = %d, want 7 (stats come from the final chunk)", resp.EvalCount)
	}
	if resp.Latency() != 1500*time.Millisecond {
		t.Errorf("Latency() = %v, want 1.5s", resp.Latency())
	}
	if !resp.Done {
		t.Error("Done = false, want true on a completed stream")
	}
}

// TestGenerateStreamSendsStreamTrue guards the one request-shape
// difference from Generate; getting this wrong would silently fall back
// to a single buffered response that still "works", hiding the bug.
func TestGenerateStreamSendsStreamTrue(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		io.WriteString(w, `{"model":"m","response":"x","done":true}`+"\n")
	}))
	defer server.Close()

	if _, err := New(server.URL).GenerateStream(context.Background(), "m", "sys", "p", nil, nil); err != nil {
		t.Fatalf("GenerateStream error: %v", err)
	}
	if stream, _ := gotBody["stream"].(bool); !stream {
		t.Errorf("request body stream = %v, want true; body was %#v", gotBody["stream"], gotBody)
	}
}

// TestGenerateStreamRejectsTruncatedStream is a safety test, not a
// robustness nicety. A stream cut short still yields syntactically valid
// text, and that text becomes a shell command — "rm -rf /home/user/tmp"
// truncated to "rm -rf /home" is a completely different, far more
// destructive instruction. Partial output must therefore never be
// returned to the classifier or executor at all.
func TestGenerateStreamRejectsTruncatedStream(t *testing.T) {
	server := streamingServer(t, []string{
		`{"model":"m","response":"rm -rf /home/user/tmp","done":false}`,
		// connection ends here: no done:true ever arrives
	})
	defer server.Close()

	resp, err := New(server.URL).GenerateStream(context.Background(), "m", "", "p", nil, nil)
	if err == nil {
		t.Fatalf("expected an error on a truncated stream, got response %#v", resp)
	}
	if resp != nil {
		t.Errorf("expected no response on truncation, got %#v — partial text must never reach the caller", resp)
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error = %q, want it to name truncation explicitly", err)
	}
}

func TestGenerateStreamErrorsOnEmptyStream(t *testing.T) {
	server := streamingServer(t, nil)
	defer server.Close()

	if _, err := New(server.URL).GenerateStream(context.Background(), "m", "", "p", nil, nil); err == nil {
		t.Error("expected an error when the stream contains no chunks at all")
	}
}

func TestGenerateStreamSurfacesNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "model not found")
	}))
	defer server.Close()

	_, err := New(server.URL).GenerateStream(context.Background(), "m", "", "p", nil, nil)
	if err == nil {
		t.Fatal("expected an error on a non-200 response")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error = %q, want it to include the server's message", err)
	}
}

func TestGenerateStreamSurfacesMalformedChunk(t *testing.T) {
	server := streamingServer(t, []string{
		`{"model":"m","response":"ok","done":false}`,
		`{"model":"m","response":` + "\n" + `NOT JSON`,
	})
	defer server.Close()

	if _, err := New(server.URL).GenerateStream(context.Background(), "m", "", "p", nil, nil); err == nil {
		t.Error("expected an error when a chunk is not valid JSON")
	}
}

func TestGenerateStreamRespectsContextCancellation(t *testing.T) {
	// The handler is released by an explicit channel rather than by
	// watching r.Context(): a handler that blocks on the request context
	// can deadlock httptest.Server.Close(), because a server that has
	// never finished writing a response does not reliably observe the
	// client's disconnect. Found the hard way — the first version of this
	// test hung for the full 20s test timeout inside Close(), not inside
	// the code under test. Defer order matters and is deliberate: Close()
	// is registered first so it runs *last*, after the release below has
	// already let the handler return.
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		io.WriteString(w, `{"model":"m","response":"partial","done":false}`+"\n")
		flusher.Flush()
		<-release // hold the stream open, mid-response, until the test is done
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := New(server.URL).GenerateStream(ctx, "m", "", "p", nil, nil)
	if err == nil {
		t.Fatal("expected an error when the context is cancelled mid-stream")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
}

// TestGenerateStreamWorksWithNilOnToken confirms the callback is
// optional — callers that only want the assembled result should not have
// to supply a no-op.
func TestGenerateStreamWorksWithNilOnToken(t *testing.T) {
	server := streamingServer(t, []string{
		`{"model":"m","response":"a","done":false}`,
		`{"model":"m","response":"b","done":true}`,
	})
	defer server.Close()

	resp, err := New(server.URL).GenerateStream(context.Background(), "m", "", "p", nil, nil)
	if err != nil {
		t.Fatalf("GenerateStream error: %v", err)
	}
	if resp.Response != "ab" {
		t.Errorf("Response = %q, want %q", resp.Response, "ab")
	}
}
