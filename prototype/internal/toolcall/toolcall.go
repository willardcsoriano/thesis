// Package toolcall extracts a tool invocation from a model's chat reply.
//
// It exists because Ollama does not populate the `tool_calls` field for
// qwen2.5-coder: measured 0 times in 69 attempts at 3B and 23 at 7B
// (prior-art.md, 2026-09-15). The model is not the problem — a direct request
// to /api/chat shows it emitting a well-formed tool call as JSON in the
// *content* field, and `ollama show` confirms the model declares a `tools`
// capability whose template renders the offered tools. Only the structured
// parsing is missing, so this package supplies it.
//
// This is the layer a frontier tool-calling API gives you for free: strict
// output shapes learned in post-training, validated server-side before the
// caller sees them. A 3B model supplies neither guarantee, and the shapes it
// actually produces are recorded in toolcall_test.go rather than imagined —
// every malformed case tested there was captured from a live reply.
//
// Three behaviours matter, and they are deliberately distinguished rather
// than collapsed into "did it work":
//
//   - A valid call against a tool that was offered. Dispatchable.
//   - Prose. The model chose to talk rather than act; this is the
//     conversational half of the fork and is not an error.
//   - An attempted call that cannot be honoured — an invented tool name, or
//     arguments whose shape carries no usable value. The model meant to act
//     and failed, which is neither prose nor dispatchable, and the caller
//     must not show the raw JSON to a user or silently treat it as chat.
package toolcall

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Call is a tool invocation the model asked for, after normalisation.
type Call struct {
	Name string
	Args map[string]any
}

// ErrNoCall reports that the reply contained no tool call — it is prose, and
// the conversational branch of the fork. Callers test for it with errors.Is
// and should render the content as the assistant's reply.
var ErrNoCall = errors.New("no tool call in reply")

// UnknownToolError reports a call naming a tool that was never offered. The
// model invented it: "hello" has produced `print_message`, a function no
// caller ever declared. Dispatching on name alone would turn a hallucinated
// name into an action, so the name is checked against what was offered.
type UnknownToolError struct{ Name string }

func (e *UnknownToolError) Error() string {
	return fmt.Sprintf("model invoked unoffered tool %q", e.Name)
}

// rawCall is the shape the model emits in the content field. It mirrors the
// tool-calling schema closely enough that the same struct parses both.
type rawCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	// Some replies nest the call under "function", matching the wire shape
	// of a native tool_calls entry rather than the bare form.
	Function *struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

// Parse extracts a tool call from an assistant reply. known reports whether a
// tool name was actually offered; it must not be nil.
//
// Returns ErrNoCall when the reply is prose, an *UnknownToolError when the
// model invented a tool name, and a plain error when a call was clearly
// intended but no usable arguments could be recovered from it.
func Parse(content string, known func(string) bool) (*Call, error) {
	blob := extractJSON(content)
	if blob == "" {
		return nil, ErrNoCall
	}
	var rc rawCall
	if err := json.Unmarshal([]byte(blob), &rc); err != nil {
		// JSON-ish but not JSON. Treated as prose rather than as a failed
		// call: the alternative is showing a user a parse error for what may
		// simply have been a sentence containing a brace.
		return nil, ErrNoCall
	}
	name, args := rc.Name, rc.Arguments
	if rc.Function != nil && rc.Function.Name != "" {
		name, args = rc.Function.Name, rc.Function.Arguments
	}
	if name == "" {
		return nil, ErrNoCall
	}
	if !known(name) {
		return nil, &UnknownToolError{Name: name}
	}
	norm, err := normaliseArgs(args)
	if err != nil {
		return nil, fmt.Errorf("tool %s: %w", name, err)
	}
	return &Call{Name: name, Args: norm}, nil
}

// normaliseArgs repairs the argument shapes this model actually produces.
//
// The recurring failure is that it echoes the JSON Schema it was shown into
// the position where the value belongs, sometimes with the value alongside:
//
//	{"command": {"type": "string", "value": "wc -l log.txt"}}
//	{"command": {"type": "string", "description": "The bash command to run."}}
//
// The first carries a usable value under a nested key; the second carries
// none at all and is unrecoverable. Telling them apart is the whole job.
func normaliseArgs(args map[string]any) (map[string]any, error) {
	if len(args) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		nested, ok := v.(map[string]any)
		if !ok {
			out[k] = v
			continue
		}
		// A nested object in an argument slot is a schema echo. Look for the
		// value the model buried inside it.
		if val, found := firstValue(nested); found {
			out[k] = val
			continue
		}
		if isSchemaOnly(nested) {
			return nil, fmt.Errorf("argument %q carries only its schema, no value", k)
		}
		out[k] = v
	}
	return out, nil
}

// valueKeys are where this model has been observed to bury the real argument
// when it echoes a schema around it. Ordered by how often each is seen.
var valueKeys = []string{"value", "default", "content", "text"}

func firstValue(m map[string]any) (any, bool) {
	for _, k := range valueKeys {
		if v, ok := m[k]; ok {
			if s, isStr := v.(string); !isStr || strings.TrimSpace(s) != "" {
				return v, true
			}
		}
	}
	return nil, false
}

// isSchemaOnly reports whether a nested object is purely JSON-Schema
// metadata, which means the value never arrived.
func isSchemaOnly(m map[string]any) bool {
	schemaKeys := map[string]bool{"type": true, "description": true, "items": true,
		"enum": true, "properties": true, "required": true, "format": true}
	for k := range m {
		if !schemaKeys[k] {
			return false
		}
	}
	return len(m) > 0
}

// extractJSON returns the first balanced JSON object in s, or "" if there is
// none. The model wraps its call in markdown fences, prefixes it with a
// sentence, or emits it bare, so the object is located by brace balance
// rather than by trimming known decorations — string literals are tracked so
// a brace inside a shell command cannot end the scan early.
func extractJSON(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// no structural meaning inside a string
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
