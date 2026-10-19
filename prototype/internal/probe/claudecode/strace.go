package claudecode

import (
	"io"

	"synapseos/internal/probe"
)

// Execve reads `strace -f -e trace=execve -o FILE` output and returns argv[0]
// of every successful execve, in order. It covers the whole process tree,
// the CLI's own helpers (its shell, ripgrep, git) included, so it is a
// turn-level list, not a per-command one. It is probe.ParseExecve: a call
// split by another process's line is joined by pid, and a line of any
// length is read.
func Execve(r io.Reader) ([]string, error) { return probe.ParseExecve(r) }
