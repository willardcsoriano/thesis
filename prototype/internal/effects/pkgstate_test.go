package effects

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fakeSys answers the read-only queries the package and service model makes, keyed
// by the joined argv, so the tests do not depend on what the host has installed.
func fakeSys(answers map[string]string) Runner {
	return func(_ context.Context, _ string, argv []string) ([]byte, error) {
		if out, ok := answers[strings.Join(argv, " ")]; ok {
			return []byte(out), nil
		}
		return nil, exec.ErrNotFound
	}
}

const policyHello = `hello:
  Installed: 2.10-3
  Candidate: 2.10-3
  Version table:
 *** 2.10-3 500
        500 http://deb.debian.org/debian trixie/main amd64 Packages
        100 /var/lib/dpkg/status
     2.9-1 500
        500 http://deb.debian.org/debian bookworm/main amd64 Packages
`

const policyGone = `old:
  Installed: 1.0
  Version table:
 *** 1.0 100
        100 /var/lib/dpkg/status
`

func analyzeWith(cmd string, answers map[string]string) *Analysis {
	a := New("/")
	a.Run = fakeSys(answers)
	return a.Analyze(context.Background(), cmd)
}

func inverses(a *Analysis) []string {
	var out []string
	for _, sc := range a.States {
		for _, argv := range sc.Inverse {
			out = append(out, strings.Join(argv, " "))
		}
	}
	return out
}

func TestAptInstallInvertsToPurge(t *testing.T) {
	a := analyzeWith("sudo apt-get install -y hello", map[string]string{
		"apt-get -s install hello":                     "Inst hello (2.10-3 Debian:13.0/stable [amd64])\n",
		"dpkg-query -W -f=${db:Status-Abbrev}\n hello": "\n",
	})
	if len(a.Issues) != 0 {
		t.Fatalf("issues: %+v", a.Issues)
	}
	if got := inverses(a); len(got) != 1 || got[0] != "apt-get purge -y hello" {
		t.Fatalf("inverse = %v", got)
	}
	if !a.States[0].Sudo {
		t.Fatal("the undo of a sudo command must run with sudo")
	}
	if v := a.Verdict(); v.Class != RecoverableWithCapture {
		t.Fatalf("class = %v", v.Class)
	}
	if p := a.Plan(); len(p.Captures) != 1 || p.Captures[0].Mechanism != Inverse {
		t.Fatalf("plan = %+v", p)
	}
}

func TestAptInstallKeepsConfigLeftByRemove(t *testing.T) {
	a := analyzeWith("apt-get install hello", map[string]string{
		"apt-get -s install hello":                     "Inst hello (2.10-3 Debian:13.0/stable [amd64])\n",
		"dpkg-query -W -f=${db:Status-Abbrev}\n hello": "rc \n",
	})
	if got := inverses(a); len(got) != 1 || got[0] != "apt-get remove -y hello" {
		t.Fatalf("a package whose config survives a remove must be undone with remove, got %v", got)
	}
}

func TestAptUpgradeInvertsToDowngrade(t *testing.T) {
	a := analyzeWith("apt-get install hello", map[string]string{
		"apt-get -s install hello": "Inst hello [2.9-1] (2.10-3 Debian:13.0/stable [amd64])\n",
		"apt-cache policy hello":   policyHello,
	})
	got := inverses(a)
	if len(got) != 1 || !strings.HasSuffix(got[0], "--allow-downgrades hello=2.9-1") {
		t.Fatalf("inverse = %v, issues %+v", got, a.Issues)
	}
}

func TestAptUpgradeFailsClosedWhenOldVersionGone(t *testing.T) {
	a := analyzeWith("apt-get install old", map[string]string{
		"apt-get -s install old": "Inst old [1.0] (2.0 Debian:13.0/stable [amd64])\n",
		"apt-cache policy old":   policyGone,
	})
	if v := a.Verdict(); v.Class != Unrecoverable {
		t.Fatalf("an upgrade that cannot be undone must be unrecoverable, got %v %v", v.Class, v.Reasons)
	}
}

func TestAptRemoveInvertsToInstall(t *testing.T) {
	a := analyzeWith("apt-get remove -y hello", map[string]string{
		"apt-get -s remove hello": "Remv hello [2.10-3]\n",
		"apt-cache policy hello":  policyHello,
	})
	got := inverses(a)
	if len(got) != 1 || !strings.HasSuffix(got[0], "hello=2.10-3") || !strings.HasPrefix(got[0], "apt-get install") {
		t.Fatalf("inverse = %v, issues %+v", got, a.Issues)
	}
}

func TestAptPurgeCapturesConfigFiles(t *testing.T) {
	dir := t.TempDir()
	conf := dir + "/hello.conf"
	if err := os.WriteFile(conf, []byte("k=v\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := analyzeWith("apt-get purge hello", map[string]string{
		"apt-get -s purge hello":                "Purg hello [2.10-3]\n",
		"apt-cache policy hello":                policyHello,
		"dpkg-query -W -f=${Conffiles}\n hello": conf + " abc123\n",
	})
	found := false
	for _, e := range a.Effects {
		if e.Kind == Remove && e.Path == conf {
			found = true
		}
	}
	if !found {
		t.Fatalf("a purge deletes config files, which must be captured: %+v", a.Effects)
	}
	if p := a.Plan(); !hasMechanism(p, Trash) || !hasMechanism(p, Inverse) {
		t.Fatalf("plan should trash the config and hold the inverse: %+v", p)
	}
}

