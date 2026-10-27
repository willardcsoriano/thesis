package main

import (
	"bytes"
	"compress/gzip"
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

	"synapseos/internal/executor"
	"synapseos/internal/ollama"
)

func TestParseAids(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
		err  bool
	}{
		{"", allAids, false},
		{"all", allAids, false},
		{" ALL ", allAids, false},
		{"none", nil, false},
		{"peek", []string{aidPeek}, false},
		{"peek, refs", []string{aidPeek, aidRefs}, false},
		{"peek,bogus", nil, true},
		{",", nil, true},
	} {
		on, err := parseAids(c.in)
		if (err != nil) != c.err {
			t.Errorf("parseAids(%q) error = %v, want error %v", c.in, err, c.err)
			continue
		}
		if c.err {
			continue
		}
		if len(on) != len(c.want) {
			t.Errorf("parseAids(%q) = %v, want %v", c.in, on, c.want)
		}
		for _, a := range c.want {
			if !on[a] {
				t.Errorf("parseAids(%q) lacks %s", c.in, a)
			}
		}
	}
}

func TestPeekShowsNamedTextFilesOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "data.csv"), "name,score\nann,10\nbob,20\ncat,30\ndan,40\neve,50\nfay,60\n")
	writeFile(t, filepath.Join(dir, "blob.bin"), "a\x00b")
	writeFile(t, filepath.Join(dir, "wide.txt"), strings.Repeat("w", 500)+"\n")
	writeFile(t, filepath.Join(dir, "datafile.csv"), "should not match")
	got := peekSection("sum the scores in data.csv and look at blob.bin and wide.txt please", dir)
	for _, want := range []string{"== data.csv", "name,score", "ann,10", "== blob.bin: not text", "== wide.txt", "…"} {
		if !strings.Contains(got, want) {
			t.Errorf("peek lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "fay,60") {
		t.Error("peek shows more than its line limit")
	}
	if strings.Contains(got, "should not match") || strings.Contains(got, "\x00") {
		t.Errorf("peek shows a file the request did not name, or raw binary:\n%q", got)
	}
	if peekSection("how many files are here", dir) != "" {
		t.Error("peek shows files when the request names none")
	}
	if peekSection("look at data", dir) != "" {
		t.Error("peek matched part of a name")
	}
	os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
	if got := peekSection("what is in notes", dir); got != "" {
		t.Errorf("peek showed a folder: %q", got)
	}
}

func TestMissingPrograms(t *testing.T) {
	for cmd, want := range map[string]string{
		"ls -la":                                     "",
		"cd x && echo hi":                            "",
		"f() { definitely-missing-a; }; true":        "definitely-missing-a",
		"g() { echo; }; g":                           "",
		"$CMD arg":                                   "",
		"definitely-missing-b x | wc -l":             "definitely-missing-b",
		"echo $(definitely-missing-c)":               "definitely-missing-c",
		"./no-such-script.sh":                        "./no-such-script.sh",
		"definitely-missing-d; definitely-missing-a": "definitely-missing-a definitely-missing-d",
		"echo 'unterminated":                         "",
	} {
		if got := strings.Join(missingPrograms(cmd), " "); got != want {
			t.Errorf("missingPrograms(%q) = %q, want %q", cmd, got, want)
		}
	}
}

// recordingModel is a fake Ollama that records every step prompt it receives.
type recordingModel struct {
	srv     *httptest.Server
	mu      sync.Mutex
	prompts []string
	reply   string // the answer to "reporting the outcome"; "fake answer" if empty
}

func newRecordingModel(t *testing.T, decisions ...stepDecision) *recordingModel {
	t.Helper()
	m := &recordingModel{}
	step := 0
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("You are reporting the outcome of a task")) {
			m.mu.Lock()
			reply := m.reply
			m.mu.Unlock()
			if reply == "" {
				reply = "fake answer"
			}
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: reply, Done: true})
			return
		}
		var req struct{ Prompt string }
		json.Unmarshal(body, &req)
		m.mu.Lock()
		m.prompts = append(m.prompts, req.Prompt)
		d := stepDecision{Action: "done"}
		if step < len(decisions) {
			d = decisions[step]
		}
		step++
		m.mu.Unlock()
		raw, _ := json.Marshal(d)
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: string(raw), Done: true})
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *recordingModel) all() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.Join(m.prompts, "\n----\n")
}

