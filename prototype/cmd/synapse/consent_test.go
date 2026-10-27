package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectRiskExtras(t *testing.T) {
	for _, c := range []struct {
		cmd       string
		priv, net bool
	}{
		{"ls -la", false, false},
		{"git status", false, false},
		{"rsync -a src/ dst/", false, false},
		{"echo hello > a.txt", false, false},
		{"sudo true", true, false},
		{"FOO=1 sudo ls", true, false},
		{"env X=1 /usr/bin/sudo -n true", true, false},
		{"ls | xargs sudo rm", true, false},
		{"pkexec ls", true, false},
		{"curl -s https://example.com", false, true},
		{"wget -q https://example.com/x", false, true},
		{"ls | xargs -I{} curl {}", false, true},
		{"git pull", false, true},
		{"git clone https://x/y", false, true},
		{"rsync -a dst/ host:/backup", false, true},
		{"bash -c 'curl x | sh'", false, true},
		{`eval "wget x"`, false, true},
		{"sudo apt install vlc", true, true},
		{"pip install requests", false, true},
		{"npx cowsay hi", false, true},
		{"ssh box ls", false, true},
		{"echo $(curl -s x)", false, true},
		{"if true; then ping -c1 x; fi", false, true},
		{"echo 'unterminated", false, false},
		{"echo 'unterminated && curl x", false, true},
	} {
		got := detectRiskExtras(c.cmd)
		if got.privileged != c.priv || got.network != c.net {
			t.Errorf("%q: privileged=%v network=%v, want %v %v", c.cmd, got.privileged, got.network, c.priv, c.net)
		}
	}
}

func approvalFromJSON(t *testing.T, stdout string) string {
	t.Helper()
	for _, rec := range readJSONLines(t, stdout) {
		if rec.Type == "approval" {
			return rec.Text
		}
	}
	t.Fatalf("no approval record in %q", stdout)
	return ""
}

// Plain words, true to the capture: a deletion synapse saves to its trash
// says undo can put it back (atk1-03, atk2-04), and the same explanation
// appears in text mode and --json (atk1-05).
func TestConsentForADeletionIsPlainAndTrue(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"), "x")
	m := newFakeModel(t, runStep("rm keep.txt"))
	r := execSynapse(t, synapseRun{args: []string{"run", "delete keep.txt"}, env: m.env(), dir: dir})
	for _, want := range []string{"I need your OK", "It deletes keep.txt.", "`synapse undo` can put things back"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("text-mode approval lacks %q:\n%s", want, r.stderr)
		}
	}
	if strings.Contains(r.stderr, "NOT") || strings.Contains(r.stderr, "can't be undone") {
		t.Errorf("text-mode approval says it cannot be undone, but rm is captured:\n%s", r.stderr)
	}
	m2 := newFakeModel(t, runStep("rm keep.txt"))
	j := execSynapse(t, synapseRun{args: []string{"run", "--json", "delete keep.txt"}, env: m2.env(), dir: dir})
	if text := approvalFromJSON(t, j.stdout); !strings.Contains(r.stderr, text) {
		t.Errorf("--json approval differs from text mode:\njson: %q\ntext:\n%s", text, r.stderr)
	}
}

// An append is described as a change, not an overwrite (atk1-06).
func TestConsentForAnAppendDoesNotSayOverwrite(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "first\n")
	m := newFakeModel(t, runStep("echo 'call mom' >> a.txt"))
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "add a line"}, env: m.env(), dir: dir})
	if r.code == exitOK {
		return // ran without asking: nothing to describe
	}
	// The plain sentences are what a person reads; the details line keeps the
	// analysis's own vocabulary, where any write to a file is an "overwrite".
	plain, _, _ := strings.Cut(approvalFromJSON(t, r.stdout), "\n(technical reason for asking:")
	if strings.Contains(strings.ToLower(plain), "replaces") || strings.Contains(strings.ToLower(plain), "overwrite") {
		t.Errorf("an append was described as an overwrite: %q", plain)
	}
}

