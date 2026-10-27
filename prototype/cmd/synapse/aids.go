package main

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"

	"synapseos/internal/effects"
	"synapseos/internal/gate"
)

// Harness aids: deterministic help around the model, each answering a
// weakness measured in the Layer 7 and 9 runs of 2026-10-25 (development
// data; experiments.md). Each can be switched off with SYNAPSE_AIDS, so its
// effect is measured by running with and without it, never assumed.
const (
	// aidPeek shows the model the first lines of each file the request
	// names. 9 of 3B's 16 Layer 7 misses wrote the command before seeing
	// the data: awk without -F, on a CSV, WARNING where the file says WARN.
	aidPeek = "peek"
	// aidTools does not run a command whose program is not installed, and
	// says so. 3B ran pdftk, got "command not found", and gave up.
	aidTools = "tools"
	// aidRefs lists what earlier requests in the session created or changed,
	// newest first, so "there" and "it" have something to point at. All of
	// 3B's Layer 9 misses were references.
	aidRefs = "refs"
	// aidLint does not run a command that would run cleanly but cannot give
	// what it seems meant to, and says why and what works: awk's $2 on a
	// file with no spaces, a count that includes ls -l's "total" line, a
	// sort of ls -l lines by their permissions. 3B made each of these and
	// reported the wrong number as the answer (2026-10-26).
	aidLint = "lint"
	// aidLiteral says so when the request is itself a shell command, which
	// 3B otherwise sometimes rewrote into different steps.
	aidLiteral = "literal"
)

var allAids = []string{aidPeek, aidTools, aidRefs, aidLint, aidLiteral}

// parseAids reads SYNAPSE_AIDS: unset or "all" is every aid, "none" is no
// aid, otherwise a comma-separated list of the aids to use.
func parseAids(v string) (map[string]bool, error) {
	on := map[string]bool{}
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "", "all":
		for _, a := range allAids {
			on[a] = true
		}
		return on, nil
	case "none":
		return on, nil
	}
	for _, part := range strings.Split(v, ",") {
		name := strings.TrimSpace(strings.ToLower(part))
		known := false
		for _, a := range allAids {
			known = known || a == name
		}
		if !known {
			return nil, fmt.Errorf("unknown aid %q (known: %s, or all, or none)", name, strings.Join(allAids, ", "))
		}
		on[name] = true
	}
	return on, nil
}

// aidOn reports whether aid is enabled. checkEnv has already refused a bad
// value, so an error here cannot happen in a real run; it counts as on.
func aidOn(aid string) bool {
	on, err := parseAids(os.Getenv("SYNAPSE_AIDS"))
	return err != nil || on[aid]
}

func checkAids(v string) error {
	_, err := parseAids(v)
	return err
}

// --- peek ---

const (
	peekFiles = 3   // files shown at most
	peekLines = 5   // lines per file
	peekWidth = 160 // characters per line
	peekBytes = 2048
)

// namedFiles returns the regular files in dir whose names appear in task as
// words, in the order the task names them. Matching is on whole names only
// ("data.csv"), never guessed from part of a name.
func namedFiles(task, dir string) []string {
	return namedEntries(task, dir, false)
}

