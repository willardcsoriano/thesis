// Command effexplain prints what the effect analysis makes of one command, on the
// fixture the oracle would build for it. It exists to debug disagreements between
// the analysis and the corpus labels: pass a corpus id and file, or a command.
// -nocompose runs the composition-ablated configuration (docs/open-problems.md row
// 38) instead, for side-by-side comparison against the default.
//
//	effexplain -corpus pilot/corpus4.jsonl N351
//	effexplain 'find . -name "*.o" -delete'
//	effexplain -nocompose 'find . -name "*.o" -delete'
//	effexplain -dir ~/some/folder 'find . -name "*.o" -delete'
//	effexplain -dir ~/some/folder -list 'find . -name "*.o" -delete'
//
// -list also prints what today's pattern-list classifier would do with the command,
// the baseline the analysis is evaluated against.
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

	"synapseos/internal/classifier"
	"synapseos/internal/effects"
	"synapseos/internal/oracle"
)

func main() {
	corpus := flag.String("corpus", "", "corpus file to look an id up in")
	nocompose := flag.Bool("nocompose", false, "disable composition (wrapper/find-exec/xargs/eval/shell/loop resolution), per docs/open-problems.md row 38")
	dir := flag.String("dir", "", "analyse against this existing directory instead of a generated fixture (read-only; nothing in it is changed)")
	list := flag.Bool("list", false, "also print what today's pattern-list classifier would do with the command")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: effexplain [-corpus file] [-dir path] [-nocompose] [-list] <id|command>")
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
	var wd string
	var an *effects.Analyzer
	if *dir != "" {
		abs, err := filepath.Abs(*dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		wd = abs
		an = effects.New(wd)
	} else {
		if fx == nil {
			fx = oracle.Infer(cmd, 0)
		}
		wd, _ = os.MkdirTemp("", "effexplain-")
		defer os.RemoveAll(wd)
		oracle.Materialize(wd, fx)
		os.MkdirAll(filepath.Join(wd, "home"), 0o755)
		an = effects.New(wd)
		an.Env = append(os.Environ(), "HOME="+filepath.Join(wd, "home"))
	}
	an.Run = effects.DefaultRunner(8 * time.Second)
	an.Compose = !*nocompose
	if *list {
		v, why := classifier.ClassifyForDir(cmd, wd)
		switch {
		case v == classifier.Irreversible && classifier.Protected(cmd, wd):
			fmt.Println("pattern list (today):  asks first, and takes its own backup (" + why + ")")
		case v == classifier.Irreversible:
			fmt.Println("pattern list (today):  asks first, with no backup (" + why + ")")
		case classifier.Protected(cmd, wd):
			fmt.Println("pattern list (today):  runs it without asking, with its own backup")
		default:
			fmt.Println("pattern list (today):  runs it without asking, with no backup")
		}
	}
	res := an.Analyze(context.Background(), cmd)
	v := res.Verdict()
	fmt.Println("command:   ", cmd)
	if *nocompose {
		fmt.Println("composition: disabled (table alone, ablation)")
	} else {
		fmt.Println("composition: enabled (full algorithm)")
	}
	fmt.Println("class:     ", v.Class)
	for _, e := range res.Effects {
		to := ""
		if e.MovedTo != "" {
			to = " -> " + rel(wd, e.MovedTo)
		}
		fmt.Printf("  %-8s %s%s\n", e.Kind, rel(wd, e.Path), to)
	}
	for _, s := range res.States {
		fmt.Printf("  state    %s (undo: %v)\n", s.Change, s.Inverse)
	}
	for _, is := range res.Issues {
		fmt.Printf("  issue    %s: %s\n", is.Kind, is.Reason)
	}
}

// rel shows a path relative to the analysed directory, so output carries no
// temporary or home-directory prefix.
func rel(wd, p string) string {
	r, err := filepath.Rel(wd, p)
	if err != nil {
		return p
	}
	return "./" + r
}
