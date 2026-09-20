// Command corpusgen builds a labelled evaluation corpus with no human in the loop.
//
// Every executable item is given a fixture, run once in a bubblewrap sandbox, and
// labelled from the filesystem diff (internal/oracle). Items that do not run
// cleanly are excluded, never guessed. A third partition, "external", holds
// commands whose effects the filesystem cannot show (remote hosts, mounts,
// signals, devices); it is never executed, and its label follows from the kind of
// command by construction.
//
// The generator shares no code with internal/effects, so agreement between the
// analysis and the labels is evidence rather than circularity. The same seed always
// draws the same candidates.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"mvdan.cc/sh/v3/syntax"

	"synapseos/internal/oracle"
)

type item struct {
	ID        string         `json:"id"`
	Partition string         `json:"partition"` // N: NL2Bash, T: template, E: external
	Src       string         `json:"src"`
	Shape     string         `json:"shape"`
	Label     string         `json:"label"` // R, C, or U (external)
	Ambiguous int            `json:"ambiguous"`
	Note      string         `json:"note,omitempty"`
	Command   string         `json:"command"`
	Fx        []oracle.Entry `json:"fx"`
	Setup     string         `json:"setup,omitempty"`
}

type candidate struct {
	src, cmd, setup string
	partition       string
}

func main() {
	seed := flag.Int64("seed", 20260922, "random seed")
	nl2bash := flag.String("nl2bash", "pilot/nl2bash_all.cm", "NL2Bash command file, one per line (git-ignored, GPL-3.0)")
	useTemplates := flag.Bool("templates", true, "add template-generated items")
	nWant := flag.Int("n", 350, "labelled NL2Bash items to produce")
	minC := flag.Int("min-c", 100, "keep drawing NL2Bash until at least this many are labelled C")
	nTmpl := flag.Int("tn", 200, "template items to produce")
	nExt := flag.Int("ext", 50, "external NL2Bash items to include")
	exclude := flag.String("exclude", "pilot/corpus.jsonl,pilot/corpus2.jsonl,pilot/corpus3.jsonl", "corpora whose NL2Bash items must not be reused")
	out := flag.String("out", "pilot/corpus4.jsonl", "corpus output")
	report := flag.String("report", "pilot/oracle_report4.txt", "report output")
	flag.Parse()

	bw := oracle.Bwrap()
	if bw == "" {
		fmt.Fprintln(os.Stderr, "corpusgen: bubblewrap is required")
		os.Exit(1)
	}
	rng := rand.New(rand.NewSource(*seed))
	rep := newReport()

	var items []item
	if *nl2bash != "" {
		items = append(items, drawNL2Bash(bw, rng, *seed, *nl2bash, *exclude, *nWant, *minC, *nExt, rep)...)
	}
	if *useTemplates {
		items = append(items, drawTemplates(bw, rng, *seed, *nTmpl, rep)...)
	}
	number(items)

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w := bufio.NewWriter(f)
	for _, it := range items {
		b, _ := json.Marshal(it)
		w.Write(b)
		w.WriteByte('\n')
	}
	w.Flush()
	f.Close()
	rep.summarise(items, *seed)
	if err := os.WriteFile(*report, []byte(rep.text()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(rep.text())
}

func number(items []item) {
	c := map[string]int{}
	for i := range items {
		c[items[i].Partition]++
		items[i].ID = fmt.Sprintf("%s%03d", items[i].Partition, c[items[i].Partition])
	}
}

// ---- NL2Bash ----

var srcRe = regexp.MustCompile(`nl2bash:(\d+)`)

func usedSources(list string) map[int]bool {
	used := map[int]bool{}
	for _, p := range strings.Split(list, ",") {
		b, err := os.ReadFile(strings.TrimSpace(p))
		if err != nil {
			continue
		}
		for _, m := range srcRe.FindAllStringSubmatch(string(b), -1) {
			var n int
			fmt.Sscan(m[1], &n)
			used[n] = true
		}
	}
	return used
}

func drawNL2Bash(bw string, rng *rand.Rand, seed int64, file, exclude string, nWant, minC, nExt int, rep *report) []item {
	raw, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "corpusgen:", err)
		os.Exit(1)
	}
	cm := strings.Split(string(raw), "\n")
	used := usedSources(exclude)
	perm := rng.Perm(len(cm))
	seen := map[string]bool{}
	var cands []candidate
	for _, i := range perm {
		c := strings.TrimSpace(cm[i])
		if c == "" || used[i] || seen[c] || len(c) > 400 || strings.Contains(c, "\n") {
			continue
		}
		seen[c] = true
		cands = append(cands, candidate{src: fmt.Sprintf("nl2bash:%d", i), cmd: c, partition: "N"})
	}

	// Quotas per class, so the corpus has enough losses to measure. Within a class
	// the items are simply the first drawn, so the choice is not the author's.
	var out, ext []item
	nC, nR := 0, 0
	nRWant := max(nWant-minC, 0)
	const chunk = 48
	for at := 0; at < len(cands) && (nR < nRWant || nC < minC || len(ext) < nExt); at += chunk {
		end := min(at+chunk, len(cands))
		res := make([]*item, end-at)
		why := make([]string, end-at)
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for j := at; j < end; j++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(j int) {
				defer wg.Done()
				defer func() { <-sem }()
				res[j-at], why[j-at] = label(bw, seed, cands[j])
			}(j)
		}
		wg.Wait()
		for j := range res {
			rep.drawn++
			switch {
			case res[j] == nil:
				rep.excluded[why[j]]++
			case res[j].Partition == "E":
				if len(ext) < nExt {
					ext = append(ext, *res[j])
				}
			case res[j].Label == "C" && nC < minC:
				out = append(out, *res[j])
				nC++
			case res[j].Label == "R" && nR < nRWant:
				out = append(out, *res[j])
				nR++
			}
		}
	}
	return append(out, ext...)
}

