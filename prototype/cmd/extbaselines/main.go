// Command extbaselines scores the corpus with external, open-source command-safety
// checks, as additional baselines beside listpilot's. It writes one JSON object per
// command in listpilot's format (system, prompted, protected, reason) and never
// executes a corpus command.
//
// It scores Codex CLI's dangerous-command check itself (internal/baselines/codex).
// With -fixtures it also lays out each command's starting files under DIR/<id>, so a
// checker that runs elsewhere (cc-safety-net, a Node library) can be asked about the
// same command from the same working directory; see pilot/ccsafetynet.mjs.
//
//	extbaselines -fixtures DIR pilot/corpus7.jsonl > pilot/external_baselines7.jsonl
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"synapseos/internal/baselines/codex"
	"synapseos/internal/oracle"
)

type row struct {
	ID      string         `json:"id"`
	Command string         `json:"command"`
	CmdFx   string         `json:"cmd_fx"`
	Fx      []oracle.Entry `json:"fx"`
}

type result struct {
	ID        string `json:"id"`
	System    string `json:"system"`
	Prompted  bool   `json:"prompted"`
	Protected bool   `json:"protected"`
	Reason    string `json:"reason"`
}

// handBuilt is listpilot's fixture for items that carry none of their own.
var handBuilt = map[string]string{
	"a.txt": "a\n", "b.txt": "b\n", "notes.txt": "notes\n", "app.conf": "k=v\n",
	"report.docx": "doc\n", "data.csv": "x,1\ny,2\n", "big.log": "log\n",
	"src/main.go": "package main\n", "logs/x.log": "x\n", "logs/y.log": "y\n",
	"build/out.o": "o\n", "backup/old.txt": "old\n",
}

func layout(dir string, r row) error {
	if r.Fx != nil {
		if err := oracle.Materialize(dir, r.Fx); err != nil {
			return err
		}
		return os.MkdirAll(filepath.Join(dir, "home"), 0o755)
	}
	for p, body := range handBuilt {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return os.MkdirAll(filepath.Join(dir, "tmp"), 0o755)
}

func main() {
	fixtures := flag.String("fixtures", "", "also lay out each command's starting files under this directory")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: extbaselines [-fixtures DIR] corpus.jsonl")
		os.Exit(2)
	}
	f, err := os.Open(flag.Arg(0))
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
		if r.CmdFx != "" {
			r.Command = r.CmdFx
		}
		m := codex.DangerousScript(r.Command)
		reason := map[codex.Match]string{codex.None: "", codex.ForcedRm: "forced rm", codex.Other: "wrapper depth exceeded"}[m]
		enc.Encode(result{ID: r.ID, System: "CODEX", Prompted: m != codex.None, Reason: reason})
		if *fixtures != "" {
			if err := layout(filepath.Join(*fixtures, r.ID), r); err != nil {
				fmt.Fprintln(os.Stderr, r.ID, err)
				os.Exit(1)
			}
		}
	}
}