// Each aid reaches the model when on and is gone when SYNAPSE_AIDS=none, so a
// with/without comparison measures the aid and nothing else.
func TestAidsReachThePromptAndSwitchOff(t *testing.T) {
	for _, aids := range []string{"all", "none"} {
		t.Run(aids, func(t *testing.T) {
			dir, home := t.TempDir(), t.TempDir()
			writeFile(t, filepath.Join(dir, "data.csv"), "name,score\nann,10\n")
			writeFile(t, filepath.Join(dir, "big.bin"), "x")
			// peek, one-shot
			m := newRecordingModel(t, runStep("awk -F, 'NR>1{s+=$2}END{print s}' data.csv"))
			execSynapse(t, synapseRun{args: []string{"run", "add up the scores in data.csv"}, env: []string{"SYNAPSE_OLLAMA=" + m.srv.URL, "SYNAPSE_AIDS=" + aids}, dir: dir, home: home})
			if got := strings.Contains(m.all(), "name,score"); got != (aids == "all") {
				t.Errorf("peek in prompt = %v with SYNAPSE_AIDS=%s", got, aids)
			}
			// refs, in a session: the second request's prompt names what the first made
			r := newRecordingModel(t, runStep("mkdir old && mv big.bin old/"), stepDecision{Action: "done"}, runStep("ls old | wc -l"))
			execSynapse(t, synapseRun{args: []string{"repl"}, env: []string{"SYNAPSE_OLLAMA=" + r.srv.URL, "SYNAPSE_AIDS=" + aids}, dir: dir, home: home,
				stdin: "move big.bin into a folder called old\nhow many files are in there now\n"})
			if got := strings.Contains(r.all(), "Most recently created or changed in this session"); got != (aids == "all") {
				t.Errorf("refs in prompt = %v with SYNAPSE_AIDS=%s:\n%s", got, aids, r.all())
			}
			if aids == "all" && !strings.Contains(r.all(), "old/") {
				t.Errorf("refs does not name the folder the first request made:\n%s", r.all())
			}
			if got := strings.Contains(r.all(), `"in there" most likely means old/`); got != (aids == "all") {
				t.Errorf("refs note beside the request = %v with SYNAPSE_AIDS=%s:\n%s", got, aids, r.all())
			}
			// literal, one-shot
			l := newRecordingModel(t, runStep("du -sh ."))
			execSynapse(t, synapseRun{args: []string{"run", "du -sh ."}, env: []string{"SYNAPSE_OLLAMA=" + l.srv.URL, "SYNAPSE_AIDS=" + aids}, dir: dir, home: home})
			if got := strings.Contains(l.all(), literalNote); got != (aids == "all") {
				t.Errorf("literal note in prompt = %v with SYNAPSE_AIDS=%s", got, aids)
			}
		})
	}
}

// A program that is not installed is not run, the model is told, and nothing
// is asked; with the aid off the command goes to the gate as before.
func TestToolsAidStopsUninstalledPrograms(t *testing.T) {
	for _, aids := range []string{"all", "none"} {
		t.Run(aids, func(t *testing.T) {
			m := newFakeModel(t, runStep("definitely-not-installed-tool a.pdf b.pdf"))
			r := execSynapse(t, synapseRun{args: []string{"run", "--json", "combine the pdfs"}, env: append(m.env(), "SYNAPSE_AIDS="+aids)})
			recs := readJSONLines(t, r.stdout)
			var notRun, asked bool
			for _, rec := range recs {
				notRun = notRun || (rec.Type == "result" && rec.NotRun && strings.Contains(rec.Stderr, "not installed"))
				asked = asked || rec.Type == "approval"
			}
			if aids == "all" && (!notRun || asked) {
				t.Errorf("with the aid: notRun=%v asked=%v, want the command refused without asking:\n%s", notRun, asked, r.stdout)
			}
			if aids == "none" && notRun {
				t.Errorf("with SYNAPSE_AIDS=none the aid still acted:\n%s", r.stdout)
			}
		})
	}
}

func TestPeekSkipsWhatItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "secret.txt")
	writeFile(t, p, "hidden")
	os.Chmod(p, 0)
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	if got := peekSection("read secret.txt", dir); strings.Contains(got, "hidden") {
		t.Fatalf("peek showed an unreadable file: %q", got)
	}
}

