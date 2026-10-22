// Package testenv decides what a test does when the host lacks something the
// test needs from it: bubblewrap, pasta and the isolated network, strace,
// prlimit. On a developer's machine that is a skip. Where the environment is
// known to have them (CI sets RequireEnv=1), it is a failure, so a broken
// sandbox cannot turn the sandbox tests into a green run that ran nothing.
//
// Every test that skips for a missing sandbox tool goes through Unavailable.
// It imports nothing from the repository, so any package's tests can use it.
package testenv

import (
	"fmt"
	"os"
	"testing"
)

// RequireEnv, set to 1, turns every Unavailable into a failure.
const RequireEnv = "SYNAPSE_REQUIRE_SANDBOX"

// Required reports whether the sandbox tools must be present.
func Required() bool { return os.Getenv(RequireEnv) == "1" }

// Unavailable ends the test because the host lacks what it needs: a skip,
// or with RequireEnv=1 a failure naming the variable.
func Unavailable(t testing.TB, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	if Required() {
		t.Fatalf("%s, and %s=1 says this host must have it", msg, RequireEnv)
	}
	t.Skip(msg)
}

// HermeticGit makes every git command the test process runs ignore the
// developer's global and system git config, and gives commits a fixed identity.
// A test that builds a throwaway repository otherwise depends on the machine:
// a personal commit hook refuses "git commit -m init", and a machine with no
// user.email cannot commit at all. Call it from an init in each test package
// that runs git; it changes the process environment, which children inherit.
func HermeticGit() {
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "test",
		"GIT_AUTHOR_EMAIL":    "test@example.invalid",
		"GIT_COMMITTER_NAME":  "test",
		"GIT_COMMITTER_EMAIL": "test@example.invalid",
	} {
		os.Setenv(k, v)
	}
}
