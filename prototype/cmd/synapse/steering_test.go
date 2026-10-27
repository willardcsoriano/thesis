package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"synapseos/internal/ollama"
)

func TestFolderListing(t *testing.T) {
	d := t.TempDir()
	if got := folderListing(d); got != "(empty)" {
		t.Errorf("empty folder = %q, want (empty)", got)
	}
	os.WriteFile(filepath.Join(d, "b.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(d, "a.pdf"), nil, 0o644)
	os.WriteFile(filepath.Join(d, ".secret"), nil, 0o644)
	os.Mkdir(filepath.Join(d, "old"), 0o755)
	if got := folderListing(d); got != "a.pdf, b.txt, old/" {
		t.Errorf("listing = %q, want sorted names, folders marked, hidden left out", got)
	}
	if got := folderListing(filepath.Join(d, "gone")); got != "" {
		t.Errorf("unreadable folder = %q, want nothing", got)
	}
	big := t.TempDir()
	for i := 0; i < maxListed+5; i++ {
		os.WriteFile(filepath.Join(big, fmt.Sprintf("f%03d", i)), nil, 0o644)
	}
	got := folderListing(big)
	if n := strings.Count(got, ", ") + 1; !strings.HasSuffix(got, "and 5 more") || n != maxListed+1 {
		t.Errorf("a large folder gives %d parts ending %q, want %d names and the rest counted", n, got[len(got)-20:], maxListed)
	}
}

// The step prompt shows the folder as it is when the step is asked for, so a
// file the last step made is in it.
func TestStepPromptListsTheFolderAsItIsNow(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "notes.txt"), nil, 0o644)
	if p := buildStepPrompt("t", nil, "", d); !strings.Contains(p, "In it: notes.txt\n") {
		t.Errorf("prompt does not list the folder:\n%s", p)
	}
	os.Mkdir(filepath.Join(d, "made"), 0o755)
	if p := buildStepPrompt("t", nil, "", d); !strings.Contains(p, "In it: made/, notes.txt\n") {
		t.Errorf("prompt does not show the change:\n%s", p)
	}
	if p := buildStepPrompt("t", nil, "", ""); strings.Contains(p, "In it:") {
		t.Errorf("a prompt with no working directory lists one:\n%s", p)
	}
}

// loopingServer aborts the first `aborts` step requests the way Ollama does when
// a model repeats itself, then answers; it records each step request's options.
func loopingServer(t *testing.T, aborts int, steps ...string) (*httptest.Server, func() []map[string]any) {
	var mu sync.Mutex
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("You are reporting the outcome of a task")) {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "ok", Done: true})
			return
		}
		var req struct{ Options map[string]any }
		json.Unmarshal(body, &req)
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, req.Options)
		if len(seen) <= aborts {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":"prediction aborted, token repeat limit reached"}`)
			return
		}
		if len(steps) == 0 {
			t.Error("more steps asked for than scripted")
			return
		}
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: steps[0], Done: true, EvalCount: 1})
		steps = steps[1:]
	}))
	return srv, func() []map[string]any { mu.Lock(); defer mu.Unlock(); return seen }
}

// One looped reply is retried once, with a repeat penalty the first request did
// not carry, and the task goes on.
func TestRunLoopRetriesALoopedReplyOnce(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, seen := loopingServer(t, 1, "echo hi", "DONE")
	defer srv.Close()
	var out, errOut bytes.Buffer
	if code := runLoop(context.Background(), ollama.New(srv.URL), "m", "say hi", neverConfirm(t), &out, &errOut, ""); code != exitOK {
		t.Fatalf("exit %d, want 0; stderr:\n%s", code, errOut.String())
	}
	reqs := seen()
	if len(reqs) < 2 || reqs[0]["repeat_penalty"] != nil || reqs[1]["repeat_penalty"] != 1.2 {
		t.Errorf("step requests' options = %v; want the retry, and only it, with repeat_penalty 1.2", reqs)
	}
	if len(reqs) == 3 && reqs[2]["repeat_penalty"] != nil {
		t.Error("the penalty stayed on for the next step")
	}
}

// A second loop is not retried again: the step fails as before, and says why.
func TestRunLoopDoesNotRetryALoopedReplyTwice(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, seen := loopingServer(t, 2)
	defer srv.Close()
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(srv.URL), "m", "say hi", neverConfirm(t), &out, &errOut, "")
	if code != exitModelUnavailable || len(seen()) != 2 {
		t.Errorf("exit %d after %d requests, want %d after 2", code, len(seen()), exitModelUnavailable)
	}
	if !strings.Contains(errOut.String(), "token repeat limit") {
		t.Errorf("stderr does not carry the cause:\n%s", errOut.String())
	}
}
