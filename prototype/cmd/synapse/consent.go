package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"synapseos/internal/effects"
	"synapseos/internal/gate"
)

// consent is what a person is told before approving a command, in words that
// need no knowledge of the shell (docs/cli-breaking.md, round-1 findings
// atk1-03, atk1-05, atk1-06, atk2-04). It used to say "it can't be undone" for
// every confirmation, including deletions synapse saves to its trash and
// undoes exactly, and text mode and --json gave different reasons for the
// same command. Every interface now shows this one message.
type consent struct {
	does   []string // what the command does, one plain sentence each
	undo   string   // whether undo can take it back; always true to what is captured
	detail string   // the technical reason, for those who want it
}

// text renders the message for --json and the TUI, which show the command
// themselves.
func (c consent) text() string {
	var b strings.Builder
	for _, d := range c.does {
		b.WriteString(d + "\n")
	}
	b.WriteString(c.undo)
	if c.detail != "" {
		b.WriteString("\n(technical reason for asking: " + c.detail + ")")
	}
	return b.String()
}

// riskExtras is what the effect analysis does not model but a person must
// know before saying yes: the command uses administrator rights, or it
// reaches the network. Either one always asks, whatever the analysis says
// about files, because neither can be undone by capturing files.
type riskExtras struct {
	privileged, network bool
	// ownState: the command changes synapse's own undo history, trash or
	// backups (~/.synapse). Deleting them destroyed the very copies undo was
	// promising (findings r2-atk5-05, r2-atk5-06).
	ownState bool
	// emptiesBeforeReading: files a command truncates with > while also
	// reading them, as in `sort f > f`; the shell empties f first, so the
	// result is an empty file (finding r3-atk9-04).
	emptiesBeforeReading []string
	// appends: files written with >>, which keep what is there (r3-atk9-01).
	appends map[string]bool
}

var (
	privilegedPrograms = map[string]bool{"sudo": true, "doas": true, "pkexec": true, "su": true, "run0": true, "sudoedit": true}
	// Programs whose purpose is to talk to another machine.
	networkPrograms = map[string]bool{
		"curl": true, "wget": true, "ssh": true, "scp": true, "sftp": true, "nc": true, "ncat": true, "netcat": true,
		"telnet": true, "ftp": true, "lftp": true, "ping": true, "traceroute": true, "dig": true, "nslookup": true,
		"whois": true, "aria2c": true, "socat": true, "sendmail": true,
	}
	// Programs that reach the network only for some subcommands.
	networkSubcommands = map[string]map[string]bool{
		"git":     {"clone": true, "fetch": true, "pull": true, "push": true, "ls-remote": true, "submodule": true, "remote": true},
		"apt":     {"install": true, "update": true, "upgrade": true, "full-upgrade": true, "dist-upgrade": true, "download": true, "source": true},
		"apt-get": {"install": true, "update": true, "upgrade": true, "dist-upgrade": true, "download": true, "source": true},
		"pip":     {"install": true, "download": true}, "pip3": {"install": true, "download": true},
		"npm": {"install": true, "i": true, "publish": true, "update": true}, "npx": {"*": true},
		"yarn": {"add": true, "install": true}, "pnpm": {"add": true, "install": true},
		"cargo": {"install": true, "fetch": true, "publish": true}, "gem": {"install": true},
		"docker": {"pull": true, "push": true, "login": true, "run": true}, "podman": {"pull": true, "push": true, "login": true, "run": true},
		"snap": {"install": true, "refresh": true}, "flatpak": {"install": true, "update": true},
		"go": {"get": true, "install": true}, "ollama": {"pull": true, "push": true, "run": true},
	}
	wordRE = regexp.MustCompile(`[A-Za-z0-9_.+-]+`)
	// A URL, a bash network path, or a socket library in an interpreter
	// one-liner reaches the network whatever program carries it
	// (finding r3-atk10-01: python3 -c with requests.get asked, but did not
	// say data would leave the machine).
	urlRE         = regexp.MustCompile(`(?i)\b(https?|ftps?|sftp|ssh|git|wss?|rsync)://|/dev/(tcp|udp)/`)
	interpreterRE = regexp.MustCompile(`\b(python[0-9.]*|perl|ruby|node|php|lua)\b`)
	netLibraryRE  = regexp.MustCompile(`\b(socket|urllib|requests|http\.client|httplib|fetch|LWP|Net::|open-uri|XMLHttpRequest)\b`)
)

