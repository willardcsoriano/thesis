package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestLineSourceDeliversLinesInOrderThenEOF(t *testing.T) {
	src := newLineSource(strings.NewReader("one\ntwo\nthree"))
	ctx := context.Background()
	for _, want := range []struct {
		text string
		err  error
	}{{"one\n", nil}, {"two\n", nil}, {"three", io.EOF}, {"", io.EOF}, {"", io.EOF}} {
		got, err := src.next(ctx)
		if got != want.text || !errors.Is(err, want.err) && !(err == nil && want.err == nil) {
			t.Fatalf("next() = %q, %v; want %q, %v", got, err, want.text, want.err)
		}
	}
}

// The property the REPL depends on: a caller that stops waiting does not take
// the line with it. The line typed afterwards goes to the next caller.
func TestLineSourceCancelledWaitDoesNotLoseTheLine(t *testing.T) {
	r, w := io.Pipe()
	src := newLineSource(r)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := src.next(ctx); done <- err }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled next() returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("next() did not return when its context was cancelled")
	}

	go w.Write([]byte("typed after cancelling\n"))
	got, err := src.next(context.Background())
	if err != nil || got != "typed after cancelling\n" {
		t.Fatalf("next caller got %q, %v; want the line typed after cancelling", got, err)
	}
	w.Close()
}

func TestConfirmLineDeclinesWhenCancelledWhileWaiting(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	src := newLineSource(r)
	ctx, cancel := context.WithCancel(context.Background())
	var out strings.Builder
	got := make(chan bool, 1)
	go func() { got <- confirmLine(ctx, src, &out, "delete it?") }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case approved := <-got:
		if approved {
			t.Fatal("a cancelled confirmation approved")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("confirmLine kept waiting after its context was cancelled")
	}
}

func TestConfirmLineIgnoresAnAnswerAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := newLineSource(strings.NewReader("y\n"))
	time.Sleep(20 * time.Millisecond) // let the reader have the "y" ready
	if confirmLine(ctx, src, io.Discard, "delete it?") {
		t.Fatal("an already-cancelled confirmation approved")
	}
}

// zeros is an endless stream with no newline, like /dev/zero.
type zeros struct{ read int64 }

func (z *zeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	z.read += int64(len(p))
	return len(p), nil
}

// Input with no line end in sight is treated as ended after a bounded read,
// so a confirmation declines instead of growing without bound (atk4-13).
func TestLineSourceGivesUpOnEndlessInput(t *testing.T) {
	z := &zeros{}
	src := newLineSource(z)
	if _, err := src.next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("next on endless input = %v, want io.EOF", err)
	}
	if z.read > maxDrain+maxLine+64<<10 {
		t.Fatalf("read %d bytes before giving up", z.read)
	}
	if confirmLine(context.Background(), src, io.Discard, "ok?") {
		t.Fatal("endless input approved a confirmation")
	}
}

func TestLineSourceSkipsAnOverlongLineAndContinues(t *testing.T) {
	in := strings.Repeat("a", maxLine+10) + "\nnext line\n"
	src := newLineSource(strings.NewReader(in))
	if _, err := src.next(context.Background()); !errors.Is(err, errLineTooLong) {
		t.Fatalf("first next = %v, want errLineTooLong", err)
	}
	if got, err := src.next(context.Background()); got != "next line\n" || err != nil {
		t.Fatalf("second next = %q %v", got, err)
	}
}

// Nothing is read from stdin until something asks.
func TestLineSourceReadsOnlyWhenAsked(t *testing.T) {
	z := &zeros{}
	newLineSource(z)
	time.Sleep(50 * time.Millisecond)
	if z.read != 0 {
		t.Fatalf("read %d bytes with nobody asking", z.read)
	}
}
