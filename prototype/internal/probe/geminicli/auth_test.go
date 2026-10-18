package geminicli

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"synapseos/internal/probe"
)

// runScript runs one turn whose "CLI" is a shell script, outside any
// sandbox: only the runner's handling of exit codes and results is tested.
func runScript(t *testing.T, script string) ([]probe.Record, bool, error) {
	t.Helper()
	r := testRunner(t, Sandbox{}, Auth{APIKey: fakeKey})
	turn := Turn{Argv: []string{"/bin/sh", "-c", script}, Raw: filepath.Join(r.Out, "t0.jsonl")}
	return r.runTurn(context.Background(), turn, probe.Record{})
}

const initEvent = `echo '{"type":"init","session_id":"s","model":"m"}'; `

func TestFatalFailuresStopTheRun(t *testing.T) {
	for name, script := range map[string]string{
		// What the installed CLI does with no login (TestRealCLIStartsWithoutHostCredentials):
		// nothing on stdout, exit 41.
		"auth exit, empty stream": `echo 'Please set an Auth method' >&2; exit 41`,
		"auth result":             initEvent + `echo '{"type":"result","status":"error","error":{"type":"FatalAuthenticationError","message":"Manual authorization is required"}}'; exit 41`,
		"quota result": initEvent + `echo '{"type":"tool_use","tool_name":"run_shell_command","tool_id":"a","parameters":{"command":"ls"}}'
echo '{"type":"result","status":"error","error":{"type":"TerminalQuotaError","message":"You have exhausted your daily quota"}}'; exit 1`,
		"untrusted folder":           `echo 'not running in a trusted directory' >&2; exit 55`,
		"half a line then auth exit": `printf '{"type":"in'; exit 41`,
	} {
		t.Run(name, func(t *testing.T) {
			recs, ok, err := runScript(t, script)
			if !errors.Is(err, ErrFatal) || ok {
				t.Fatalf("ok=%v err=%v, want a fatal error", ok, err)
			}
			if strings.Contains(err.Error(), fakeKey) {
				t.Error("error message carries the key")
			}
			if name == "quota result" && len(recs) != 1 {
				t.Errorf("records before the failure must be kept: %+v", recs)
			}
		})
	}
}

func TestPerTaskFailuresDoNotStopTheRun(t *testing.T) {
	for name, script := range map[string]string{
		"turn limit": initEvent + `echo '{"type":"result","status":"error","error":{"type":"FatalTurnLimitedError","message":"Reached max session turns"}}'; exit 53`,
		"api error":  initEvent + `echo '{"type":"result","status":"error","error":{"type":"ApiError","message":"500 internal"}}'; exit 1`,
		"no result":  initEvent + `exit 1`,
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := runScript(t, script)
			if err != nil {
				t.Fatalf("err = %v, want none", err)
			}
			if wantOK := name != "no result"; ok != wantOK {
				t.Errorf("ok = %v, want %v", ok, wantOK)
			}
		})
	}
}
