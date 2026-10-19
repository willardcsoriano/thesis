package geminicli

import (
	"strings"
	"sync"
	"testing"

	"synapseos/internal/probe/claudecode"
	"synapseos/internal/testenv"
)

// The isolated network is claudecode's (claudecode.Network), whose attacks
// are tested there in full; this package's sandboxes run on it by default.

var (
	isoOnce sync.Once
	isoNet  claudecode.Network
	isoErr  error
)

// defaultNet is what agentprobe uses by default, the isolated network. A
// host where it cannot be built skips the test (fails it under
// testenv.RequireEnv) rather than quietly running it on the host's network.
func defaultNet(t *testing.T) claudecode.Network {
	t.Helper()
	isoOnce.Do(func() {
		isoNet, isoErr = claudecode.ResolveNetwork(claudecode.NetIsolated)
		if isoErr == nil {
			isoErr = isoNet.Probe()
		}
	})
	if isoErr != nil {
		testenv.Unavailable(t, "isolated network unavailable: %v", isoErr)
	}
	return isoNet
}

// The Gemini driver's sandbox goes through the same isolated chain, with
// the same lifeline, and its RunTask refuses a network never resolved.
func TestGeminiSandboxUsesTheIsolatedNetwork(t *testing.T) {
	n := claudecode.Network{Mode: claudecode.NetIsolated, Pasta: "/usr/bin/pasta", Flock: "/usr/bin/flock", IP: "/usr/bin/ip"}
	sb := testSandbox(t, "")
	sb.Net = n
	w := claudecode.Workdir{Root: "/w"}
	r := testRunner(t, sb, Auth{APIKey: fakeKey})
	turns, err := r.Plan(oneTurnTask, "technical", w, "11111111-2222-4333-8444-555555555555")
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(turns[0].Argv, " ")
	if !strings.HasPrefix(argv, "/usr/bin/pasta --config-net ") || !strings.Contains(argv, " sh /w/lifeline /usr/bin/bwrap ") || !strings.Contains(argv, "--cap-drop ALL") {
		t.Errorf("argv = %s", argv)
	}
	if turns[0].Lifeline != "/w/lifeline" {
		t.Errorf("lifeline = %q", turns[0].Lifeline)
	}
	r.Sandbox.Net = claudecode.Network{}
	if _, err := r.RunTask(t.Context(), oneTurnTask, "technical"); err == nil || !strings.Contains(err.Error(), "pasta") {
		t.Errorf("RunTask on an unresolved network: %v", err)
	}
}
