package claudecode

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
)

var (
	execveLine    = regexp.MustCompile(`^(\d+)\s+execve\("(?:[^"\\]|\\.)*", \["((?:[^"\\]|\\.)*)"`)
	execveDone    = regexp.MustCompile(`\) = 0$`)
	execveUnfin   = regexp.MustCompile(`<unfinished \.\.\.>$`)
	execveResumed = regexp.MustCompile(`^(\d+)\s+<\.\.\. execve resumed>.*\) = 0$`)
)

// Execve reads `strace -f -e trace=execve -o FILE` output and returns argv[0]
// of every successful execve, in order. It covers the whole process tree,
// the CLI's own helpers (its shell, ripgrep, git) included, so it is a
// turn-level list, not a per-command one. A call split by another process's
// line ("<unfinished ...>" then "<... execve resumed>") is joined by pid.
func Execve(r io.Reader) ([]string, error) {
	var out []string
	pending := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := execveResumed.FindStringSubmatch(line); m != nil {
			if a, ok := pending[m[1]]; ok {
				out = append(out, a)
				delete(pending, m[1])
			}
			continue
		}
		m := execveLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		arg0 := unescape(m[2])
		switch {
		case execveDone.MatchString(line):
			out = append(out, arg0)
		case execveUnfin.MatchString(line):
			pending[m[1]] = arg0
		}
	}
	return out, sc.Err()
}

func unescape(s string) string {
	if u, err := strconv.Unquote(`"` + s + `"`); err == nil {
		return u
	}
	return s
}
