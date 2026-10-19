package effects

import (
	"archive/tar"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// archiveFixture is the standard fixture plus rotated logs, compressed files, and
// archives, for the compression, zip, and tar rules.
func archiveFixture(t *testing.T) string {
	t.Helper()
	wd := fixture(t)
	for p, body := range map[string]string{
		"app.log.1": "one\n", "app.log.2": "two\n", "app.log.1.gz": "stale\n",
		"c.txt.gz": "gz\n", "pkg.tgz": "tgz\n", "pkg.txz": "txz\n", "notes.bz2": "bz\n",
		"existing.zip": "PK\n", "arch.tar": "tar\n", "list.txt": "a.txt\n",
	} {
		if err := os.WriteFile(filepath.Join(wd, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return wd
}

func TestCompressionRules(t *testing.T) {
	wd := archiveFixture(t)
	runRuleCasesIn(t, wd, []ruleCase{
		// The default replaces the input: the original is removed (captured, the output is
		// different bytes) and the compressed file written, over an existing one if need be.
		{"gzip app.log.1", false, RecoverableWithCapture, []string{"remove app.log.1", "write app.log.1.gz"}, nil},
		{"gzip -k a.txt", false, Recoverable, []string{"create a.txt.gz"}, []string{"remove a.txt"}},
		{"gzip --keep a.txt", false, Recoverable, []string{"create a.txt.gz"}, []string{"remove a.txt"}},
		{"gzip -S .z a.txt", false, RecoverableWithCapture, []string{"create a.txt.z", "remove a.txt"}, []string{"create a.txt.gz"}},
		{"gzip -9f big.log", false, RecoverableWithCapture, []string{"create big.log.gz", "remove big.log"}, nil},
		// Standard output, test, and list modes write no file.
		{"gzip -c a.txt", false, Recoverable, nil, nil},
		{"gzip --stdout a.txt > a.gz", false, Recoverable, []string{"create a.gz"}, []string{"remove a.txt"}},
		{"gzip -t c.txt.gz", false, Recoverable, nil, nil},
		{"gzip -l c.txt.gz", false, Recoverable, nil, nil},
		// A directory is skipped unless -r, which compresses every file under it.
		{"gzip logs", false, Recoverable, nil, []string{"remove logs"}},
		{"gzip -r logs", false, RecoverableWithCapture, []string{"remove logs/x.log", "create logs/x.log.gz", "remove logs/y.log"}, []string{"remove logs"}},
		// Decompression replaces the recognised suffix; .tgz becomes .tar.
		{"gunzip c.txt.gz", false, RecoverableWithCapture, []string{"create c.txt", "remove c.txt.gz"}, nil},
		{"gzip -d pkg.tgz", false, RecoverableWithCapture, []string{"create pkg.tar", "remove pkg.tgz"}, nil},
		{"gunzip -k c.txt.gz", false, Recoverable, []string{"create c.txt"}, []string{"remove c.txt.gz"}},
		{"gunzip notes.txt", false, Recoverable, nil, nil}, // unknown suffix: gunzip skips it
		{"gunzip -N c.txt.gz", false, Unrecoverable, nil, nil},
		// bzip2 has no -S; a name it cannot derive becomes NAME.out.
		{"bzip2 a.txt", false, RecoverableWithCapture, []string{"create a.txt.bz2", "remove a.txt"}, nil},
		{"bunzip2 notes.bz2", false, RecoverableWithCapture, []string{"create notes", "remove notes.bz2"}, nil},
		{"bunzip2 notes.txt", false, RecoverableWithCapture, []string{"create notes.txt.out", "remove notes.txt"}, nil},
		{"bzip2 -dk notes.bz2", false, Recoverable, []string{"create notes"}, []string{"remove notes.bz2"}},
		// xz: another format means another suffix, and --files reads names from a file.
		{"xz a.txt", false, RecoverableWithCapture, []string{"create a.txt.xz", "remove a.txt"}, nil},
		{"unxz pkg.txz", false, RecoverableWithCapture, []string{"create pkg.tar", "remove pkg.txz"}, nil},
		{"xz -T 4 -k a.txt", false, Recoverable, []string{"create a.txt.xz"}, nil},
		{"xz --format=lzma a.txt", false, Unrecoverable, nil, nil},
		{"xz --files=list.txt", false, Unrecoverable, nil, nil},
		// zstd keeps its input unless --rm, and -o names the output.
		{"zstd a.txt", false, Recoverable, []string{"create a.txt.zst"}, []string{"remove a.txt"}},
		{"zstd -19 -T0 a.txt", false, Recoverable, []string{"create a.txt.zst"}, []string{"remove a.txt"}},
		{"zstd --rm a.txt", false, RecoverableWithCapture, []string{"create a.txt.zst", "remove a.txt"}, nil},
		{"zstd -o b.txt a.txt", false, RecoverableWithCapture, []string{"write b.txt"}, []string{"create a.txt.zst"}},
		{"zstd -d c.txt.gz", false, Recoverable, []string{"create c.txt"}, []string{"remove c.txt.gz"}},
		{"zstd --output-dir-flat out a.txt", false, Unrecoverable, nil, nil},
		{"zstd -T a.txt", false, Unrecoverable, nil, nil}, // -T takes only an attached value
		// An option a rule does not know may change what it writes, so it asks.
		{"gzip --frobnicate a.txt", false, Unrecoverable, nil, nil},
	})
}

func TestZipRule(t *testing.T) {
	wd := archiveFixture(t)
	runRuleCasesIn(t, wd, []ruleCase{
		// A new archive is a creation; an existing one is rewritten in place and captured.
		{"zip old-logs.zip app.log.1 app.log.2", false, Recoverable, []string{"create old-logs.zip"}, []string{"remove app.log.1"}},
		{"zip old-logs app.log.1", false, Recoverable, []string{"create old-logs.zip"}, nil}, // .zip is added to a name with no suffix
		{"zip existing.zip a.txt", false, RecoverableWithCapture, []string{"write existing.zip"}, nil},
		{"zip existing a.txt", false, RecoverableWithCapture, []string{"write existing.zip"}, nil},
		{"zip -r -q src.zip src", false, Recoverable, []string{"create src.zip"}, nil},
		{"zip -qr9 src.zip src -x '*.o' '*.tmp'", false, Recoverable, []string{"create src.zip"}, nil},
		{"zip -u existing.zip a.txt", false, RecoverableWithCapture, []string{"write existing.zip"}, nil},
		{"zip -T new.zip a.txt", false, Recoverable, []string{"create new.zip"}, nil},
		// -m deletes the inputs once archived; the archive is a copy, so each is captured.
		{"zip -m logs.zip app.log.1 app.log.2", false, RecoverableWithCapture, []string{"create logs.zip", "remove app.log.1", "remove app.log.2"}, nil},
		{"zip --move logs.zip app.log.1", false, RecoverableWithCapture, []string{"remove app.log.1"}, nil},
		{"zip -rm src.zip src", false, RecoverableWithCapture, []string{"create src.zip", "remove src"}, nil},
		{"zip -m d.zip src", false, Recoverable, []string{"create d.zip"}, []string{"remove src"}}, // without -r a full directory stays
		{"zip -m t.zip tmp", false, RecoverableWithCapture, []string{"remove tmp"}, nil},           // an empty one goes
		{"zip -m - a.txt > out.zip", false, RecoverableWithCapture, []string{"create out.zip", "remove a.txt"}, nil},
		{"zip - a.txt > out.zip", false, Recoverable, []string{"create out.zip"}, nil},
		// -d deletes entries inside the archive, not the files named.
		{"zip -d existing.zip a.txt", false, RecoverableWithCapture, []string{"write existing.zip"}, []string{"remove a.txt"}},
		// What the rule cannot model asks.
		{"zip -m p.zip '*.txt'", false, Unrecoverable, nil, nil},
		{"zip -@ l.zip < list.txt", false, Unrecoverable, nil, nil},
		{"zip -e s.zip a.txt", false, Unrecoverable, nil, nil},
		{"zip -TT 'rm a.txt' t.zip a.txt", false, Unrecoverable, nil, nil},
		{"zip -sf s.zip a.txt", false, Unrecoverable, nil, nil},
		{"zip --out new.zip existing.zip a.txt", false, Unrecoverable, nil, nil},
		{"zip -b /tmp s.zip a.txt", false, Unrecoverable, nil, nil},
		// The agent command study's line (open-problems row 44): zip, test the archive,
		// then delete the originals. The rm's targets are known, so they are captured.
		{"zip old-logs.zip app.log.1 app.log.2 && unzip -tq old-logs.zip && rm app.log.1 app.log.2 && ls", true, RecoverableWithCapture,
			[]string{"create old-logs.zip", "remove app.log.1", "remove app.log.2"}, nil},
	})
}

func TestZipBatchCapturePlan(t *testing.T) {
	wd := archiveFixture(t)
	a := analyze(t, wd, "zip old-logs.zip app.log.1 app.log.2 && unzip -tq old-logs.zip && rm app.log.1 app.log.2 && ls", true)
	var got []string
	for _, c := range a.Plan().Captures {
		got = append(got, string(c.Mechanism)+" "+filepath.Base(c.Path))
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "trash app.log.1" || got[1] != "trash app.log.2" {
		t.Fatalf("plan = %v, want the two logs captured and nothing else", got)
	}
}

func TestTarRuleOptions(t *testing.T) {
	wd := archiveFixture(t)
	runRuleCasesIn(t, wd, []ruleCase{
		// --remove-files deletes the inputs wherever the archive goes, standard output included.
		{"tar -czf out.tgz src --remove-files", false, RecoverableWithCapture, []string{"create out.tgz", "remove src"}, nil},
		{"tar -c src --remove-files | gzip > s.tgz", false, RecoverableWithCapture, []string{"remove src", "create s.tgz"}, nil},
		// Each -C applies to the names after it.
		{"tar -cf m.tar a.txt -C src main.go --remove-files", false, RecoverableWithCapture, []string{"remove a.txt", "remove src/main.go"}, []string{"remove main.go"}},
		{"tar -cf m.tar --remove-files -T list.txt", false, Unrecoverable, nil, nil},
		// Creating over an existing archive, appending, and deleting from one rewrite it.
		{"tar -cf arch.tar a.txt", false, RecoverableWithCapture, []string{"write arch.tar"}, nil},
		{"tar -rf arch.tar b.txt", false, RecoverableWithCapture, []string{"write arch.tar"}, nil},
		{"tar --delete -f arch.tar a.txt", false, RecoverableWithCapture, []string{"write arch.tar"}, []string{"remove a.txt"}},
		{"tar -cf new.tar -g snap.db src", false, Recoverable, []string{"create new.tar", "create snap.db"}, nil},
		// A compressor named with -I is a filter; any other program is not visible.
		{"tar -I zstd -cf s.tar.zst src", false, Recoverable, []string{"create s.tar.zst"}, nil},
		{"tar -I 'sh -c x' -cf s.tar src", false, Unrecoverable, nil, nil},
		{"tar -xf arch.tar --to-command 'rm a.txt'", true, Unrecoverable, nil, nil},
		{"tar -xf arch.tar --checkpoint=1 --checkpoint-action=exec='rm a.txt'", true, Unrecoverable, nil, nil},
		// Names the listing would not show where they land.
		{"tar -xf arch.tar --transform 's/^/x/'", true, Unrecoverable, nil, nil},
		{"tar -xPf arch.tar", true, Unrecoverable, nil, nil},
		{"tar -xf arch.tar --strip-components x", true, Unrecoverable, nil, nil},
		// host:file is a remote archive to GNU tar.
		{"tar -cf host:backup.tar src", false, Unrecoverable, nil, nil},
		{"tar --force-local -cf host:backup.tar src", false, Recoverable, nil, nil},
	})
}

// TestTarExtractReplacesAFileWithADirectory: GNU tar removes a file standing where the
// archive has a directory, so the file is lost unless captured. The analysis used to see
// only the members and miss it.
func TestTarExtractReplacesAFileWithADirectory(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar is not installed")
	}
	wd := archiveFixture(t)
	f, err := os.Create(filepath.Join(wd, "tree.tar"))
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for _, h := range []*tar.Header{
		{Name: "a.txt/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "a.txt/inner", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
		{Name: "deep/er/file", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			tw.Write([]byte("x"))
		}
	}
	tw.Close()
	f.Close()
	runRuleCasesIn(t, wd, []ruleCase{
		{"tar -xf tree.tar", true, RecoverableWithCapture,
			[]string{"remove a.txt", "create a.txt", "create a.txt/inner", "create deep", "create deep/er", "create deep/er/file"}, nil},
		{"tar -xf tree.tar -O", true, Recoverable, nil, []string{"remove a.txt"}},
		{"tar --strip-components 1 -xf tree.tar", true, Recoverable, []string{"create inner", "create er/file"}, []string{"remove a.txt"}},
	})
}