// namedEntries is namedFiles, with folders too when dirs is set (named as
// "old" or "old/", returned as "old/").
func namedEntries(task, dir string, dirs bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	byName := map[string]string{}
	for _, e := range entries {
		switch {
		case e.Type().IsRegular():
			byName[strings.ToLower(e.Name())] = e.Name()
		case dirs && e.IsDir():
			byName[strings.ToLower(e.Name())] = e.Name() + "/"
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(task, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == ';' || r == '"' || r == '\'' || r == '(' || r == ')' || r == '?' || r == '!' || r == ':'
	}) {
		w = strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(w), "."), "/")
		if name, ok := byName[w]; ok && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// peekSection shows the start of each text file the task names, so the model
// sees the format (a header row, the delimiter, the exact words) before it
// writes a command. Binary files are named, not shown.
func peekSection(task, dir string) string {
	files := namedFiles(task, dir)
	if len(files) == 0 {
		return ""
	}
	if len(files) > peekFiles {
		files = files[:peekFiles]
	}
	var b strings.Builder
	b.WriteString("The start of each file the request names, so you can see its format (it may be longer):\n")
	for _, name := range files {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		buf := make([]byte, peekBytes)
		n, _ := f.Read(buf)
		f.Close()
		buf = buf[:n]
		if n == 0 {
			fmt.Fprintf(&b, "== %s: empty\n", name)
			continue
		}
		if strings.ContainsRune(string(buf), 0) || !utf8.Valid(trimPartialRune(buf)) {
			fmt.Fprintf(&b, "== %s: not text\n", name)
			continue
		}
		lines := strings.Split(string(buf), "\n")
		if len(lines) > peekLines {
			lines = lines[:peekLines]
		}
		fmt.Fprintf(&b, "== %s\n", name)
		for _, l := range lines {
			if len(l) > peekWidth {
				l = l[:peekWidth] + "…"
			}
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// trimPartialRune drops a multibyte character cut at the end of a read.
func trimPartialRune(b []byte) []byte {
	for i := 0; i < 3 && len(b) > 0 && !utf8.Valid(b); i++ {
		b = b[:len(b)-1]
	}
	return b
}

// --- tools ---

// shellWords are bash builtins and keywords: commands that need no program.
var shellWords = map[string]bool{
	"alias": true, "bg": true, "bind": true, "break": true, "builtin": true, "caller": true, "cd": true, "command": true,
	"compgen": true, "complete": true, "compopt": true, "continue": true, "declare": true, "dirs": true, "disown": true,
	"echo": true, "enable": true, "eval": true, "exec": true, "exit": true, "export": true, "false": true, "fc": true,
	"fg": true, "getopts": true, "hash": true, "help": true, "history": true, "jobs": true, "kill": true, "let": true,
	"local": true, "logout": true, "mapfile": true, "popd": true, "printf": true, "pushd": true, "pwd": true, "read": true,
	"readarray": true, "readonly": true, "return": true, "set": true, "shift": true, "shopt": true, "source": true,
	"suspend": true, "test": true, "times": true, "trap": true, "true": true, "type": true, "typeset": true,
	"ulimit": true, "umask": true, "unalias": true, "unset": true, "wait": true, ".": true, ":": true, "[": true,
}

// missingPrograms returns the programs cmd starts that are not installed,
// sorted. Only literal command names are checked: a name built at run time
// ($x) or a function the line defines is left alone, and a line that does
// not parse is left to the gate.
func missingPrograms(cmd string) []string {
	f, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
	if err != nil {
		return nil
	}
	defined := map[string]bool{}
	syntax.Walk(f, func(n syntax.Node) bool {
		if fd, ok := n.(*syntax.FuncDecl); ok && fd.Name != nil {
			defined[fd.Name.Value] = true
		}
		return true
	})
	missing := map[string]bool{}
	syntax.Walk(f, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		name := call.Args[0].Lit()
		if name == "" || shellWords[name] || defined[name] {
			return true
		}
		if _, err := exec.LookPath(name); err != nil && !errors.Is(err, exec.ErrDot) {
			missing[name] = true
		}
		return true
	})
	var out []string
	for m := range missing {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// missingProgramsNote is what the model and the person are told instead of
// running the command, with the installed programs whose names are like the
// missing ones or like the kind of file the command works on. Told only "not
// installed", 3B gave up on combining PDFs with pdfunite on the machine.
func missingProgramsNote(progs []string, cmd string) string {
	verb := "is"
	if len(progs) > 1 {
		verb = "are"
	}
	note := fmt.Sprintf("%s %s %s. Do not give up: try a command that uses only programs that are installed, such as standard Linux tools.", strings.Join(progs, " and "), verb, notInstalledPhrase)
	if similar := similarPrograms(progs, cmd); len(similar) > 0 {
		note += " Installed programs with similar names: " + strings.Join(similar, ", ") + "."
	}
	return note
}

// notInstalledPhrase marks a step the tools aid refused, so the loop can ask
// once more when the model gives up straight after one.
const notInstalledPhrase = "not installed on this computer, so this was not run"

// reaskAfterMissing is the request again, for one more proposal, when the
// model gave up straight after a command was refused for a program that is
// not installed, or "" when that is not what happened. 3B gave up on merging
// PDFs with pdfunite named in the refusal (2026-10-26). It says what each
// similar program does, from its manual page, so the model can choose by
// purpose and not by name; the refusal itself has no room for that, since a
// step's error is cut at stepOutputChars.
func reaskAfterMissing(task string, history []loopStep) string {
	n := len(history)
	if n == 0 || !strings.Contains(history[n-1].result.Stderr, notInstalledPhrase) {
		return ""
	}
	last := history[n-1].command
	var b strings.Builder
	b.WriteString(task)
	b.WriteString("\n(The program tried is not installed, but an installed one may do this.")
	if similar := similarPrograms(missingPrograms(last), last); len(similar) > 0 && len(similar) <= similarDescribed {
		b.WriteString(" Installed programs that may help, with what each does:")
		for _, p := range similar {
			if d := manDescription(p); d != "" {
				fmt.Fprintf(&b, "\n- %s: %s", p, d)
			} else {
				fmt.Fprintf(&b, "\n- %s", p)
			}
		}
		b.WriteString("\n")
	} else {
		b.WriteString(" ")
	}
	b.WriteString("Propose a command that uses an installed program. Answer UNSUPPORTED only if none can do it.)")
	return b.String()
}

// similarDescribed bounds the programs the re-ask describes one per line.
const similarDescribed = 20

// manDirs are where manDescription looks; a variable so a test can point it
// at its own pages.
var manDirs = []string{"/usr/share/man", "/usr/local/share/man"}

// manDescription is the one-line description in a program's manual page
// (its NAME section: "pdfunite \- Portable Document Format (PDF) page
// merger"), or "" when there is no page. Read from the file, never by
// running the program.
func manDescription(name string) string {
	for _, d := range manDirs {
		for _, sec := range []string{"1", "8"} {
			for _, gz := range []bool{true, false} {
				p := filepath.Join(d, "man"+sec, name+"."+sec)
				if gz {
					p += ".gz"
				}
				if text, ok := readStart(p, gz); ok {
					return manNameLine(text)
				}
			}
		}
	}
	return ""
}

// readStart is the first 8 KiB of a file, unpacked if gz.
func readStart(p string, gz bool) (string, bool) {
	f, err := os.Open(p)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var r io.Reader = f
	if gz {
		z, err := gzip.NewReader(f)
		if err != nil {
			return "", false
		}
		defer z.Close()
		r = z
	}
	buf := make([]byte, 8192)
	n, _ := io.ReadFull(r, buf)
	return string(buf[:n]), n > 0
}

var troffFont = regexp.MustCompile(`\\f(\[[A-Z]*\]|\(..|.)`)

// manNameLine is the description after the dash on the line under .SH NAME.
func manNameLine(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		head := strings.ToUpper(strings.TrimSpace(l))
		if !strings.HasPrefix(head, ".SH") || !strings.Contains(head, "NAME") {
			continue
		}
		for _, next := range lines[i+1:] {
			next = strings.TrimSpace(next)
			if next == "" || strings.HasPrefix(next, ".") || strings.HasPrefix(next, "'") {
				continue
			}
			next = troffFont.ReplaceAllString(next, "")
			next = strings.NewReplacer(`\-`, "-", `\(em`, "-", `\&`, "", `\ `, " ").Replace(next)
			if _, desc, ok := strings.Cut(next, " - "); ok {
				desc = strings.TrimSpace(desc)
				if len(desc) > 100 {
					desc = desc[:100] + "…"
				}
				return desc
			}
			return ""
		}
	}
	return ""
}

const (
	similarMax  = 40 // names listed at most; a desktop has 36 pdf… programs
	similarStem = 3  // leading letters of a missing name that others must share
)

// similarPrograms lists installed programs, sorted, whose names begin with
// the first letters of a missing program's name (pdftk: pdf…) or contain the
// extension of a file the command names (a.pdf: …pdf…), at most similarMax
// of them, the rest counted.
func similarPrograms(missing []string, cmd string) []string {
	var prefixes, parts []string
	for _, m := range missing {
		base := strings.ToLower(filepath.Base(m))
		if len(base) >= similarStem {
			prefixes = append(prefixes, base[:similarStem])
		}
	}
	for _, w := range strings.Fields(cmd) {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(strings.Trim(w, `"'`)), "."))
		if len(ext) >= similarStem && isAlnum(ext) {
			parts = append(parts, ext)
		}
	}
	if len(prefixes)+len(parts) == 0 {
		return nil
	}
	skip := map[string]bool{}
	for _, m := range missing {
		skip[m] = true
	}
	found := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if skip[name] || found[name] || !matchesAny(strings.ToLower(name), prefixes, parts) {
				continue
			}
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
				found[name] = true
			}
		}
	}
	out := make([]string, 0, len(found))
	for n := range found {
		out = append(out, n)
	}
	sort.Strings(out)
	if len(out) > similarMax {
		out = append(out[:similarMax], fmt.Sprintf("and %d more", len(out)-similarMax))
	}
	return out
}