func TestLintCommand(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "data.csv"), "name,score\nann,10\nbob,20\n")
	writeFile(t, filepath.Join(dir, "semi.csv"), "a;1\nb;2\n")
	writeFile(t, filepath.Join(dir, "spaced.txt"), "ann 10\nbob 20\n")
	writeFile(t, filepath.Join(dir, "names.csv"), "ann lee,10\nbob ray,20\n")
	writeFile(t, filepath.Join(dir, "plain.txt"), "one\ntwo\n")
	writeFile(t, filepath.Join(dir, "blob.bin"), "a,\x00,b\n")
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	writeFile(t, filepath.Join(dir, "sub", "a.txt"), "x")
	writeFile(t, filepath.Join(dir, "sub", "b.txt"), "x")
	for cmd, want := range map[string]string{
		// awk's later fields on a file without blanks
		`awk '{s+=$2}END{print s}' data.csv`:            `-F','`,
		`awk 'NR>1 {s += $2} END {print s}' data.csv`:   `-F','`,
		`gawk '{print $3}' semi.csv`:                    `-F';'`,
		`awk -F, '{s+=$2}END{print s}' data.csv`:        "",
		`awk -F ',' '{print $2}' data.csv`:              "",
		`awk -v FS=, '{print $2}' data.csv`:             "",
		`awk -vFS=, '{print $2}' data.csv`:              "", // attached -v sets the separator too
		`awk 'BEGIN{FS=","}{print $2}' data.csv`:        "",
		`awk '{print $2}' FS=, data.csv`:                "",
		`awk -f prog.awk data.csv`:                      "",
		`awk '{s+=$2}END{print s}' spaced.txt`:          "", // awk's default is right here
		`awk '{print $2}' names.csv`:                    "", // $2 is "lee,10": not empty, not judged
		`sort -k5 data.csv | awk '{print $2}' data.csv`: "-F','",
		`ls -l | sort --key=5`:                          "",
		`awk '{print $1}' data.csv`:                     "", // $1 is the whole line: right
		`awk '{print $2}' plain.txt`:                    "", // no separator to suggest
		`awk '{print $2}' missing.csv`:                  "",
		`awk '{print $2}' blob.bin`:                     "",
		`awk '{print $2}' *.csv`:                        "",
		`awk "{print $2}" data.csv`:                     "",      // $2 expanded by the shell: not judged
		`cat data.csv | awk '{s+=$2}END{print s}'`:      `-F','`, // awk reads what cat names
		`awk '{s+=$2}END{print s}' < data.csv`:          `-F','`,
		`cat -n data.csv | awk '{print $2}'`:            "", // cat -n changes the lines
		`cat data.csv | awk '{print $2}' spaced.txt`:    "", // awk reads its own file
		`cat data.csv | awk -F, '{print $2}'`:           "",
		`sort data.csv | awk '{print $2}'`:              "", // sorted lines: not judged
		`awk '{print $2}' < $f`:                         "",
		`awk '{print $2}' > data.csv`:                   "", // written, not read
		`awk '{print $2}' spaced.txt data.csv`:          `lines of data.csv`,
		`awk '{print $2}' missing.csv data.csv`:         `lines of data.csv`,
		// ls -l's total line, counted
		`ls -l | wc -l`:            `ls -A | wc -l`,
		`ls -l sub | wc -l`:        `ls -A sub | wc -l`,
		`ls -la sub/ | wc -l`:      "total",
		`ls -ld sub | wc -l`:       "",
		`ls -l sub/a.txt | wc -l`:  "", // files alone: no total line
		`ls -l *.txt | wc -l`:      "",
		`ls -l nowhere | wc -l`:    "",
		`ls | wc -l`:               "",
		`ls -A sub | wc -l`:        "",
		`ls -l | wc -c`:            "",
		`ls -l | grep txt | wc -l`: "",
		`ls -l | grep -c .`:        "total",
		`ls -l | grep -c '^-'`:     "", // counts files: the total line does not match
		`ls -l | grep -c txt`:      "",
		`ls -l | grep -c -v '^d'`:  "total", // the total line is not a directory, so it is counted
		`ls -l | grep -ci TOTAL`:   "total",
		`ls -l | grep -cF total`:   "total",
		`ls -l | grep -c -f pats`:  "", // patterns from a file: not judged
		`ls -l | grep -v x`:        "",
		`ls -ld sub | grep -c .`:   "",
		// du's own total, ranked first
		`du -ah --max-depth=1 | sort -hr | head -n 1`: "ls -S | head -n 1",
		`du -h . | sort -rh | head -1`:                "ls -S",
		`du -a | sort -n | tail -n 1`:                 "ls -S",
		`du -a | sort -n | tail -n1`:                  "ls -S",
		`du -ah | sort -hr | head -n 3`:               "",
		`du -sh * | sort -hr | head -n 1`:             "",
		`du -sh . | sort -hr | head -n 1`:             "",
		`du -ach | sort -hr | head -n 1`:              "",
		`du -ah sub | sort -hr | head -n 1`:           "",
		`du -a | sort -hr | tail -n 1`:                "",
		`du -a | head -n 1`:                           "",
		`du -a | grep -v '\.$' | sort -rh | head -1`:  "", // the filter drops the total line
		`sort -hr x | head -n 1 | du -a`:              "",
		// ls -l lines sorted by their permissions
		`ls -lh sub/a.txt sub/b.txt | sort -rh | head -n 1`: "ls -S | head -n 1",
		`ls -l | sort -n`:                        "ls -S | head -n 1",
		`ls -l | sort -k5,5n`:                    "",
		`ls -l | sort -nk5`:                      "",
		`ls -lS | head -n 1`:                     "first line ls -l prints for a folder",
		`ls -lhS sub | head -1`:                  "total",
		`ls -lS | head -n 2`:                     "",
		`ls -lSd sub | head -n 1`:                "",
		`ls -lS sub/a.txt sub/b.txt | head -n 1`: "",
		`ls -S | head -n 1`:                      "",
		`ls | sort`:                              "",
		`ls --format=long | sort`:                "",
		// not judged
		`echo 'unterminated`: "",
		``:                   "",
	} {
		got := lintCommand(cmd, dir)
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("lintCommand(%q) = %q, want one containing %q", cmd, got, want)
		}
		if got != "" && !strings.HasPrefix(got, "Not run: ") {
			t.Errorf("lintCommand(%q) = %q, does not say it was not run", cmd, got)
		}
	}
	// Two problems in one line are both named, once each.
	both := lintCommand(`ls -l | wc -l; ls -l | wc -l; awk '{s+=$2}END{print s}' data.csv; awk '{print $2}' data.csv`, dir)
	if strings.Count(both, "total") != 1 || strings.Count(both, "-F','") != 1 {
		t.Errorf("two problems: %q", both)
	}
}

