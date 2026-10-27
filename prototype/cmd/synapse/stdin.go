package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// lineSource is the single owner of an input stream. One goroutine reads lines
// in order and hands each to whichever caller asks next, so a caller can stop
// waiting (Ctrl+C at a confirmation prompt) without abandoning a read that a
// second reader would then race for. The line that read eventually returns is
// delivered to the next caller, typically the session's own prompt, which is
// where a person typing after cancelling expects it to go.
//
// This replaces two bufio.Readers or a blocking read on the main goroutine,
// the trade-off runTaskInterruptibly used to document as a known limitation.
type lineSource struct {
	req   chan struct{}   // asks the reader for one more line
	lines chan lineResult // the reader's answers, in order
	mu    sync.Mutex
	asked bool // a request is outstanding; its answer goes to the next caller
	done  bool // the stream ended; every later call returns io.EOF
}

type lineResult struct {
	text string
	err  error
}

// maxLine bounds one line of input, and maxDrain how much more of an
// over-long line is skipped looking for its end. Input past both (stdin from
// /dev/zero, a binary file) is treated as ended. Before, the reader started at
// once and held a line of any length, so `synapse run … < /dev/zero` grew
// without bound before the model was even asked (finding atk4-13).
const (
	maxLine  = 64 << 10
	maxDrain = 1 << 20
)

// errLineTooLong is a line over maxLine; it was skipped, not delivered.
var errLineTooLong = errors.New("input line too long; it was ignored")

func newLineSource(r io.Reader) *lineSource {
	s := &lineSource{req: make(chan struct{}), lines: make(chan lineResult)}
	go func() {
		br := bufio.NewReaderSize(r, 4096)
		for range s.req {
			res := readBoundedLine(br)
			s.lines <- res
			if res.err != nil && res.err != errLineTooLong {
				return
			}
		}
	}()
	return s
}

// readBoundedLine reads one line of at most maxLine bytes. An over-long line
// is skipped up to its newline, within maxDrain bytes; past that the input is
// treated as ended.
func readBoundedLine(br *bufio.Reader) lineResult {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(line)+len(chunk) > maxLine {
			skipped := len(line) + len(chunk)
			for err == bufio.ErrBufferFull && skipped <= maxDrain {
				chunk, err = br.ReadSlice('\n')
				skipped += len(chunk)
			}
			if err != nil {
				return lineResult{err: io.EOF} // no line end in sight: unusable input
			}
			return lineResult{err: errLineTooLong}
		}
		line = append(line, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return lineResult{text: string(line), err: err}
	}
}

// next returns the next line, with its trailing newline, and the read error
// that came with it (io.EOF on a final line with no newline). It returns
// ctx.Err() if ctx ends first, and io.EOF for every call after the stream
// ended. Nothing is read until a caller asks.
func (s *lineSource) next(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return "", io.EOF
	}
	if !s.asked {
		s.asked = true
		s.mu.Unlock()
		select {
		case s.req <- struct{}{}:
		case <-ctx.Done():
			s.mu.Lock()
			s.asked = false
			s.mu.Unlock()
			return "", ctx.Err()
		}
	} else {
		s.mu.Unlock()
	}
	select {
	case r := <-s.lines:
		s.mu.Lock()
		s.asked = false
		if r.err != nil && r.err != errLineTooLong {
			s.done = true
			close(s.req)
		}
		s.mu.Unlock()
		return r.text, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// approves is the one rule for a confirmation answer: only y or yes, in any
// case and with surrounding space, approves. Everything else declines.
func approves(line string) bool {
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// confirmLine asks prompt and reads the answer from src. It declines when ctx
// ends before an answer arrives, and on a closed stream, so a cancelled task
// or unattended run never approves anything. A final answer with no trailing
// newline still counts: "y" then end of input is an answer, not silence.
func confirmLine(ctx context.Context, src *lineSource, out io.Writer, prompt string) bool {
	fmt.Fprintf(out, "%s [y/N] ", prompt)
	line, err := src.next(ctx)
	if ctx.Err() != nil {
		fmt.Fprintln(out) // leave the cursor on a fresh line after ^C
		return false
	}
	if err != nil && line == "" {
		return false
	}
	return approves(line)
}
