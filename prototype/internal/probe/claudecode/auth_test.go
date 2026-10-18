package claudecode

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"synapseos/internal/probe"
)

// The first live run's result event, reduced to the fields that matter: the
// CLI reports a failed login as a "success" subtype with is_error set.
const authFailedStream = `{"type":"system","subtype":"init","session_id":"s","model":"m","claude_code_version":"2.1.302","permissionMode":"default"}
{"type":"result","subtype":"success","is_error":true,"num_turns":1,"result":"Failed to authenticate. API Error: 401 OAuth access token is invalid.","permission_denials":[]}
`

func TestResultCarriesErrorTextAndDenials(t *testing.T) {
	s, err := ParseSession(strings.NewReader(authFailedStream), probe.Record{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Result == nil || !s.Result.IsError || !authFailure(s.Result.Text) {
		t.Fatalf("auth failure not recognised: %+v", s.Result)
	}
	denied := strings.Replace(authFailedStream, `"permission_denials":[]`, `"permission_denials":[{"tool_name":"WebFetch"}]`, 1)
	s, err = ParseSession(strings.NewReader(denied), probe.Record{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Result.Denials != 1 {
		t.Fatalf("denials = %d, want 1", s.Result.Denials)
	}
}

func TestAuthFailureIgnoresOrdinaryErrors(t *testing.T) {
	if authFailure("Error: reached max turns") {
		t.Fatal("a max-turns error is not an auth failure")
	}
}

func TestClaudeArgsDeclareAllowedTools(t *testing.T) {
	args, err := ClaudeArgs("list the files", "id", 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--allowedTools Bash,Read,Write,Edit") {
		t.Fatalf("allowedTools not declared: %s", joined)
	}
}

func TestAuthErrorIsFatal(t *testing.T) {
	err := fmt.Errorf("task X: %w", fmt.Errorf("%w: refused", ErrFatal))
	if !errors.Is(err, ErrFatal) {
		t.Fatal("a wrapped auth failure must stay recognisable as fatal")
	}
}