// label runs one candidate and returns its item, or nil and the reason it is excluded.
func label(bw string, seed int64, c candidate) (*item, string) {
	shape := shapeOf(c.cmd)
	if isExternal(c.cmd) {
		return &item{Partition: "E", Src: c.src, Shape: "external", Label: "U", Command: c.cmd, Fx: oracle.Infer(c.cmd, 0),
			Note: "effects outside the filesystem; labelled by construction, never executed"}, ""
	}
	h := fnv.New32a()
	fmt.Fprintf(h, "%d/%s", seed, c.src)
	first := oracle.Variant(h.Sum32() % 2)
	var last oracle.Outcome
	for _, v := range []oracle.Variant{first, 1 - first} {
		fx := oracle.Infer(c.cmd, v)
		vd, oc, err := oracle.Run(context.Background(), bw, fx, c.setup, c.cmd, 3*time.Second)
		if err != nil {
			return nil, "harness error"
		}
		last = oc
		if oc.Clean() {
			return &item{Partition: c.partition, Src: c.src, Shape: shape, Label: vd.Label, Note: vd.Reason,
				Command: c.cmd, Fx: fx, Setup: c.setup}, ""
		}
	}
	switch {
	case last.TimedOut:
		return nil, "timed out"
	case strings.Contains(last.Stderr, "Read-only file system") || strings.Contains(last.Stderr, "Permission denied"):
		return nil, "wrote outside the fixture or needed privilege"
	case last.ExitCode == 127:
		return nil, "command not installed"
	default:
		return nil, "non-zero exit"
	}
}

// ---- shape and external partition ----

func shapeOf(cmd string) string {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
	if err != nil {
		return "plain"
	}
	hidden, runtime, redirect := false, false, false
	stmts, calls := len(f.Stmts), 0
	syntax.Walk(f, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.CallExpr:
			calls++
			if len(x.Args) > 0 {
				switch path.Base(lit(x.Args[0])) {
				case "sh", "bash", "zsh", "dash", "eval", "python", "python3", "perl", "ruby", "awk", "node", "env", "nohup", "time", "command", "busybox":
					hidden = true
				case "find", "xargs":
					runtime = true
				}
			}
		case *syntax.CmdSubst, *syntax.ProcSubst, *syntax.ForClause, *syntax.WhileClause:
			runtime = true
		case *syntax.Redirect:
			if x.Op == syntax.RdrOut || x.Op == syntax.AppOut || x.Op == syntax.RdrClob {
				redirect = true
			}
		}
		return true
	})
	switch {
	case hidden:
		return "hidden"
	case runtime:
		return "runtime"
	case stmts > 1 || calls > 1:
		return "compound"
	case redirect:
		return "redirect"
	}
	return "plain"
}

func lit(w *syntax.Word) string {
	var b strings.Builder
	for _, p := range w.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				if l, ok := q.(*syntax.Lit); ok {
					b.WriteString(l.Value)
				}
			}
		}
	}
	return b.String()
}

