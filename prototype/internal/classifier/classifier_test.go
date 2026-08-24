package classifier

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		verdict Verdict
	}{
		{"plain ls", "ls -la /home/user", Reversible},
		{"find", "find /home/user -name '*.pdf' -mtime -7", Reversible},
		{"ps sorted by memory", "ps aux --sort=-%mem | head -6", Reversible},
		{"append redirect is safe", "echo hello >> log.txt", Reversible},
		{"mv into a new dir", "mv ~/Desktop/*.png ~/Desktop/Screenshots/", Reversible},

		{"rm", "rm old.txt", Irreversible},
		{"rm recursive force", "rm -rf /tmp/build", Irreversible},
		{"rm inside pipeline", "find . -name '*.tmp' | xargs rm", Irreversible},
		{"dd", "dd if=/dev/zero of=/dev/sda bs=1M", Irreversible},
		{"mkfs", "mkfs.ext4 /dev/sdb1", Irreversible},
		{"shred", "shred -u secrets.txt", Irreversible},
		{"git reset hard", "git reset --hard HEAD~1", Irreversible},
		{"git clean force", "git clean -fd", Irreversible},
		{"truncating redirect", "echo data > important.txt", Irreversible},
		{"truncating stderr redirect", "some-cmd 2> error.log", Irreversible},

		{"word containing rm substring is not rm", "confirm the alarm settings", Reversible},
		{"word containing dd substring is not dd", "add a new user to the group", Reversible},

		// Content-mutating gap found and closed Session 22: these previously
		// came back Reversible (auto-run, no confirmation) despite destroying
		// existing file content with no built-in undo, same failure class as
		// rm/dd/shred above.
		{"sed in-place edit", `sed -i 's/foo/bar/' file.txt`, Irreversible},
		{"sed in-place edit with backup suffix", `sed -i.bak 's/foo/bar/' file.txt`, Irreversible},
		{"sed in-place long flag", `sed --in-place 's/foo/bar/' file.txt`, Irreversible},
		{"awk in-place edit", `awk -i inplace '{print}' file.txt`, Irreversible},
		{"tee without append overwrites", `echo "new content" | tee file.txt`, Irreversible},
		{"tee heredoc without append", `tee file.txt <<< "new content"`, Irreversible},
		{"truncate shrinks a file", "truncate -s 0 file.txt", Irreversible},
		{"truncate to a size", "truncate -s 1M bigfile.bin", Irreversible},

		// False-positive guards for the new rules: don't flag lookalikes.
		{"sed without -i is a read-only substitution preview", `sed 's/foo/bar/' file.txt`, Reversible},
		{"grep -i is case-insensitivity, not sed -i", `sed 's/a/b/' file.txt | grep -i foo`, Reversible},
		{"awk without -i inplace is a normal filter", `awk '{print $1}' file.txt`, Reversible},
		{"tee with append flag is safe", "echo hello | tee -a log.txt", Reversible},
		{"tee with long append flag is safe", "echo hello | tee --append log.txt", Reversible},
		{"word containing truncate substring is not truncate", "the untruncated report is attached", Reversible},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := Classify(tc.cmd)
			if got != tc.verdict {
				t.Fatalf("Classify(%q) = %v (reason %q), want %v", tc.cmd, got, reason, tc.verdict)
			}
			if got == Irreversible && reason == "" {
				t.Fatalf("Classify(%q) returned Irreversible with no reason", tc.cmd)
			}
			if got == Reversible && reason != "" {
				t.Fatalf("Classify(%q) returned Reversible with a non-empty reason %q", tc.cmd, reason)
			}
		})
	}
}

func TestVerdictString(t *testing.T) {
	if got := Reversible.String(); got != "reversible" {
		t.Errorf("Reversible.String() = %q, want %q", got, "reversible")
	}
	if got := Irreversible.String(); got != "irreversible" {
		t.Errorf("Irreversible.String() = %q, want %q", got, "irreversible")
	}
}