// detectRiskExtras reads every simple command in the line, including ones
// nested in pipelines, lists, substitutions and wrappers such as
// `env X=1 sudo …` or `xargs curl`. It over-asks rather than under-asks: any
// word naming a privileged or network program counts, and a line that does
// not parse is checked word by word.
func detectRiskExtras(cmd string) riskExtras {
	var r riskExtras
	check := func(words []string) {
		for i, w := range words {
			base := filepath.Base(w)
			if privilegedPrograms[base] {
				r.privileged = true
			}
			if networkPrograms[base] {
				r.network = true
			}
			if subs, ok := networkSubcommands[base]; ok {
				if subs["*"] {
					r.network = true
				}
				for _, a := range words[i+1:] {
					if subs[a] {
						r.network = true
					}
				}
			}
			if base == "rsync" {
				for _, a := range words[i+1:] {
					if strings.Contains(a, ":") {
						r.network = true // host:path or rsync://
					}
				}
			}
		}
	}
	if urlRE.MatchString(cmd) || (interpreterRE.MatchString(cmd) && netLibraryRE.MatchString(cmd)) {
		r.network = true
	}
	f, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
	if err != nil {
		check(wordRE.FindAllString(cmd, -1))
		return r
	}
	r.appends = map[string]bool{}
	syntax.Walk(f, func(n syntax.Node) bool {
		st, ok := n.(*syntax.Stmt)
		if !ok {
			return true
		}
		var args []string
		if call, ok := st.Cmd.(*syntax.CallExpr); ok {
			for _, w := range call.Args[min(1, len(call.Args)):] {
				args = append(args, literalWord(w))
			}
		}
		for _, rd := range st.Redirs {
			if rd.Word == nil {
				continue
			}
			target := literalWord(rd.Word)
			switch rd.Op {
			case syntax.AppOut, syntax.AppAll:
				r.appends[target] = true
			case syntax.RdrOut, syntax.ClbOut, syntax.RdrAll:
				for _, a := range args {
					if a != "" && a == target {
						r.emptiesBeforeReading = append(r.emptiesBeforeReading, target)
					}
				}
			}
		}
		return true
	})
	syntax.Walk(f, func(n syntax.Node) bool {
		if call, ok := n.(*syntax.CallExpr); ok {
			var words []string
			for _, w := range call.Args {
				words = append(words, literalWord(w))
			}
			check(words)
		}
		return true
	})
	// A command run through a string (bash -c '…', eval) is not parsed as
	// syntax above; its words are checked as text.
	if strings.Contains(cmd, "-c") || strings.Contains(cmd, "eval") {
		check(wordRE.FindAllString(cmd, -1))
	}
	return r
}