type refCase struct {
	task          string
	recent, named []string
	want          string
}

func TestReferenceNote(t *testing.T) {
	cases := []refCase{
		{"how many files are in there now", []string{"stuff/two.txt", "stuff/one.txt", "stuff/"}, nil, `"in there" most likely means stuff/`},
		{"put the two text files in it", []string{"stuff/"}, nil, `"in it" most likely means stuff/`},
		{"move it into a folder called old", []string{"old/big.bin"}, nil, `"it" most likely means old/big.bin`},
		{"what's in there", []string{"notes.txt", "a/b/c.txt"}, nil, `means a/b/`}, // a top-level file is no folder
		{"what's in there", []string{"notes.txt"}, nil, ""},
		{"how many files are there", []string{"stuff/"}, nil, ""}, // asks what exists, here
		{"is there a file called x", []string{"stuff/"}, nil, ""},
		{"delete it", nil, nil, ""},
		{"list the files", []string{"stuff/"}, nil, ""},
		{"edit the item list", []string{"stuff/"}, nil, ""}, // "item" is not "it"
		// After a request that changed nothing, "it" is what its reply named.
		{"move it into a folder called old", nil, []string{"big.bin"}, `"it" most likely means big.bin, which the last reply named`},
		{"move it into a folder called old", []string{"stuff/"}, []string{"big.bin"}, `means big.bin`},
		{"move it into a folder called old", []string{"stuff/"}, []string{"big.bin", "small.txt"}, ""}, // two named: no guess
		{"how many files are in it", []string{"stuff/"}, []string{"big.bin"}, `"in it" most likely means stuff/`},
	}
	for _, c := range cases {
		got := referenceNote(c.task, c.recent, c.named)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("referenceNote(%q, %v, %v) = %q, want %q", c.task, c.recent, c.named, got, c.want)
		}
	}
}