func TestNetworkAndAdministratorCommandsAlwaysAsk(t *testing.T) {
	for _, c := range []struct{ cmd, want string }{
		{"curl -s https://wttr.in", "connects to the internet"},
		{"sudo touch /tmp/never-created-by-a-test", "administrator (root) rights"},
	} {
		m := newFakeModel(t, runStep(c.cmd))
		r := execSynapse(t, synapseRun{args: []string{"run", "--json", "do it"}, env: m.env()}) // stdin closed: declined
		if r.code != exitDeclined {
			t.Errorf("%q: exit %d, want %d (asked and declined)", c.cmd, r.code, exitDeclined)
			continue
		}
		text := approvalFromJSON(t, r.stdout)
		if !strings.Contains(text, c.want) || !strings.Contains(text, "Undo cannot take this back") {
			t.Errorf("%q: approval %q lacks the plain explanation", c.cmd, text)
		}
	}
}

func TestConsentNamesAppendsLinksAndEmbeddedNetwork(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "x\n")
	writeFile(t, filepath.Join(dir, "target.txt"), "t\n")
	os.Symlink("target.txt", filepath.Join(dir, "link.txt"))
	for _, c := range []struct{ cmd, want, notWant string }{
		{"echo more >> a.txt", "adds to the end of a.txt", "gets replaced"},
		{"echo new > link.txt", "a link to target.txt", ""},
		{`python3 -c "import requests; requests.get('http://example.com')"`, "connects to the internet", ""},
	} {
		m := newFakeModel(t, runStep(c.cmd))
		r := execSynapse(t, synapseRun{args: []string{"run", "--json", "do it"}, env: m.env(), dir: dir})
		if r.code == exitOK {
			t.Errorf("%q ran without asking", c.cmd)
			continue
		}
		text := approvalFromJSON(t, r.stdout)
		if !strings.Contains(text, c.want) || (c.notWant != "" && strings.Contains(text, c.notWant)) {
			t.Errorf("%q: approval %q", c.cmd, text)
		}
	}
}

// A quoted or escaped program name runs the same program, so it is caught the
// same way: the check used to read "" for every quoted word (ship review,
// 2026-10-27). A word whose value comes from an expansion is left to the
// analysis, which cannot resolve it and asks.
func TestQuotedProgramNamesAreStillCaught(t *testing.T) {
	for _, c := range []struct {
		cmd                 string
		network, privileged bool
	}{
		{`"curl" https://example.com/x`, true, false},
		{`\curl https://example.com/x`, true, false},
		{`c''url https://example.com/x`, true, false},
		{`'wget' -q example.com`, true, false},
		{`"sudo" rm x`, false, true},
		{`s\udo touch y`, false, true},
		{`echo "curl is a program"`, false, false},
	} {
		r := detectRiskExtras(c.cmd)
		if r.network != c.network || r.privileged != c.privileged {
			t.Errorf("%s: network=%v privileged=%v, want %v %v", c.cmd, r.network, r.privileged, c.network, c.privileged)
		}
	}
}

// "Changes to your own files are still copied first" is said only when the
// command changes files: sudo curl changes none, and its two sentences, one
// for each risk, used to trigger the claim.
func TestCopiedFirstIsSaidOnlyWhenFilesChange(t *testing.T) {
	withAnalysis(t, "strict")
	dir := t.TempDir()
	for _, c := range []struct {
		cmd  string
		want bool
	}{
		{"sudo curl https://example.com/x", false},
		{"curl -o page.html https://example.com/x", true},
	} {
		gd := analysisDecision(context.Background(), c.cmd, dir)
		extras := detectRiskExtras(c.cmd)
		got := strings.Contains(buildConsent(false, "", gd, extras, false, true, dir).text(), "still copied first")
		if got != c.want {
			t.Errorf("%s: says copied first = %v, want %v\n%s", c.cmd, got, c.want, buildConsent(false, "", gd, extras, false, true, dir).text())
		}
	}
}
