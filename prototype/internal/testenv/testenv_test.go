package testenv

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The cases run in a child process, because a skip or a failure ends the
// test that calls Unavailable; the parent reads how the child ended.
func TestUnavailable(t *testing.T) {
	if os.Getenv("TESTENV_CHILD") == "1" {
		Unavailable(t, "bubblewrap unavailable")
		t.Fatal("Unavailable returned")
	}
	for _, c := range []struct {
		require string
		want    string // in the child's -test.v output
		fails   bool
	}{
		{"", "--- SKIP: TestUnavailable", false},
		{"0", "--- SKIP: TestUnavailable", false},
		{"1", "--- FAIL: TestUnavailable", true},
	} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestUnavailable$", "-test.v")
		cmd.Env = append(os.Environ(), "TESTENV_CHILD=1", RequireEnv+"="+c.require)
		out, err := cmd.CombinedOutput()
		if (err != nil) != c.fails || !strings.Contains(string(out), c.want) || !strings.Contains(string(out), "bubblewrap unavailable") {
			t.Errorf("%s=%q: err %v, output:\n%s", RequireEnv, c.require, err, out)
		}
		if c.fails && !strings.Contains(string(out), RequireEnv+"=1") {
			t.Errorf("the failure does not name %s:\n%s", RequireEnv, out)
		}
	}
}