// literalWord is a word's value once the shell has removed its quotes and
// backslashes: "curl", 'cu'rl and \curl all run curl. syntax.Word.Lit is ""
// for any quoted or escaped word, so the network and administrator checks
// read past exactly the spellings that hide a program (ship review,
// 2026-10-27). A word with an expansion in it has no literal value and gives
// "": what it runs is decided at run time, and the analysis treats that as
// unresolved, which asks.
func literalWord(w *syntax.Word) string {
	var b strings.Builder
	var lit func(parts []syntax.WordPart) bool
	lit = func(parts []syntax.WordPart) bool {
		for _, p := range parts {
			switch x := p.(type) {
			case *syntax.Lit:
				v := x.Value
				for i := 0; i < len(v); i++ {
					if v[i] == '\\' && i+1 < len(v) {
						i++
					}
					b.WriteByte(v[i])
				}
			case *syntax.SglQuoted:
				b.WriteString(x.Value)
			case *syntax.DblQuoted:
				if !lit(x.Parts) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	if !lit(w.Parts) {
		return ""
	}
	return b.String()
}

// touchesOwnState reports whether cmd changes anything under ~/.synapse:
// any effect the analysis found there, or, since a path it cannot resolve
// may still land there, any mention of the folder in the command.
func touchesOwnState(cmd string, gd *gate.Decision) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return strings.Contains(cmd, ".synapse")
	}
	own := filepath.Join(home, ".synapse")
	if gd != nil && gd.Analysis != nil {
		for _, e := range gd.Analysis.Effects {
			if e.Path == own || strings.HasPrefix(e.Path, own+string(filepath.Separator)) || (e.Dir && strings.HasPrefix(own, e.Path+string(filepath.Separator))) {
				return true
			}
		}
	}
	return strings.Contains(cmd, ".synapse")
}

// describeEffects turns what the analysis found into plain sentences, naming
// at most three paths per sentence, relative to the working directory.
func describeEffects(a *effects.Analysis, wd string, appends map[string]bool) []string {
	if a == nil {
		return nil
	}
	group := map[string][]string{}
	var order []string
	add := func(key, path string) {
		if _, ok := group[key]; !ok {
			order = append(order, key)
		}
		group[key] = append(group[key], path)
	}
	rel := func(p string) string {
		name := p
		if r, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(r, "..") {
			if r == "." {
				return "this folder" // "the project at .." read as the parent folder (r2-atk6-06)
			}
			name = r
		}
		// A link is named with what it reaches: the file a write through it
		// changes is the target, not the link (round-3 observation).
		if li, err := os.Lstat(p); err == nil && li.Mode()&os.ModeSymlink != 0 {
			if t, err := os.Readlink(p); err == nil {
				name += " (a link to " + t + ")"
			}
		}
		return name
	}
	appended := func(p string) bool {
		for t := range appends {
			if t == p || filepath.Join(wd, t) == p {
				return true
			}
		}
		return false
	}
	for _, e := range a.Effects {
		switch {
		case e.Hint == "git-head":
			add("git", rel(e.Path))
		case e.Kind == effects.Remove && e.MovedTo != "":
			add("move", rel(e.Path)+" to "+rel(e.MovedTo))
		case e.Kind == effects.Remove:
			add("delete", rel(e.Path))
		case e.Kind == effects.Write && appended(e.Path):
			add("append", rel(e.Path))
		case e.Kind == effects.Write:
			add("write", rel(e.Path))
		case e.Kind == effects.Create:
			add("create", rel(e.Path))
		case e.Kind == effects.Meta:
			add("meta", rel(e.Path))
		}
	}
	phrase := map[string]string{
		"delete": "It deletes %s.",
		"move":   "It moves %s.",
		"write":  "It changes what is inside %s (what is there now gets replaced).",
		"append": "It adds to the end of %s (what is there now is kept).",
		"create": "It creates %s.",
		"meta":   "It changes who can open or change %s.",
		"git":    "It throws away unsaved changes in the project in %s.",
	}
	var out []string
	for _, k := range order {
		paths := dedupe(group[k])
		// Up to eight names: hiding the one that matters in "and 6 more"
		// let a person approve deleting the folder they asked to keep
		// (finding r2-atk6-04).
		list := strings.Join(paths, ", ")
		if len(paths) > 8 {
			list = strings.Join(paths[:8], ", ") + fmt.Sprintf(" and %d more", len(paths)-8)
		}
		out = append(out, fmt.Sprintf(phrase[k], list))
	}
	if len(a.States) > 0 {
		out = append(out, "It installs, removes or changes software or a system service.")
	}
	return out
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

// listReasonPlain says in everyday words what a pattern-list rule means.
func listReasonPlain(reason string) string {
	r := strings.ToLower(reason)
	switch {
	case strings.HasPrefix(r, "shred"):
		return "It overwrites a file's contents so they can never be read again, and may delete the file." // r2-atk5-09
	case strings.HasPrefix(r, "rm ") || strings.Contains(r, "delete") || strings.Contains(r, "unlink"):
		return "It deletes files."
	case strings.Contains(r, "redirect") || strings.Contains(r, "overwrite") || strings.Contains(r, "truncate") || strings.Contains(r, "tee "):
		return "It replaces what is inside a file."
	case strings.Contains(r, "chmod") || strings.Contains(r, "chown") || strings.Contains(r, "permission"):
		return "It changes who can open or change files."
	case strings.Contains(r, "git"):
		return "It changes or throws away work in a git project."
	case strings.Contains(r, "kill"):
		return "It stops running programs."
	case strings.Contains(r, "executes arbitrary") || strings.Contains(r, "shell interpreter"):
		return "It downloads or decodes code and runs it without showing it to you first."
	}
	return "It can change or delete things on this computer."
}

// buildConsent assembles the message. unprotected is the gate's own answer to
// whether a capture backs this command; the undo sentence follows it exactly,
// so the message cannot promise more than the safety net delivers.
//
// recording is false when this session keeps no undo journal at all; then no
// command can be undone, whatever its capture would have been.
func buildConsent(listIrreversible bool, listReason string, gd *gate.Decision, extras riskExtras, unprotected, recording bool, wd string) consent {
	var c consent
	confident := gd != nil && gd.Confident
	for _, f := range extras.emptiesBeforeReading {
		c.does = append(c.does, fmt.Sprintf("WARNING: it empties %s before reading it, so %s will end up empty. This is almost certainly not what you want.", f, f))
	}
	if confident {
		c.does = append(c.does, describeEffects(gd.Analysis, wd, extras.appends)...)
	}
	if len(c.does) == 0 {
		switch {
		case listIrreversible && gd != nil && !gd.Confident:
			c.does = []string{listReasonPlain(listReason), "I can't tell exactly which files it affects."}
		case listIrreversible:
			c.does = []string{listReasonPlain(listReason)}
		case gd != nil && !gd.Confident:
			c.does = []string{"I can't tell for certain what this would change, so I'm checking with you first."}
		}
	}
	// Whether any file change was described, counted before the network and
	// administrator sentences: "still copied first" is about files, and
	// sudo curl changes none (ship review, 2026-10-27).
	fileChanges := confident && len(describeEffects(gd.Analysis, wd, extras.appends)) > 0
	if extras.privileged {
		c.does = append(c.does, "It uses administrator (root) rights, so it can change anything on this computer, including system settings and other people's files.")
	}
	if extras.network {
		c.does = append(c.does, "It connects to the internet or to another computer, and may send or download information.")
	}
	if extras.ownState {
		c.does = append(c.does, "It changes synapse's own undo history and saved copies (in ~/.synapse). After that, earlier steps can no longer be undone.")
	}
	switch {
	case extras.ownState:
		c.undo = "Undo cannot take this back: it removes the copies undo would use."
	case extras.network || extras.privileged:
		c.undo = "Undo cannot take this back: synapse can't recall what is sent over the network or undo changes made with administrator rights."
		if !unprotected && fileChanges {
			c.undo += " Changes to your own files are still copied first."
		}
	case !recording:
		c.undo = "Undo is not available right now (synapse is not keeping an undo history), so this cannot be taken back."
	case unprotected:
		c.undo = "If it goes wrong, undo will NOT be able to bring things back."
	default:
		c.undo = "If you change your mind, `synapse undo` can put things back: I keep a copy first."
		if deletes(gd) {
			// "Free up space" requests: deleted files stay in synapse's trash
			// so undo can bring them back, which frees nothing yet.
			c.undo += " (Because of that copy, deleting does not free disk space yet.)"
		}
	}
	if listIrreversible {
		c.detail = listReason
	} else if gd != nil {
		c.detail = firstReason(gd)
	}
	// The analysis calls every write an "overwrite"; for an append that is
	// false (finding r3-atk9-01).
	if rest, ok := strings.CutPrefix(c.detail, "overwrite "); ok {
		for t := range extras.appends {
			if t == rest || filepath.Join(wd, t) == rest {
				c.detail = "appends to " + rest
			}
		}
	}
	return c
}

// deletes reports whether the analysis found a deletion (not a move).
func deletes(gd *gate.Decision) bool {
	if gd == nil || gd.Analysis == nil {
		return false
	}
	for _, e := range gd.Analysis.Effects {
		if e.Kind == effects.Remove && e.MovedTo == "" && e.Hint == "" {
			return true
		}
	}
	return false
}