func TestLiteralCommand(t *testing.T) {
	for task, want := range map[string]bool{
		"mkdir -p logs && mv *.log logs/":            true,
		"du -sh .":                                   true,
		"awk -F, 'NR>1{s+=$2}END{print s}' data.csv": true,
		"ls -la":                true,
		"find my files":         false, // no shell punctuation or option
		"find files - big ones": false, // a dash between words is prose
		"tidy the log files away into their own folder": false,
		"how many files are there?":                     false,
		"definitely-missing-x -a":                       false,
		"echo 'unterminated":                            false,
		"$CMD -a":                                       false,
		"":                                              false,
	} {
		if got := literalCommand(task); got != want {
			t.Errorf("literalCommand(%q) = %v, want %v", task, got, want)
		}
	}
}

func TestSimilarPrograms(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"pdfunite", "pdfinfo", "qpdf", "unrelated"} {
		writeFile(t, filepath.Join(bin, name), "#!/bin/sh\n")
		os.Chmod(filepath.Join(bin, name), 0o755)
	}
	writeFile(t, filepath.Join(bin, "pdfnotexec"), "x") // not executable
	os.MkdirAll(filepath.Join(bin, "pdfdir"), 0o755)    // not a file
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Join(bin, "missing-dir"))
	if got := strings.Join(similarPrograms([]string{"pdftk"}, "pdftk a b cat output c"), " "); got != "pdfinfo pdfunite" {
		t.Errorf("by name: %q", got)
	}
	if got := strings.Join(similarPrograms([]string{"zzz"}, "zzz a.pdf b.pdf"), " "); got != "pdfinfo pdfunite qpdf" {
		t.Errorf("by file kind: %q", got)
	}
	if got := similarPrograms([]string{"zz"}, "zz a b"); got != nil {
		t.Errorf("no stem, still listed: %v", got)
	}
	for i := 0; i < similarMax+5; i++ {
		writeFile(t, filepath.Join(bin, fmt.Sprintf("pdfx%02d", i)), "")
		os.Chmod(filepath.Join(bin, fmt.Sprintf("pdfx%02d", i)), 0o755)
	}
	got := similarPrograms([]string{"pdftk"}, "pdftk")
	if len(got) != similarMax+1 || got[similarMax] != "and 7 more" {
		t.Errorf("over the cap: %d names, last %q", len(got), got[len(got)-1])
	}
	if note := missingProgramsNote([]string{"pdftk"}, "pdftk"); !strings.Contains(note, "pdfunite") {
		t.Errorf("note lacks the similar names: %q", note)
	}
}

// The lint aid stops awk's $2 on a CSV before it runs and says what works;
// with it off, the command runs.
func TestLintAidStopsAWrongResult(t *testing.T) {
	for _, aids := range []string{"all", "none"} {
		t.Run(aids, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "data.csv"), "name,score\nann,10\nbob,20\n")
			m := newFakeModel(t, runStep("awk '{s+=$2}END{print s}' data.csv"))
			r := execSynapse(t, synapseRun{args: []string{"run", "--json", "add up the scores in data.csv"}, env: append(m.env(), "SYNAPSE_AIDS="+aids), dir: dir})
			var stopped, ran bool
			for _, rec := range readJSONLines(t, r.stdout) {
				if rec.Type == "result" {
					stopped = stopped || (rec.NotRun && strings.Contains(rec.Stderr, "-F','"))
					ran = ran || !rec.NotRun
				}
			}
			if aids == "all" && (!stopped || ran) {
				t.Errorf("with the aid: stopped=%v ran=%v:\n%s", stopped, ran, r.stdout)
			}
			if aids == "none" && (stopped || !ran) {
				t.Errorf("with SYNAPSE_AIDS=none: stopped=%v ran=%v:\n%s", stopped, ran, r.stdout)
			}
		})
	}
}

