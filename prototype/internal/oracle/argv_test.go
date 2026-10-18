package oracle

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeBwrap is an executable that records its arguments, NUL-separated, in the
// file named by $ORACLE_ARGV_OUT and exits 0, so a test can read the exact
// command line ExecuteIn hands to bubblewrap without bubblewrap installed.
func fakeBwrap(t *testing.T) (bin, out string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "bwrap")
	out = filepath.Join(dir, "argv")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\0' \"$@\" > \"$ORACLE_ARGV_OUT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORACLE_ARGV_OUT", out)
	return bin, out
}

func recordedArgv(t *testing.T, sb Sandbox, cmd string) (root string, argv []string) {
	t.Helper()
	bin, out := fakeBwrap(t)
	root = t.TempDir()
	if _, err := ExecuteIn(context.Background(), bin, root, cmd, 5*time.Second, sb); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return root, strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}

// goldenScript is the shell script the corpus pipelines ran when the paper's
// numbers were produced. It is spelled out rather than built from SudoShim so
// that a change to either is caught here.
const goldenScript = `ulimit -v 4000000 -f 200000 2>/dev/null; sudo() { while [ $# -gt 0 ]; do case "$1" in -u|-g|-h|-p|-C|-D|-r|-t|-T|-U) shift 2;; -*) shift;; *) break;; esac; done; "$@"; }; doas() { sudo "$@"; }; rm -f a.txt`

// TestEmptySandboxArgvUnchanged pins the bubblewrap command line of Execute
// (the zero Sandbox) to the one recorded before HideHost existed. The
// recoverability corpus, listpilot, corpusgen and composecheck all run
// through it, so any difference would make their results irreproducible.
func TestEmptySandboxArgvUnchanged(t *testing.T) {
	root, got := recordedArgv(t, Sandbox{}, "rm -f a.txt")
	want := []string{
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--bind", root, root,
		"--unshare-all", "--die-with-parent",
		"--chdir", root, "--setenv", "HOME", root + "/home", "--setenv", "PWD", root, "--",
		"bash", "-c", goldenScript,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv changed:\n got %q\nwant %q", got, want)
	}
}

// TestWrapWritableArgvUnchanged pins Wrap and Writable without HideHost.
func TestWrapWritableArgvUnchanged(t *testing.T) {
	w := t.TempDir()
	root, got := recordedArgv(t, Sandbox{Wrap: []string{"/usr/bin/env", "-u", "X"}, Writable: []string{w}}, "rm -f a.txt")
	want := []string{
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--bind", root, root,
		"--bind", w, w,
		"--unshare-all", "--die-with-parent",
		"--chdir", root, "--setenv", "HOME", root + "/home", "--setenv", "PWD", root, "--",
		"/usr/bin/env", "-u", "X",
		"bash", "-c", goldenScript,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv changed:\n got %q\nwant %q", got, want)
	}
}
