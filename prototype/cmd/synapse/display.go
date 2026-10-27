package main

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"synapseos/internal/termsafe"
)

// displaySafe is termsafe.Display; see there.
func displaySafe(s string) string { return termsafe.Display(s) }

// outputGuard wraps stdout so that losing it, as `synapse run … | head -1`
// does once head has its line, stops the task instead of the process. main
// catches SIGPIPE (catchBrokenPipe), so a write to a closed pipe returns
// EPIPE rather than killing synapse between running a command and
// journaling its undo; the guard turns that EPIPE into a cancelled task,
// which the loop ends at its next step boundary, after the journal write.
//
// Any other write failure (a full disk, /dev/full) stops the task too, and
// failure() reports it so the run exits 1 instead of 0 with its results lost
// (finding atk4-10).
type outputGuard struct {
	w      io.Writer
	once   sync.Once
	broken func()
	mu     sync.Mutex
	err    error // the first write error
}

func (g *outputGuard) Write(p []byte) (int, error) {
	n, err := g.w.Write(p)
	if err != nil {
		g.mu.Lock()
		if g.err == nil {
			g.err = err
		}
		g.mu.Unlock()
		g.once.Do(g.broken)
	}
	return n, err
}

// failure returns the first write error other than a closed pipe, which is a
// reader that stopped reading, not a failure.
func (g *outputGuard) failure() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.err == nil || errors.Is(g.err, syscall.EPIPE) {
		return nil
	}
	return g.err
}

// catchBrokenPipe makes writes to a closed stdout or stderr fail with EPIPE
// instead of killing the process (os/signal: a program that has called
// Notify for SIGPIPE gets the error). It catches rather than ignores the
// signal on purpose: an ignored signal is inherited by the commands synapse
// runs, which would change how their own pipelines end, while a caught one
// is reset to the default in every child.
func catchBrokenPipe() {
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
}
