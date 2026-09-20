// Command effexplain prints what the effect analysis makes of one command, on the
// fixture the oracle would build for it. It exists to debug disagreements between
// the analysis and the corpus labels: pass a corpus id and file, or a command.
//
//	effexplain -corpus pilot/corpus4.jsonl N351
//	effexplain 'find . -name "*.o" -delete'
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"synapseos/internal/effects"
	"synapseos/internal/oracle"
)

func main() {
	corpus := flag.String("corpus", "", "corpus file to look an id up in")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: effexplain [-corpus file] <id|command>")
		os.Exit(2)
	}
	cmd, fx := flag.Arg(0), []oracle.Entry(nil)
	if *corpus != "" {
		f, err := os.Open(*corpus)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var r struct {
				ID, Command, Label, Note string
				Fx                       []oracle.Entry
			}
			json.Unmarshal(sc.Bytes(), &r)
			if r.ID == flag.Arg(0) {
				cmd, fx = r.Command, r.Fx
				fmt.Printf("truth: %s (%s)\n", r.Label, r.Note)
			}
		}
	}
	if fx == nil {
		fx = oracle.Infer(cmd, 0)
	}
	wd, _ := os.MkdirTemp("", "effexplain-")
	defer os.RemoveAll(wd)
	oracle.Materialize(wd, fx)
	os.MkdirAll(filepath.Join(wd, "home"), 0o755)
	an := effects.New(wd)
	an.Env = append(os.Environ(), "HOME="+filepath.Join(wd, "home"))
	an.Run = effects.DefaultRunner(8 * time.Second)
	res := an.Analyze(context.Background(), cmd)
	v := res.Verdict()
	fmt.Println("command:", cmd)
	fmt.Println("class:  ", v.Class)
	for _, e := range res.Effects {
		to := ""
		if e.MovedTo != "" {
			to = " -> " + e.MovedTo
		}
		fmt.Printf("  %-8s %s%s\n", e.Kind, e.Path, to)
	}
	for _, s := range res.States {
		fmt.Printf("  state    %s (undo: %v)\n", s.Change, s.Inverse)
	}
	for _, is := range res.Issues {
		fmt.Printf("  issue    %s: %s\n", is.Kind, is.Reason)
	}
}
