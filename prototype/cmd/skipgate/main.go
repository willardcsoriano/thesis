// Command skipgate reads `go test -json` on standard input and fails unless
// every test that ran passed and every test that skipped is named in the
// allowlist with a reason. `go test` itself treats a skip as a pass, so a
// host that lacks a sandbox tool could otherwise turn whole packages of
// sandbox tests into a green run that tested nothing.
//
//	go test -json ./... | skipgate -allow ci-allowed-skips.txt
//
// It prints the output of every failed test and build, each skip the
// allowlist does not name, with the test's own reason, and each allowlist
// entry that names no test the run saw (stale, so it cannot outlive the test
// it excuses). Exit status 1 for any of those, 2 for a bad allowlist.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("skipgate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	allowPath := fs.String("allow", "", "allowlist: one `package test reason` per line, # comments")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	allow := map[key]string{}
	if *allowPath != "" {
		f, err := os.Open(*allowPath)
		if err != nil {
			fmt.Fprintln(stderr, "skipgate:", err)
			return 2
		}
		allow, err = readAllow(f)
		f.Close()
		if err != nil {
			fmt.Fprintf(stderr, "skipgate: %s: %v\n", *allowPath, err)
			return 2
		}
	}
	r, err := read(in)
	if err != nil {
		fmt.Fprintln(stderr, "skipgate:", err)
		return 1
	}
	return r.verdict(allow, stdout)
}

type key struct{ pkg, test string }

func (k key) String() string { return k.pkg + " " + k.test }

// readAllow parses the allowlist: a package import path, a test name as
// go test -json reports it (Parent/sub for a subtest), and the reason, which
// is required. Blank lines and lines starting with # are ignored.
func readAllow(r io.Reader) (map[key]string, error) {
	out := map[key]string{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			return nil, fmt.Errorf("line %d: want `package test reason`, with a reason: %q", n, line)
		}
		k := key{f[0], f[1]}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("line %d: %s is listed twice", n, k)
		}
		out[k] = strings.Join(f[2:], " ")
	}
	return out, sc.Err()
}

// event is the part of test2json's TestEvent and the build's BuildEvent the
// gate reads.
type event struct {
	Action     string
	Package    string
	Test       string
	Output     string
	ImportPath string
}

type results struct {
	seen    map[key]bool             // every test that started
	output  map[key]*strings.Builder // per test, and per package under test ""
	build   map[string]*strings.Builder
	failed  []key // tests, and packages under test ""
	skipped []key
	builds  []string // import paths whose build failed
	noise   []string // lines that are not events
	tests   int
}

func read(in io.Reader) (*results, error) {
	r := &results{seen: map[key]bool{}, output: map[key]*strings.Builder{}, build: map[string]*strings.Builder{}}
	br := bufio.NewReader(in)
	for {
		line, err := br.ReadBytes('\n')
		if t := strings.TrimSpace(string(line)); t != "" {
			var e event
			if json.Unmarshal([]byte(t), &e) != nil || e.Action == "" {
				r.noise = append(r.noise, t)
			} else {
				r.add(e)
			}
		}
		if err == io.EOF {
			return r, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func (r *results) add(e event) {
	k := key{e.Package, e.Test}
	switch e.Action {
	case "build-output":
		b := r.build[e.ImportPath]
		if b == nil {
			b = &strings.Builder{}
			r.build[e.ImportPath] = b
		}
		b.WriteString(e.Output)
	case "build-fail":
		r.builds = append(r.builds, e.ImportPath)
	case "run":
		r.seen[k] = true
		r.tests++
	case "output":
		b := r.output[k]
		if b == nil {
			b = &strings.Builder{}
			r.output[k] = b
		}
		b.WriteString(e.Output)
	case "fail":
		r.failed = append(r.failed, k)
	case "skip":
		// A package-level skip is a package with no test files.
		if e.Test != "" {
			r.skipped = append(r.skipped, k)
		}
	}
}

func (r *results) verdict(allow map[key]string, w io.Writer) int {
	bad := 0
	for _, t := range r.noise {
		fmt.Fprintf(w, "not a go test -json event: %s\n", t)
		bad++
	}
	for _, p := range r.builds {
		fmt.Fprintf(w, "--- BUILD FAILED: %s\n%s", p, text(r.build[p]))
		bad++
	}
	for _, k := range r.failed {
		if k.test == "" {
			fmt.Fprintf(w, "--- FAIL: package %s\n%s", k.pkg, text(r.output[k]))
		} else {
			fmt.Fprintf(w, "--- FAIL: %s\n%s", k, text(r.output[k]))
		}
		bad++
	}
	allowed := 0
	for _, k := range r.skipped {
		if reason, ok := allow[k]; ok {
			fmt.Fprintf(w, "allowed skip: %s (%s)\n", k, reason)
			allowed++
			continue
		}
		fmt.Fprintf(w, "--- SKIP not in the allowlist: %s\n%s", k, text(r.output[k]))
		bad++
	}
	var stale []string
	for k := range allow {
		if !r.seen[k] {
			stale = append(stale, k.String())
		}
	}
	sort.Strings(stale)
	for _, s := range stale {
		fmt.Fprintf(w, "stale allowlist entry, no such test ran: %s\n", s)
		bad++
	}
	fmt.Fprintf(w, "skipgate: %d tests ran, %d failed, %d skipped (%d allowed), %d builds failed, %d stale entries\n",
		r.tests, len(r.failed), len(r.skipped), allowed, len(r.builds), len(stale))
	if bad > 0 {
		return 1
	}
	return 0
}

func text(b *strings.Builder) string {
	if b == nil {
		return ""
	}
	return b.String()
}
