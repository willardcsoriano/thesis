package probe

import (
	"reflect"
	"strings"
	"testing"
)

// strace lines an agent's commands can make arbitrarily long: one execve
// line holds every argument (xargs packs up to ARG_MAX, about 2 MB, into
// one exec). A long line must be parsed, not abort the trace.
func TestParseExecveLongLines(t *testing.T) {
	args := strings.Repeat(`"f0123456789", `, 2<<20/15)
	log := `5 execve("/usr/bin/xargs", ["xargs", "rm"], 0x1 /* 1 var */) = 0
6 execve("/usr/bin/rm", ["rm", ` + args + `"last"], 0x1 /* 1 var */) = 0
7 execve("/usr/bin/rm", ["rm", ` + args + `"last"], 0x1 /* 1 var */ <unfinished ...>
8 execve("/usr/bin/ls", ["ls"], 0x1 /* 1 var */) = 0
7 <... execve resumed>) = 0
9 execve("/usr/bin/x", ["x", ` + args + `], 0x1 /* 1 var */) = -1 ENOENT (No such file or directory)
`
	got, err := parseExecve(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"xargs", "rm", "ls", "rm"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	raw, err := ParseExecve(strings.NewReader(log))
	if err != nil || len(raw) != 4 {
		t.Errorf("ParseExecve = %q, %v", raw, err)
	}
}

// Truncated and garbage lines (strace killed mid-write) are skipped; an
// execve split by another process is joined by pid and an unmatched resume
// is ignored.
func TestParseExecveHostileLines(t *testing.T) {
	log := "garbage\n\x00\x01\n" +
		`12 <... execve resumed>) = 0` + "\n" + // resume with no start
		`13 execve("/usr/bin/a", ["/usr/bin/a"], 0x1 /* 1 var */) = 0` + "\n" +
		`14 execve("/usr/bin/b", ["b\x1b[2Jc"], 0x1 /* 1 var */) = 0` + "\n" +
		`15 execve("/usr/bin/cut", ["cu`
	got, err := ParseExecve(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/usr/bin/a", "b\x1b[2Jc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The real tracer on a command whose one execve line is over a megabyte
// (strace shows at most 4096 arguments, each cut at -s 4096 bytes).
func TestTraceHugeArgv(t *testing.T) {
	tr := runTrace(t, `/bin/true $(seq -f "%0300g" 1 4000)`)
	if !reflect.DeepEqual(tr.Execve, []string{"seq", "true"}) {
		t.Errorf("execve = %q", tr.Execve)
	}
}