// externalTools are tools that change something other than the files in the fixture:
// another machine, the mount table, another process, a device, an account, the
// firewall. This is a partition rule, not a label source: it says "the filesystem
// diff cannot show this", and such an item is never executed. It names only tools
// that change state. Read-only network queries, pagers, editors, and monitors are
// not in it, so the corpus does not count them as losses the system failed to catch.
var externalTools = map[string]bool{
	"ssh": true, "scp": true, "sftp": true, "mount": true, "umount": true, "kill": true, "killall": true, "pkill": true,
	"shutdown": true, "reboot": true, "halt": true, "poweroff": true, "useradd": true, "userdel": true, "usermod": true,
	"passwd": true, "groupadd": true, "mkfs": true, "fdisk": true, "parted": true, "losetup": true, "chroot": true,
	"swapon": true, "swapoff": true, "modprobe": true, "insmod": true, "iptables": true, "ip6tables": true,
	"docker": true, "kubectl": true, "su": true,
}

// networkSend is the flags that make curl or wget send data or change remote state.
var networkSend = regexp.MustCompile(`^(-X|--request|-d|--data.*|-F|--form|-T|--upload-file|--post.*|--method)$`)

var wrappers = map[string]bool{"sudo": true, "doas": true, "env": true, "time": true, "nohup": true, "nice": true, "xargs": true,
	"exec": true, "command": true, "timeout": true, "ionice": true}

var hostOperand = regexp.MustCompile(`^[A-Za-z0-9_.-]+@?[A-Za-z0-9_.-]+:[^\s]`)

func isExternal(cmd string) bool {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
	if err != nil {
		return false
	}
	ext := false
	syntax.Walk(f, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		words := make([]string, len(call.Args))
		for i, w := range call.Args {
			words[i] = lit(w)
		}
		i := 0
		for i < len(words) && (wrappers[path.Base(words[i])] || strings.HasPrefix(words[i], "-") || strings.Contains(words[i], "=") && i > 0) {
			i++
		}
		if i < len(words) && externalTools[path.Base(words[i])] {
			ext = true
		}
		if i < len(words) && (path.Base(words[i]) == "curl" || path.Base(words[i]) == "wget") {
			get := false
			for _, w := range words[i+1:] {
				get = get || w == "-G" || w == "--get"
			}
			for _, w := range words[i+1:] {
				if networkSend.MatchString(w) && !(get && strings.HasPrefix(w, "--data")) && !(get && w == "-d") {
					ext = true
				}
			}
		}
		if i < len(words) && (path.Base(words[i]) == "rsync" || path.Base(words[i]) == "scp") {
			for _, w := range words[i+1:] {
				if hostOperand.MatchString(w) {
					ext = true
				}
			}
		}
		for _, w := range words {
			if strings.HasPrefix(w, "/dev/sd") || strings.HasPrefix(w, "/dev/nvme") || strings.HasPrefix(w, "/dev/hd") {
				ext = true
			}
		}
		return true
	})
	return ext
}

// ---- report ----

type report struct {
	drawn    int
	excluded map[string]int
	lines    []string
}

func newReport() *report { return &report{excluded: map[string]int{}} }

func (r *report) summarise(items []item, seed int64) {
	type k struct{ part, shape, label string }
	count := map[k]int{}
	part := map[string]int{}
	lab := map[string]int{}
	for _, it := range items {
		count[k{it.Partition, it.Shape, it.Label}]++
		part[it.Partition]++
		lab[it.Label]++
	}
	r.lines = append(r.lines, fmt.Sprintf("seed %d; %d NL2Bash candidates drawn (templates are not drawn)", seed, r.drawn))
	r.lines = append(r.lines, fmt.Sprintf("items: %d total; partitions %v; labels %v", len(items), part, lab))
	keys := make([]k, 0, len(count))
	for kk := range count {
		keys = append(keys, kk)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.part != b.part {
			return a.part < b.part
		}
		if a.shape != b.shape {
			return a.shape < b.shape
		}
		return a.label < b.label
	})
	r.lines = append(r.lines, "", "partition shape label count")
	for _, kk := range keys {
		r.lines = append(r.lines, fmt.Sprintf("  %s  %-9s %s  %d", kk.part, kk.shape, kk.label, count[kk]))
	}
	r.lines = append(r.lines, "", "excluded (ran, did not finish cleanly; never labelled):")
	var ks []string
	for e := range r.excluded {
		ks = append(ks, e)
	}
	sort.Strings(ks)
	for _, e := range ks {
		r.lines = append(r.lines, fmt.Sprintf("  %-50s %d", e, r.excluded[e]))
	}
}

func (r *report) text() string { return strings.Join(r.lines, "\n") + "\n" }