func TestManDescription(t *testing.T) {
	dir := t.TempDir()
	old := manDirs
	manDirs = []string{filepath.Join(dir, "missing"), dir}
	t.Cleanup(func() { manDirs = old })
	os.MkdirAll(filepath.Join(dir, "man1"), 0o755)
	os.MkdirAll(filepath.Join(dir, "man8"), 0o755)
	var gz bytes.Buffer
	z := gzip.NewWriter(&gz)
	z.Write([]byte(".TH pdfunite 1\n.SH NAME\npdfunite \\- Portable Document Format (PDF) page merger\n.SH SYNOPSIS\n"))
	z.Close()
	os.WriteFile(filepath.Join(dir, "man1", "pdfunite.1.gz"), gz.Bytes(), 0o644)
	writeFile(t, filepath.Join(dir, "man8", "tool.8"), ".SH \"NAME\"\n.PP\n\\fBtool\\fR \\- does \\fIthings\\fP\n")
	writeFile(t, filepath.Join(dir, "man1", "nodash.1"), ".SH NAME\nnodash\n")
	writeFile(t, filepath.Join(dir, "man1", "noname.1"), ".SH SYNOPSIS\nnoname - x\n")
	writeFile(t, filepath.Join(dir, "man1", "broken.1.gz"), "not gzip")
	for name, want := range map[string]string{
		"pdfunite": "Portable Document Format (PDF) page merger",
		"tool":     "does things",
		"nodash":   "",
		"noname":   "",
		"broken":   "",
		"absent":   "",
	} {
		if got := manDescription(name); got != want {
			t.Errorf("manDescription(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReaskAfterMissing(t *testing.T) {
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "pdfunite"), "#!/bin/sh\n")
	os.Chmod(filepath.Join(bin, "pdfunite"), 0o755)
	t.Setenv("PATH", bin)
	man := t.TempDir()
	old := manDirs
	manDirs = []string{man}
	t.Cleanup(func() { manDirs = old })
	os.MkdirAll(filepath.Join(man, "man1"), 0o755)
	writeFile(t, filepath.Join(man, "man1", "pdfunite.1"), ".SH NAME\npdfunite \\- page merger\n")
	refused := loopStep{command: "pdftk a.pdf b.pdf cat output c.pdf", result: executor.Result{Stderr: missingProgramsNote([]string{"pdftk"}, "pdftk a.pdf"), ExitCode: 127}}
	got := reaskAfterMissing("put my pdfs together", []loopStep{refused})
	for _, want := range []string{"put my pdfs together\n", "- pdfunite: page merger", "Answer UNSUPPORTED only if none can do it."} {
		if !strings.Contains(got, want) {
			t.Errorf("re-ask lacks %q:\n%s", want, got)
		}
	}
	failed := loopStep{command: "ls nowhere", result: executor.Result{Stderr: "ls: cannot access", ExitCode: 2}}
	for _, h := range [][]loopStep{nil, {failed}, {refused, failed}} {
		if got := reaskAfterMissing("x", h); got != "" {
			t.Errorf("re-ask where the last step was no refusal: %q", got)
		}
	}
}

// Given up straight after a refusal, the model is asked once more, and only
// once; with the aid off it is not asked again.
func TestToolsAidAsksOnceMoreAfterGivingUp(t *testing.T) {
	for _, aids := range []string{"all", "none"} {
		t.Run(aids, func(t *testing.T) {
			m := newRecordingModel(t,
				runStep("definitely-not-installed-tool a.pdf b.pdf"),
				stepDecision{Action: "unsupported"},
				stepDecision{Action: "unsupported"},
				stepDecision{Action: "unsupported"})
			r := execSynapse(t, synapseRun{args: []string{"run", "--json", "combine the pdfs"}, env: []string{"SYNAPSE_OLLAMA=" + m.srv.URL, "SYNAPSE_AIDS=" + aids}})
			asked := strings.Count(m.all(), "Answer UNSUPPORTED only if none can do it.")
			if want := map[string]int{"all": 1, "none": 0}[aids]; asked != want {
				t.Errorf("asked again %d times with SYNAPSE_AIDS=%s, want %d", asked, aids, want)
			}
			// With the aid off the command goes to the gate, which asks, and
			// nobody answers here; with it on, the task ends given up.
			if aids == "all" && r.code != exitFailed {
				t.Errorf("exit %d, want %d (gave up):\n%s", r.code, exitFailed, r.stdout)
			}
		})
	}
}

// What a reply named is what "it" means in the next request, when that
// request follows one that changed nothing; a reply naming two things gives no
// note, and with the aid off there is none.
func TestRefsRemembersWhatTheReplyNamed(t *testing.T) {
	for _, c := range []struct {
		aids, first, reply, want string
	}{
		{"all", "ls -S", "The biggest file here is big.bin.", `"it" most likely means big.bin, which the last reply named`},
		{"none", "ls -S", "The biggest file here is big.bin.", ""},
		{"all", "ls -S", "big.bin and small.txt are both here.", ""},
		// The first request changed something: "it" is that, not the reply's.
		{"all", "touch new.txt", "Made new.txt; big.bin is here too.", `"it" most likely means new.txt, what this session changed`},
	} {
		dir, home := t.TempDir(), t.TempDir()
		writeFile(t, filepath.Join(dir, "big.bin"), strings.Repeat("x", 4096))
		writeFile(t, filepath.Join(dir, "small.txt"), "x")
		m := newRecordingModel(t, runStep(c.first), stepDecision{Action: "done"}, runStep("true"))
		m.reply = c.reply
		execSynapse(t, synapseRun{args: []string{"repl"}, env: []string{"SYNAPSE_OLLAMA=" + m.srv.URL, "SYNAPSE_AIDS=" + c.aids}, dir: dir, home: home,
			stdin: "what's the biggest file here\nmove it into a folder called old\n"})
		all := m.all()
		second := all[strings.LastIndex(all, "Task: move it"):]
		if c.want == "" && strings.Contains(second, "most likely means") || c.want != "" && !strings.Contains(second, c.want) {
			t.Errorf("SYNAPSE_AIDS=%s, %q then reply %q: want %q in:\n%s", c.aids, c.first, c.reply, c.want, second)
		}
	}
}

// A request about "it" may move or delete one thing: the step that would make
// it two is not run, whether in one command or over two; a folder with its
// contents is one thing; a request about several things, or the aid off, is
// not held to one.
func TestRefsHoldsItToOneThing(t *testing.T) {
	for _, c := range []struct {
		name, aids, task string
		steps            []string
		refused          string // the command not run, or "" for none
	}{
		{"two in one command", "all", "move it into a folder called old", []string{"mkdir old && mv big.bin small.txt old"}, "mkdir old && mv big.bin small.txt old"},
		{"two over two steps", "all", "move it into a folder called old", []string{"mkdir old && mv big.bin old", "mv small.txt old"}, "mv small.txt old"},
		{"one thing", "all", "move it into a folder called old", []string{"mkdir old && mv big.bin old"}, ""},
		{"a folder and its contents", "all", "delete it", []string{"rm -r box"}, ""},
		{"several asked for", "all", "move them into a folder called old", []string{"mkdir old && mv big.bin small.txt old"}, ""},
		{"and names two", "all", "move it and the text file into old", []string{"mkdir old && mv big.bin small.txt old"}, ""},
		{"in it is a place", "all", "put the pair in it", []string{"mv big.bin small.txt box/"}, ""},
		{"no it at all", "all", "tidy up here", []string{"mkdir old && mv big.bin small.txt old"}, ""},
		{"do it names nothing", "all", "do it", []string{"mkdir old && mv big.bin small.txt old"}, ""},
		{"it, to a place", "all", "move it in there", []string{"mv big.bin small.txt box/"}, "mv big.bin small.txt box/"},
		{"get rid of it", "all", "get rid of it", []string{"rm big.bin small.txt"}, "rm big.bin small.txt"},
		{"aid off", "none", "move it into a folder called old", []string{"mkdir old && mv big.bin small.txt old"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "big.bin"), "x")
			writeFile(t, filepath.Join(dir, "small.txt"), "x")
			os.MkdirAll(filepath.Join(dir, "box"), 0o755)
			writeFile(t, filepath.Join(dir, "box", "a"), "x")
			writeFile(t, filepath.Join(dir, "box", "b"), "x")
			var ds []stepDecision
			for _, s := range c.steps {
				ds = append(ds, runStep(s))
			}
			m := newFakeModel(t, append(ds, stepDecision{Action: "done"})...)
			r := execSynapse(t, synapseRun{args: []string{"run", "--json", c.task}, env: append(m.env(), "SYNAPSE_AIDS="+c.aids), dir: dir, stdin: "y\ny\ny\n"})
			var refused []string
			for _, rec := range readJSONLines(t, r.stdout) {
				if rec.Type == "result" && rec.NotRun && strings.Contains(rec.Stderr, `says "it", one thing`) {
					refused = append(refused, rec.Command)
				}
			}
			want := []string{}
			if c.refused != "" {
				want = []string{c.refused}
			}
			if strings.Join(refused, "|") != strings.Join(want, "|") {
				t.Errorf("refused %q, want %q:\n%s", refused, want, r.stdout)
			}
			// What was refused did not happen.
			if c.refused != "" && !exists(dir, "small.txt") {
				t.Error("small.txt was moved although the step was refused")
			}
		})
	}
}
