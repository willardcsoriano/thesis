package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"synapseos/internal/oracle"
	"synapseos/internal/probe/claudecode"
)

// The dry run prints the whole isolated chain by default: pasta with no
// forwards, the namespace's rules, the lifeline, then bwrap with the
// forwarder as resolver. -net host prints bwrap first and says what the
// agent can then reach. Both drivers.
func TestDryRunPrintsTheNetworkChain(t *testing.T) {
	if oracle.Bwrap() == "" {
		t.Skip("bubblewrap unavailable")
	}
	if _, err := exec.LookPath("pasta"); err != nil {
		t.Skip("pasta not installed")
	}
	creds := filepath.Join(t.TempDir(), "oauth_creds.json")
	if err := os.WriteFile(creds, []byte(`{"refresh_token":"1//fake-refresh"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct {
		driver string
		args   []string
		skip   func() error
	}{
		{"claude", nil, func() error { _, err := claudecode.ResolveInstall(); return err }},
		{"gemini", []string{"-oauth-creds", creds}, func() error { _, err := exec.LookPath("gemini"); return err }},
	} {
		t.Run(d.driver, func(t *testing.T) {
			if err := d.skip(); err != nil {
				t.Skip(err)
			}
			dry := func(extra ...string) (string, int, string) {
				var out, errb bytes.Buffer
				args := append(append([]string{d.driver, "-tasks", suite, "-tiers", "technical", "-limit", "1", "-dry-run", "-out", t.TempDir()}, d.args...), extra...)
				code := run(context.Background(), args, env(map[string]string{claudecode.TokenEnv: "x"}), &out, &errb)
				return out.String(), code, errb.String()
			}
			s, code, e := dry()
			if code != 0 {
				t.Fatalf("exit %d: %s", code, e)
			}
			for _, want := range []string{"# note: -net isolated:", "<workdir>/lifeline",
				"pasta --config-net --quiet -t none -u none -T none -U none --map-host-loopback none --map-guest-addr none --dns-forward " + claudecode.DNSForward,
				"to 169.254.0.0/16 prohibit", "--ro-bind '<workdir>/etc/resolv.conf' ", "--cap-drop ALL"} {
				if !strings.Contains(s, want) {
					t.Errorf("isolated dry run lacks %q:\n%s", want, s)
				}
			}
			s, code, e = dry("-net", "host")
			if code != 0 {
				t.Fatalf("exit %d: %s", code, e)
			}
			if strings.Contains(s, "pasta") || !strings.Contains(s, "# note: -net host:") || !strings.Contains(s, "\n"+oracle.Bwrap()+" ") {
				t.Errorf("host dry run:\n%s", s)
			}
			if _, code, e = dry("-net", "bridge"); code != 2 || !strings.Contains(e, "isolated or host") {
				t.Errorf("unknown -net: exit %d: %s", code, e)
			}
		})
	}
}

// Without pasta on PATH the default refuses to start, names the install
// line and -net host, and prints no command line: it never falls back to
// the host's network. A real run and a dry run alike.
func TestRefusesIsolatedWithoutPasta(t *testing.T) {
	bin := t.TempDir()
	for _, tool := range []string{"bwrap", "prlimit", "flock", "ip", "true"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not installed", tool)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	if oracle.Bwrap() == "" {
		t.Skip("bubblewrap cannot create namespaces here")
	}
	for _, dryRun := range []bool{false, true} {
		args := []string{"claude", "-tasks", suite, "-out", t.TempDir()}
		if dryRun {
			args = append(args, "-dry-run")
		}
		var out, errb bytes.Buffer
		code := run(context.Background(), args, env(map[string]string{claudecode.TokenEnv: "x"}), &out, &errb)
		if code != 1 || out.Len() != 0 || !strings.Contains(errb.String(), "sudo apt install -y passt") || !strings.Contains(errb.String(), "-net host") {
			t.Errorf("dry run %v: exit %d, stdout %q, stderr %q", dryRun, code, out.String(), errb.String())
		}
	}
}