func hasMechanism(p Plan, m Mechanism) bool {
	for _, c := range p.Captures {
		if c.Mechanism == m {
			return true
		}
	}
	return false
}

func TestAptWithoutRunnerFailsClosed(t *testing.T) {
	a := New("/")
	res := a.Analyze(context.Background(), "apt-get install hello")
	if v := res.Verdict(); v.Class != Unrecoverable {
		t.Fatalf("no simulation must mean unrecoverable, got %v", v.Class)
	}
}

func TestAptOptionThatChangesSimulationIsUnresolved(t *testing.T) {
	a := analyzeWith("apt-get -o Dpkg::Options::=x install hello", map[string]string{})
	if v := a.Verdict(); v.Class != Unrecoverable {
		t.Fatalf("got %v", v.Class)
	}
}

func TestAptUpdateAndReadOnlyStayRecoverable(t *testing.T) {
	for _, cmd := range []string{"apt-get update", "apt update", "apt list --installed", "apt-cache policy hello", "dpkg -l"} {
		a := analyzeWith(cmd, map[string]string{})
		if v := a.Verdict(); v.Class != Recoverable {
			t.Errorf("%s: %v %v", cmd, v.Class, v.Reasons)
		}
	}
}

func TestDpkgRemoveAndUnknownForm(t *testing.T) {
	a := analyzeWith("dpkg -r hello", map[string]string{
		"dpkg-query -W -f=${Version}\n hello": "2.10-3\n",
		"apt-cache policy hello":              policyHello,
	})
	if got := inverses(a); len(got) != 1 {
		t.Fatalf("inverse = %v, issues %+v", got, a.Issues)
	}
	if v := analyzeWith("dpkg --force-all -r hello", nil).Verdict(); v.Class != Unrecoverable {
		t.Fatalf("force options must fail closed, got %v", v.Class)
	}
}

func unitAnswers(unit, active, enabled string) map[string]string {
	return map[string]string{
		"systemctl is-active " + unit:  active + "\n",
		"systemctl is-enabled " + unit: enabled + "\n",
	}
}

func TestSystemctlInverses(t *testing.T) {
	cases := []struct {
		cmd, active, enabled string
		want                 []string
	}{
		{"systemctl stop cron", "active", "enabled", []string{"systemctl start cron"}},
		{"systemctl stop cron", "inactive", "enabled", nil},
		{"systemctl start cron", "inactive", "enabled", []string{"systemctl stop cron"}},
		{"systemctl start cron", "active", "enabled", nil},
		{"systemctl enable cron", "active", "disabled", []string{"systemctl disable cron"}},
		{"systemctl enable --now cron", "inactive", "disabled", []string{"systemctl disable cron", "systemctl stop cron"}},
		{"systemctl disable cron", "active", "enabled", []string{"systemctl enable cron"}},
		{"systemctl mask cron", "inactive", "enabled", []string{"systemctl unmask cron", "systemctl enable cron"}},
		{"systemctl unmask cron", "inactive", "masked", []string{"systemctl mask cron"}},
		{"service cron stop", "active", "enabled", []string{"systemctl start cron"}},
	}
	for _, c := range cases {
		a := analyzeWith("sudo "+c.cmd, unitAnswers("cron", c.active, c.enabled))
		if len(a.Issues) != 0 {
			t.Errorf("%s: issues %+v", c.cmd, a.Issues)
			continue
		}
		if got := inverses(a); strings.Join(got, ";") != strings.Join(c.want, ";") {
			t.Errorf("%s (%s/%s): inverse %v, want %v", c.cmd, c.active, c.enabled, got, c.want)
		}
	}
}

func TestSystemctlUnknownAndIrreversible(t *testing.T) {
	if v := analyzeWith("systemctl stop nosuch", map[string]string{
		"systemctl is-active nosuch":  "inactive\n",
		"systemctl is-enabled nosuch": "not-found\n",
	}).Verdict(); v.Class != Unrecoverable {
		t.Errorf("an unknown unit must fail closed, got %v", v.Class)
	}
	for _, cmd := range []string{"systemctl poweroff", "systemctl reboot", "systemctl isolate rescue.target", "systemctl kill cron"} {
		if v := analyzeWith(cmd, nil).Verdict(); v.Class != Unrecoverable {
			t.Errorf("%s must be unrecoverable, got %v", cmd, v.Class)
		}
	}
	if v := analyzeWith("systemctl daemon-reload", nil).Verdict(); v.Class != Recoverable {
		t.Errorf("daemon-reload changes nothing lasting, got %v", v.Class)
	}
}

// TestLiveDebian checks the real tools' output formats against the parser. It skips
// where apt is not installed.
func TestLiveDebian(t *testing.T) {
	if _, err := exec.LookPath("apt-get"); err != nil {
		t.Skip("apt-get not installed")
	}
	if _, err := exec.LookPath("dpkg-query"); err != nil {
		t.Skip("dpkg-query not installed")
	}
	an := New("/")
	an.Run = DefaultRunner(30 * time.Second)
	res := an.Analyze(context.Background(), "sudo apt-get remove -y bash")
	// bash is essential, so apt refuses; the point is that the analysis neither
	// crashes nor reports the command as harmless.
	if v := res.Verdict(); v.Class == Recoverable {
		t.Fatalf("removing an essential package must not be recoverable: %+v", res)
	}
}
