// Command listpilot runs five systems over pilot/corpus.jsonl and emits one JSON
// object per command per system: L0 (the current pattern-list classifier), L1 (a
// list flipped to fail closed), ALG-strict and ALG-capture (two readings of the
// full effect analysis), and ALG-nocompose (the same analysis with wrapper,
// find -exec, xargs, eval, shell -c, and loop composition disabled, falling back
// to the per-command rule table alone). ALG-nocompose exists to answer
// docs/open-problems.md row 38 — the adviser's own question of how much of the
// algorithm's result comes from composition versus the table it extends. It
// reports what each system would do and never executes a corpus command. The
// only commands ever run are ALG's read-only resolvers, which it proves
// read-only first and runs under a read-only sandbox where one is available.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"synapseos/internal/classifier"
	"synapseos/internal/effects"
	"synapseos/internal/oracle"
)

type row struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	// Round 2b fields. Absent in corpus.jsonl.
	CmdFx string `json:"cmd_fx"`
	Setup string `json:"setup"`
	Home  bool   `json:"home"`
	// Round 4 field: the complete fixture, written by the oracle-based generator.
	Fx []oracle.Entry `json:"fx"`
}

type result struct {
	ID        string `json:"id"`
	System    string `json:"system"`
	Prompted  bool   `json:"prompted"`
	Protected bool   `json:"protected"`
	Reason    string `json:"reason"`
	Class     string `json:"class,omitempty"`
}

// fixture builds the directory the hand-built commands are written against.
func fixture() (string, error) {
	wd, err := os.MkdirTemp("", "listpilot-")
	if err != nil {
		return "", err
	}
	files := map[string]string{
		"a.txt": "a\n", "b.txt": "b\n", "notes.txt": "notes\n", "app.conf": "k=v\n",
		"report.docx": "doc\n", "data.csv": "x,1\ny,2\n", "big.log": "log\n",
		"src/main.go": "package main\n", "logs/x.log": "x\n", "logs/y.log": "y\n",
		"build/out.o": "o\n", "backup/old.txt": "old\n",
	}
	for p, body := range files {
		full := filepath.Join(wd, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	return wd, os.MkdirAll(filepath.Join(wd, "tmp"), 0o755)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: listpilot corpus.jsonl")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var r row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var wd string
		if r.Fx != nil {
			wd, err = os.MkdirTemp("", "listpilot-")
			if err == nil {
				if err = oracle.Materialize(wd, r.Fx); err == nil {
					err = os.MkdirAll(filepath.Join(wd, "home"), 0o755)
				}
			}
			r.Home = true // the oracle ran the command with HOME inside the fixture
		} else {
			wd, err = fixture()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if r.Setup != "" {
			cmd := exec.Command("bash", "-c", r.Setup)
			cmd.Dir = wd
			if out, err := cmd.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "setup for %s failed: %v\n%s\n", r.ID, err, out)
				os.Exit(1)
			}
		}
		if r.CmdFx != "" {
			r.Command = r.CmdFx
		}
		v, why := classifier.ClassifyForDir(r.Command, wd)
		enc.Encode(result{ID: r.ID, System: "L0", Prompted: v == classifier.Irreversible,
			Protected: classifier.Protected(r.Command, wd), Reason: why})

		asks, reasons := effects.FailClosed(r.Command)
		enc.Encode(result{ID: r.ID, System: "L1", Prompted: asks, Reason: first(reasons)})

		an := effects.New(wd)
		if r.Home {
			home := wd
			if r.Fx != nil {
				home = filepath.Join(wd, "home")
			}
			an.Env = append(withoutHome(os.Environ()), "HOME="+home)
		}
		an.Run = effects.DefaultRunner(8 * time.Second)
		res := an.Analyze(context.Background(), r.Command)
		vd := res.Verdict()
		protected := vd.Class == effects.RecoverableWithCapture
		enc.Encode(result{ID: r.ID, System: "ALG-strict", Prompted: vd.Class != effects.Recoverable,
			Protected: protected, Reason: first(vd.Reasons), Class: vd.Class.String()})
		enc.Encode(result{ID: r.ID, System: "ALG-capture", Prompted: vd.Class == effects.Unrecoverable,
			Protected: protected, Reason: first(vd.Reasons), Class: vd.Class.String()})

		nc := *an
		nc.Compose = false
		ncRes := nc.Analyze(context.Background(), r.Command)
		ncVd := ncRes.Verdict()
		ncProtected := ncVd.Class == effects.RecoverableWithCapture
		enc.Encode(result{ID: r.ID, System: "ALG-nocompose", Prompted: ncVd.Class == effects.Unrecoverable,
			Protected: ncProtected, Reason: first(ncVd.Reasons), Class: ncVd.Class.String()})
		os.RemoveAll(wd)
	}
}

func withoutHome(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "HOME=") {
			out = append(out, kv)
		}
	}
	return out
}

func first(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}