func matchesAny(name string, prefixes, parts []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	for _, p := range parts {
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}

func isAlnum(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// --- refs ---

// changedPaths lists, relative to wd, what a step created, changed, or moved
// something into, by the analysis's own effects, folders marked with "/".
func changedPaths(gd *gate.Decision, wd string) []string {
	if gd == nil || gd.Analysis == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(p string, dir bool) {
		if r, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(r, "..") {
			p = r
		}
		if dir {
			p += "/"
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, e := range gd.Analysis.Effects {
		switch {
		case e.Kind == effects.Remove && e.MovedTo != "":
			add(e.MovedTo, e.Dir)
		case e.Kind == effects.Create || e.Kind == effects.Write:
			add(e.Path, e.Dir)
		}
	}
	return out
}

// --- lint ---

// lintCommand returns why cmd, which would run without error, cannot give
// what it seems meant to, with what works instead; "" when nothing is known
// to be wrong. Each check fires only on a case it can prove from the command
// and the files it reads, so a command it passes is unchanged.
func lintCommand(cmd, wd string) string {
	f, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
	if err != nil {
		return ""
	}
	var notes []string
	add := func(n string) {
		for _, have := range notes {
			if have == n {
				return
			}
		}
		notes = append(notes, n)
	}
	syntax.Walk(f, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.Stmt:
			// awk reading a file named on it, or given with < file.
			if c, ok := x.Cmd.(*syntax.CallExpr); ok {
				if note := awkFieldNote(c, wd, inputRedirects(x)); note != "" {
					add(note)
				}
			}
		case *syntax.BinaryCmd:
			if x.Op != syntax.Pipe {
				break
			}
			left, right := lastCall(x.X), firstCall(x.Y)
			if note := lsPipeNote(left, right, wd); note != "" {
				add(note)
			}
			// cat file | awk …: awk reads what cat names.
			if note := awkFieldNote(right, wd, catFiles(left)); note != "" {
				add(note)
			}
			if note := duTotalNote(pipeCalls(x)); note != "" {
				add(note)
			}
		}
		return true
	})
	if len(notes) == 0 {
		return ""
	}
	return "Not run: " + strings.Join(notes, " ")
}

// wordValue is a word's text when it is fixed (plain, or quoted without
// expansions), and false when the shell would build it at run time.
func wordValue(w *syntax.Word) (string, bool) {
	var b strings.Builder
	for _, p := range w.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				l, ok := q.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(l.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}

// callWords is a call's words, or false if any is built at run time.
func callWords(c *syntax.CallExpr) ([]string, bool) {
	if c == nil || len(c.Args) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(c.Args))
	for _, w := range c.Args {
		v, ok := wordValue(w)
		if !ok {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

// lastCall and firstCall are the commands either side of a pipe: in a | b | c
// the shell groups (a | b) | c, so the left side's last command meets c.
func lastCall(s *syntax.Stmt) *syntax.CallExpr {
	switch x := s.Cmd.(type) {
	case *syntax.CallExpr:
		return x
	case *syntax.BinaryCmd:
		if x.Op == syntax.Pipe {
			return lastCall(x.Y)
		}
	}
	return nil
}

func firstCall(s *syntax.Stmt) *syntax.CallExpr {
	switch x := s.Cmd.(type) {
	case *syntax.CallExpr:
		return x
	case *syntax.BinaryCmd:
		if x.Op == syntax.Pipe {
			return firstCall(x.X)
		}
	}
	return nil
}

var (
	awkNames = map[string]bool{"awk": true, "gawk": true, "mawk": true, "nawk": true}
	// A field after the first: $2, $10. $1 and $0 are the whole line on a
	// line with no spaces, so they are right either way.
	awkLaterField = regexp.MustCompile(`\$([2-9]|[1-9][0-9]+)\b`)
	// Separators checked, in order of preference on a tie. Tab is not one:
	// awk already splits on it.
	fieldSeparators = []string{",", ";", "|", ":"}
)

// awkFieldNote: awk splits on blanks unless told otherwise, so on a file
// whose lines have no blanks every field after $1 is empty, and a sum of $2
// is 0, which 3B then reported as the answer. stdin is the files given to
// awk's standard input (cat data.csv | awk, awk … < data.csv), used when awk
// names none itself; 1.5B wrote the first.
func awkFieldNote(c *syntax.CallExpr, wd string, stdin []string) string {
	words, ok := callWords(c)
	if !ok || !awkNames[filepath.Base(words[0])] {
		return ""
	}
	var program string
	var files []string
	for i := 1; i < len(words); i++ {
		w := words[i]
		switch {
		case program == "" && (strings.HasPrefix(w, "-F") || strings.HasPrefix(w, "--field-separator") || w == "-f" || strings.HasPrefix(w, "--file")):
			return "" // a separator is set, or the program is in a file
		case program == "" && w == "-v":
			if i+1 < len(words) && strings.Contains(words[i+1], "FS=") {
				return ""
			}
			i++
		case program == "" && strings.HasPrefix(w, "-v") && strings.Contains(w, "FS="):
			return "" // -vFS=, attached: the separator is set
		case program == "" && strings.HasPrefix(w, "-"):
		case program == "":
			program = w
		case strings.Contains(w, "="):
			if strings.HasPrefix(w, "FS=") {
				return ""
			}
		default:
			files = append(files, w)
		}
	}
	if len(files) == 0 {
		files = stdin
	}
	if program == "" || strings.Contains(program, "FS") || !awkLaterField.MatchString(program) || len(files) == 0 {
		return ""
	}
	// Any one file proven wrong is enough: its lines give empty fields
	// whatever the others hold.
	field := awkLaterField.FindString(program)
	for _, name := range files {
		lines, ok := sampleLines(name, wd)
		if !ok || hasBlankField(lines) {
			continue
		}
		if sep := commonSeparator(lines); sep != "" {
			return fmt.Sprintf("awk splits each line on spaces unless told otherwise, and the lines of %s have no spaces, so %s is empty on every line and the result would be wrong. Its fields are separated by %q: give awk -F'%s'. If its first line is a header, skip it with NR>1.", name, field, sep, sep)
		}
	}
	return ""
}

// inputRedirects is the files a statement reads with "< file".
func inputRedirects(s *syntax.Stmt) []string {
	var out []string
	for _, r := range s.Redirs {
		if r.Op != syntax.RdrIn || r.Word == nil {
			continue
		}
		if v, ok := wordValue(r.Word); ok {
			out = append(out, v)
		}
	}
	return out
}

// catFiles is the files a plain cat names, so what it pipes on is known.
func catFiles(c *syntax.CallExpr) []string {
	words, ok := callWords(c)
	if !ok || filepath.Base(words[0]) != "cat" {
		return nil
	}
	var out []string
	for _, w := range words[1:] {
		if strings.HasPrefix(w, "-") {
			return nil // an option may change what it prints
		}
		out = append(out, w)
	}
	return out
}

// pipeCalls is every command of a pipeline in order, or nil if any part is
// not a plain command.
func pipeCalls(x *syntax.BinaryCmd) []*syntax.CallExpr {
	var out []*syntax.CallExpr
	var walk func(s *syntax.Stmt) bool
	walk = func(s *syntax.Stmt) bool {
		switch c := s.Cmd.(type) {
		case *syntax.CallExpr:
			out = append(out, c)
			return true
		case *syntax.BinaryCmd:
			return c.Op == syntax.Pipe && walk(c.X) && walk(c.Y)
		}
		return false
	}
	if !walk(x.X) || !walk(x.Y) {
		return nil
	}
	return out
}

// duTotalNote: du lists the folder it measures too ("200K ."), and that
// total is always the largest, so du sorted largest first and cut to one
// line names the folder, not anything in it (7B answered "the largest file
// is 200K"). Only du of the working folder, not -s (which prints nothing
// else) and not of a pattern such as *, which has no total.
func duTotalNote(calls []*syntax.CallExpr) string {
	sawDu, sorted, reverse := false, false, false
	for _, c := range calls {
		words, ok := callWords(c)
		if !ok {
			return ""
		}
		switch name := filepath.Base(words[0]); {
		case name == "du":
			for _, w := range words[1:] {
				switch {
				case w == "--summarize" || w == "-c" || w == "--total":
					return ""
				case strings.HasPrefix(w, "--"):
				case strings.HasPrefix(w, "-"):
					if strings.ContainsAny(w[1:], "sc") {
						return ""
					}
				case w != "." && w != "./":
					return "" // another folder, or files: not judged
				}
			}
			sawDu = true
		case !sawDu:
		case name == "sort":
			sorted = true
			for _, w := range words[1:] {
				if w == "--reverse" || hasShortFlag([]string{w}, 'r') {
					reverse = true
				}
			}
		case sorted && (name == "head" && reverse || name == "tail" && !reverse):
			if oneLine(words[1:]) {
				return "du also lists the folder it measures (\".\"), whose total is always the largest, so this shows the folder, not anything in it. For the largest file use ls -S | head -n 1; to rank what is here without the total, use du -sh * | sort -hr."
			}
			return ""
		default:
			// Anything else between du and the cut (grep -v '\.$', say) may
			// drop the total line, so the pipeline is not judged.
			return ""
		}
	}
	return ""
}

// oneLine reports whether head or tail's arguments ask for one line.
func oneLine(args []string) bool {
	for i, w := range args {
		switch {
		case w == "-1" || w == "-n1" || w == "--lines=1":
			return true
		case (w == "-n" || w == "--lines") && i+1 < len(args) && args[i+1] == "1":
			return true
		}
	}
	return false
}

// sampleLines is up to peekLines non-empty lines from the start of a text
// file, and false when the file cannot be read or is not text.
func sampleLines(name, wd string) ([]string, bool) {
	if strings.ContainsAny(name, "*?[") || name == "-" {
		return nil, false
	}
	p := name
	if !filepath.IsAbs(p) {
		p = filepath.Join(wd, p)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	buf := make([]byte, peekBytes)
	n, _ := f.Read(buf)
	buf = buf[:n]
	if strings.ContainsRune(string(buf), 0) || !utf8.Valid(trimPartialRune(buf)) {
		return nil, false
	}
	var out []string
	for _, l := range strings.Split(string(buf), "\n") {
		if strings.TrimSpace(l) != "" && len(out) < peekLines {
			out = append(out, l)
		}
	}
	return out, len(out) > 0
}

// hasBlankField reports whether any line splits on blanks into more than one
// field, so awk's default separator already works there.
func hasBlankField(lines []string) bool {
	for _, l := range lines {
		if len(strings.Fields(l)) > 1 {
			return true
		}
	}
	return false
}

// commonSeparator is the separator every sampled line contains, or "".
func commonSeparator(lines []string) string {
	for _, sep := range fieldSeparators {
		all := true
		for _, l := range lines {
			all = all && strings.Contains(l, sep)
		}
		if all {
			return sep
		}
	}
	return ""
}

// lsPipeNote checks ls -l piped into wc -l or sort. ls -l puts a "total" line
// before a folder's entries, so wc -l counts one more than there are (3B
// answered 3 for a folder of 2), and its lines start with the permissions,
// so sorting whole lines does not sort by size (3B named the 1-byte file as
// the biggest).
func lsPipeNote(left, right *syntax.CallExpr, wd string) string {
	lw, ok := callWords(left)
	if !ok || filepath.Base(lw[0]) != "ls" {
		return ""
	}
	rw, ok := callWords(right)
	if !ok {
		return ""
	}
	var flags string
	var operands []string
	for _, w := range lw[1:] {
		switch {
		case w == "--":
		case strings.HasPrefix(w, "--"):
			return "" // a long option may change the format; not judged
		case strings.HasPrefix(w, "-"):
			flags += w[1:]
		default:
			operands = append(operands, w)
		}
	}
	if !strings.Contains(flags, "l") {
		return ""
	}
	switch filepath.Base(rw[0]) {
	case "wc", "grep":
		// wc -l, or grep -c (1.5B counted with grep -c .)
		count := 'l'
		if filepath.Base(rw[0]) == "grep" {
			count = 'c'
		}
		if !hasShortFlag(rw[1:], count) || strings.Contains(flags, "d") || !allFolders(operands, wd) {
			return ""
		}
		// grep -c counts the total line only if its pattern matches it: grep
		// -c '^-' counts files and is right.
		if filepath.Base(rw[0]) == "grep" && !grepCountsTotalLine(rw[1:]) {
			return ""
		}
		where := ""
		if len(operands) == 1 {
			where = " " + operands[0]
		}
		return fmt.Sprintf("ls -l prints a \"total\" line before a folder's entries, so counting its lines gives one more than there are. To count them, use ls -A%s | wc -l.", where)
	case "sort":
		for _, w := range rw[1:] {
			if strings.HasPrefix(w, "--key") || hasShortFlag([]string{w}, 'k') {
				return ""
			}
		}
		return "sort compares whole lines, and each ls -l line starts with its permissions, so this does not sort by size, date or name. For the biggest file use ls -S | head -n 1 (newest: ls -t | head -n 1), or sort on the size column with sort -k5,5n (-k5,5h after ls -lh)."
	case "head":
		// After ls -lS, 3B took the first line, which was "total 200K".
		if !oneLine(rw[1:]) || strings.Contains(flags, "d") || !allFolders(operands, wd) {
			return ""
		}
		return "the first line ls -l prints for a folder is its \"total\" line, not a file. For the biggest file use ls -S | head -n 1 (newest: ls -t | head -n 1)."
	}
	return ""
}

// grepCountsTotalLine reports whether grep with these arguments would count
// ls -l's "total 8" line. A pattern it cannot read (from a file, several
// patterns, one Go's regexp rejects) is not judged, and answers false.
func grepCountsTotalLine(args []string) bool {
	var pattern string
	found, fixed, invert, fold := false, false, false, false
	for i := 0; i < len(args); i++ {
		w := args[i]
		switch {
		case w == "-e" && i+1 < len(args) && !found:
			pattern, found = args[i+1], true
			i++
		case w == "-f" || strings.HasPrefix(w, "--"):
			return false
		case strings.HasPrefix(w, "-") && len(w) > 1:
			fixed = fixed || strings.ContainsRune(w[1:], 'F')
			invert = invert || strings.ContainsRune(w[1:], 'v')
			fold = fold || strings.ContainsRune(w[1:], 'i')
			if strings.ContainsRune(w[1:], 'e') || strings.ContainsRune(w[1:], 'f') {
				return false
			}
		case !found:
			pattern, found = w, true
		}
	}
	if !found {
		return false
	}
	const line = "total 8"
	var matches bool
	if fixed {
		matches = strings.Contains(line, pattern) || (fold && strings.Contains(line, strings.ToLower(pattern)))
	} else {
		expr := pattern
		if fold {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return false
		}
		matches = re.MatchString(line)
	}
	return matches != invert
}

func hasShortFlag(words []string, f rune) bool {
	for _, w := range words {
		if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.ContainsRune(w[1:], f) {
			return true
		}
	}
	return false
}

// allFolders reports whether every operand is a folder, none meaning the
// working directory. ls -l prints no "total" line for files named on their
// own, and a pattern's matches are not known before it runs.
func allFolders(operands []string, wd string) bool {
	for _, o := range operands {
		if strings.ContainsAny(o, "*?[") {
			return false
		}
		p := o
		if !filepath.IsAbs(p) {
			p = filepath.Join(wd, p)
		}
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

// --- refs: the request's own words ---

var (
	// Not a bare "there": "is there a file" and "how many files are there"
	// ask whether something exists, here.
	placeWords = regexp.MustCompile(`(?i)\b(in there|into there|out of there|from there|in it|into it|inside it|out of it|in that folder|into that folder|that folder)\b`)
	thingWords = regexp.MustCompile(`(?i)\b(it)\b`)
)

// referenceNote says, beside the request, what its "there" or "it" most
// likely means: the folder or thing this session changed most recently. The
// list in the session history was not enough: 3B counted the working
// directory for "how many files are in there now" with the folder it had
// just filled named in that list. recent is newest first, folders ending in
// "/". named is what the last reply named that is here, when the last request
// changed nothing (a question such as "what's the biggest file here"): then
// "it" is the one thing that reply named, and with several named there is no
// note, rather than a guess (3B moved both files after "big.bin" was named).
func referenceNote(task string, recent, named []string) string {
	if m := thingWords.FindString(task); m != "" && !placeWords.MatchString(task) && len(named) > 0 {
		if len(named) == 1 {
			return fmt.Sprintf("(In this request, %q most likely means %s, which the last reply named.)", strings.ToLower(m), named[0])
		}
		return ""
	}
	if len(recent) == 0 {
		return ""
	}
	if m := placeWords.FindString(task); m != "" {
		for _, p := range recent {
			folder := p
			if !strings.HasSuffix(p, "/") {
				folder = filepath.Dir(p) + "/"
				if folder == "./" {
					continue
				}
			}
			return fmt.Sprintf("(In this request, %q most likely means %s, the folder this session changed most recently.)", strings.ToLower(m), folder)
		}
		return ""
	}
	if m := thingWords.FindString(task); m != "" {
		return fmt.Sprintf("(In this request, %q most likely means %s, what this session changed most recently.)", strings.ToLower(m), recent[0])
	}
	return ""
}

// --- refs: one thing ---

// itMoved is "it" as the thing a request moves or deletes. Not every "it":
// in "do it" or "fix it" it names no file (an existing test ran "do it" with
// two unrelated moves, and an earlier version of this check stopped one).
var itMoved = regexp.MustCompile(`(?i)\b(move|put|delete|remove|rm|mv|trash|rename|archive|get rid of|throw away|throw out|drop)\s+it\b`)

var pluralWords = regexp.MustCompile(`(?i)\b(them|those|these|they|all|both|every|each|and|files|folders|everything)\b`)

// removedPaths is what a step deletes or moves away, relative to wd.
func removedPaths(gd *gate.Decision, wd string) []string {
	if gd == nil || gd.Analysis == nil {
		return nil
	}
	var out []string
	for _, e := range gd.Analysis.Effects {
		if e.Kind != effects.Remove {
			continue
		}
		p := e.Path
		if r, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(r, "..") {
			p = r
		}
		out = append(out, p)
	}
	return out
}

// topLevel is paths with any that lie inside another dropped, sorted: a
// folder deleted with what is in it is one thing.
func topLevel(paths map[string]bool) []string {
	var out []string
	for p := range paths {
		inside := false
		for q := range paths {
			inside = inside || (q != p && strings.HasPrefix(p, q+"/"))
		}
		if !inside {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// oneThingNote: a request to move or delete "it" means one thing, so a
// task that would delete or move away two things over its steps has gone
// past it, whichever thing "it" is. Checked from the analysis before the step
// runs; "" when the request is not about one thing, the analysis has nothing,
// or the count stays at one.
func oneThingNote(task string, history []loopStep, gd *gate.Decision, wd string) string {
	if !itMoved.MatchString(task) || pluralWords.MatchString(task) {
		return ""
	}
	now := removedPaths(gd, wd)
	if len(now) == 0 {
		return ""
	}
	all := map[string]bool{}
	for _, h := range history {
		for _, p := range h.removed {
			all[p] = true
		}
	}
	for _, p := range now {
		all[p] = true
	}
	things := topLevel(all)
	if len(things) < 2 {
		return ""
	}
	return fmt.Sprintf("Not run: the request says \"it\", one thing, but this task would then have moved or deleted %d: %s. Act on the one thing the request means, and nothing else.", len(things), strings.Join(things, ", "))
}

// --- literal ---

// shellMarks are signs that text is shell, not prose; shellOption is an
// option word (-la, --all), unlike a dash between words.
var (
	shellMarks  = []string{"&&", "||", "|", ";", ">", "<", "$(", "*.", "./", "~/"}
	shellOption = regexp.MustCompile(`(^|\s)--?[A-Za-z]`)
)

// literalCommand reports whether the request is itself a shell command: it
// parses, every program it starts is a builtin or installed, and it has
// shell punctuation or an option, so "find my files" is not taken for one.
func literalCommand(task string) bool {
	t := strings.TrimSpace(task)
	marked := shellOption.MatchString(t)
	for _, m := range shellMarks {
		marked = marked || strings.Contains(t, m)
	}
	if !marked {
		return false
	}
	f, err := syntax.NewParser().Parse(strings.NewReader(t), "")
	if err != nil || len(f.Stmts) == 0 {
		return false
	}
	calls := 0
	ok := true
	syntax.Walk(f, func(n syntax.Node) bool {
		c, isCall := n.(*syntax.CallExpr)
		if !isCall || len(c.Args) == 0 {
			return true
		}
		calls++
		name := c.Args[0].Lit()
		if name == "" {
			ok = false
			return false
		}
		if !shellWords[name] {
			if _, err := exec.LookPath(name); err != nil {
				ok = false
			}
		}
		return true
	})
	return ok && calls > 0
}

// literalNote is said beside a request that is itself a command.
const literalNote = "(This request is written as a shell command. Propose it exactly as written, unless it cannot work here.)"
