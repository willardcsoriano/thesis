package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Hostile fixtures. A fixture is data (a task suite, an inferred corpus
// line), and Materialize runs on the host, outside any sandbox, so nothing a
// fixture says may create, change or read anything outside its root.

// arena is a root to materialize into and a writable folder beside it that
// an escape would reach, holding one file an escape could overwrite.
func arena(t *testing.T) (root, outside, victim string) {
	t.Helper()
	base := t.TempDir()
	root, outside = filepath.Join(base, "root"), filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	victim = filepath.Join(outside, "victim")
	if err := os.WriteFile(victim, []byte("intact"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, outside, victim
}

func outsideUntouched(t *testing.T, outside, victim string) {
	t.Helper()
	if b, err := os.ReadFile(victim); err != nil || string(b) != "intact" {
		t.Errorf("the file outside the root was changed: %q %v", b, err)
	}
	ents, _ := os.ReadDir(outside)
	if len(ents) != 1 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("something was created outside the root: %v", names)
	}
}

func TestMaterializeRefusesEscapes(t *testing.T) {
	for name, fx := range map[string]func(outside, victim string) []Entry{
		"parent path":        func(o, v string) []Entry { return []Entry{{Path: "../outside/new", Kind: "file", Data: "x"}} },
		"parent inside path": func(o, v string) []Entry { return []Entry{{Path: "a/../../outside/new", Kind: "file", Data: "x"}} },
		"parent dir":         func(o, v string) []Entry { return []Entry{{Path: "../outside/newdir", Kind: "dir"}} },
		"parent link":        func(o, v string) []Entry { return []Entry{{Path: "../outside/newlink", Kind: "link", Target: "x"}} },
		"absolute path":      func(o, v string) []Entry { return []Entry{{Path: v, Kind: "file", Data: "pwned"}} },
		"write through an absolute link": func(o, v string) []Entry {
			return []Entry{{Path: "l", Kind: "link", Target: v}, {Path: "l", Kind: "file", Data: "pwned"}}
		},
		"write under an absolute dir link": func(o, v string) []Entry {
			return []Entry{{Path: "d", Kind: "link", Target: o}, {Path: "d/new", Kind: "file", Data: "x"}}
		},
		"mkdir under a relative dir link": func(o, v string) []Entry {
			return []Entry{{Path: "d", Kind: "link", Target: "../outside"}, {Path: "d/newdir/sub", Kind: "dir"}}
		},
		"link chain out": func(o, v string) []Entry {
			return []Entry{{Path: "a", Kind: "link", Target: "b"}, {Path: "b", Kind: "link", Target: "../outside"}, {Path: "a/new", Kind: "file", Data: "x"}}
		},
		"link in a subdir out": func(o, v string) []Entry {
			return []Entry{{Path: "s/t/l", Kind: "link", Target: "../../../outside"}, {Path: "s/t/l/new", Kind: "file", Data: "x"}}
		},
		"link then overwrite through it relatively": func(o, v string) []Entry {
			return []Entry{{Path: "v", Kind: "link", Target: "../outside/victim"}, {Path: "v", Kind: "file", Data: "pwned", Mode: 0o600}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, outside, victim := arena(t)
			if err := Materialize(root, fx(outside, victim)); err == nil {
				t.Error("an escaping fixture was materialized without error")
			}
			outsideUntouched(t, outside, victim)
		})
	}
}

// A link may point anywhere: it is data in the fixture (the command under
// test acts on it in the sandbox). Only following it while materializing is
// refused.
func TestMaterializeKeepsOutwardLinks(t *testing.T) {
	root, outside, victim := arena(t)
	if err := Materialize(root, []Entry{{Path: "abs", Kind: "link", Target: "/etc/passwd"}, {Path: "rel", Kind: "link", Target: "../outside/victim"}}); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{"abs": "/etc/passwd", "rel": "../outside/victim"} {
		if got, err := os.Readlink(filepath.Join(root, p)); err != nil || got != want {
			t.Errorf("%s -> %q %v", p, got, err)
		}
	}
	outsideUntouched(t, outside, victim)
}

// Odd but harmless names are created exactly; impossible ones are errors,
// never panics or silent skips.
func TestMaterializeHostileNames(t *testing.T) {
	root := t.TempDir()
	names := []string{"new\nline", "-rf", "--", "ünïcödé ✓", "sp ace", "tab\tbed", "\x01ctrl", "\xff\xfe invalid", "...", "a..b", ".hidden", strings.Repeat("n", 255)}
	var fx []Entry
	for _, n := range names {
		fx = append(fx, Entry{Path: "d/" + n, Kind: "file", Data: n})
	}
	if err := Materialize(root, fx); err != nil {
		t.Fatal(err)
	}
	snap, err := Observe(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if _, ok := snap[filepath.Join("d", n)]; !ok {
			t.Errorf("%q not observed", n)
		}
	}
	for _, bad := range []Entry{
		{Path: strings.Repeat("n", 256), Kind: "file"},
		{Path: strings.Repeat("deep/", 900) + "f", Kind: "file"}, // longer than Observe could walk
		{Path: "", Kind: "file"},
		{Path: ".", Kind: "file"},
		{Path: "nul\x00byte", Kind: "file"},
		{Path: "x", Kind: "fifo"},
		{Path: "x", Kind: ""},
		{Path: "x", Kind: "file", Mode: 0o10644}, // not a permission bit
		{Path: "dirmode", Kind: "dir", Mode: 0o700},
	} {
		if err := Materialize(t.TempDir(), []Entry{bad}); err == nil {
			t.Errorf("%.40q (kind %q, mode %o) accepted", bad.Path, bad.Kind, bad.Mode)
		}
	}
}

// Modes: setuid, setgid and sticky bits are applied, not silently dropped,
// and a mode with no read bit still materializes.
func TestMaterializeModes(t *testing.T) {
	root := t.TempDir()
	modes := map[string]uint32{"suid": 0o4755, "sgid": 0o2755, "sticky": 0o1644, "none": 0o000 + 0o200, "x": 0o700}
	var fx []Entry
	for p, m := range modes {
		fx = append(fx, Entry{Path: p, Kind: "file", Data: p, Mode: m})
	}
	if err := Materialize(root, fx); err != nil {
		t.Fatal(err)
	}
	for p, m := range modes {
		var st syscall.Stat_t
		if err := syscall.Lstat(filepath.Join(root, p), &st); err != nil {
			t.Fatal(err)
		}
		if got := st.Mode & 0o7777; got != m {
			t.Errorf("%s: mode %o, want %o", p, got, m)
		}
	}
}

// A file the operator cannot read (mode 0200 in the fixture) is still
// compared: rewriting it, keeping its mode, is a loss.
func TestObserveSeesRewriteOfUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	root := t.TempDir()
	if err := Materialize(root, []Entry{{Path: "wo", Kind: "file", Data: "original", Mode: 0o200}}); err != nil {
		t.Fatal(err)
	}
	before, err := Observe(root)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "wo"), []byte("rewritten"), 0o200); err != nil {
		t.Fatal(err)
	}
	after, err := Observe(root)
	if err != nil {
		t.Fatal(err)
	}
	if v := Label(before, after); v.Label != "C" {
		t.Errorf("rewrite of an unreadable file labelled %s (%s)", v.Label, v.Reason)
	}
	again, _ := Observe(root)
	if v := Label(after, again); v.Label != "R" {
		t.Errorf("an untouched unreadable file labelled %s (%s)", v.Label, v.Reason)
	}
}

// Observe records a link as a link and never follows it, whatever it
// points at: nothing outside the root is read or listed.
func TestObserveDoesNotFollowLinks(t *testing.T) {
	root, outside, _ := arena(t)
	if err := Materialize(root, []Entry{
		{Path: "etc", Kind: "link", Target: "/etc"},
		{Path: "out", Kind: "link", Target: outside},
		{Path: "file", Kind: "link", Target: "/etc/passwd"},
		{Path: "loop", Kind: "link", Target: "loop"},
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := Observe(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 4 {
		t.Errorf("observed %d paths, want the 4 links: %v", len(snap), snap)
	}
	for p, n := range snap {
		if n.Kind != 'l' || n.Hash != "" {
			t.Errorf("%s observed as %c %q", p, n.Kind, n.Hash)
		}
	}
}

// A path swapped while Observe runs (a file replaced by a link to a host
// file, or by a FIFO) is never hashed through the link and never blocks.
func TestObserveRacingSwaps(t *testing.T) {
	root := t.TempDir()
	passwd, err := os.ReadFile("/etc/passwd")
	if err != nil {
		t.Skip(err)
	}
	sum := sha256.Sum256(passwd)
	secret := hex.EncodeToString(sum[:])
	f := filepath.Join(root, "f")
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			tmp := filepath.Join(root, ".swap")
			switch i % 3 {
			case 0:
				os.WriteFile(tmp, []byte("plain"), 0o644)
			case 1:
				os.Symlink("/etc/passwd", tmp)
			case 2:
				syscall.Mkfifo(tmp, 0o644)
			}
			os.Rename(tmp, f)
		}
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			snap, err := Observe(root)
			if err != nil {
				t.Error(err)
				return
			}
			if n, ok := snap["f"]; ok && n.Hash == secret {
				t.Error("Observe hashed /etc/passwd through a swapped-in link")
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Error("Observe blocked on a swapped-in FIFO")
	}
	close(stop)
	wg.Wait()
}

// A directory swapped for a link to outside while Materialize writes under
// it: no write lands outside.
func TestMaterializeRacingDirSwap(t *testing.T) {
	root, outside, victim := arena(t)
	d := filepath.Join(root, "d")
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			os.RemoveAll(d)
			if i%2 == 0 {
				os.Symlink(outside, d)
			} else {
				os.Mkdir(d, 0o755)
			}
		}
	}()
	var fx []Entry
	for i := 0; i < 2000; i++ {
		fx = append(fx, Entry{Path: "d/f" + strings.Repeat("x", i%7), Kind: "file", Data: "x"})
	}
	for i := 0; i < 20; i++ {
		_ = Materialize(root, fx) // errors are expected; escapes are not
	}
	close(stop)
	wg.Wait()
	outsideUntouched(t, outside, victim)
}

// A large fixture materializes and is observed whole.
func TestMaterializeHugeFixture(t *testing.T) {
	root := t.TempDir()
	var fx []Entry
	for i := 0; i < 20000; i++ {
		fx = append(fx, Entry{Path: filepath.Join("a", string(rune('a'+i%26)), "f"+strings.Repeat("0", i%5)+string(rune('A'+i/26%26))+string(rune('a'+i/676%26))), Kind: "file", Data: "x"})
	}
	if err := Materialize(root, fx); err != nil {
		t.Fatal(err)
	}
	snap, err := Observe(root)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, n := range snap {
		if n.Kind == 'f' {
			files++
		}
	}
	seen := map[string]bool{}
	for _, e := range fx {
		seen[e.Path] = true
	}
	if files != len(seen) {
		t.Errorf("observed %d files, want %d", files, len(seen))
	}
	_ = fs.ModePerm
}

// Ages are real modification times, set after the whole fixture exists, so
// an aged directory keeps its age even when its children come later in the
// list, and find(1) sees them the way a user's old files would be seen.
func TestMaterializeAges(t *testing.T) {
	root := t.TempDir()
	const day = 24 * 60 * 60
	fx := []Entry{
		{Path: "old", Kind: "dir", Age: 90 * day},
		{Path: "old/a.txt", Kind: "file", Data: "a", Age: 60 * day},
		{Path: "old/new.txt", Kind: "file", Data: "n"},
		{Path: "recent.txt", Kind: "file", Data: "r", Age: 2 * day},
		{Path: "plain.txt", Kind: "file", Data: "p"},
	}
	before := time.Now()
	if err := Materialize(root, fx); err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Duration{"old": 90 * 24 * time.Hour, "old/a.txt": 60 * 24 * time.Hour, "recent.txt": 2 * 24 * time.Hour}
	for p, age := range want {
		fi, err := os.Lstat(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		if got := before.Sub(fi.ModTime()); got < age-time.Minute || got > age+time.Minute {
			t.Errorf("%s: %v old, want %v", p, got.Round(time.Hour), age)
		}
	}
	for _, p := range []string{"old/new.txt", "plain.txt"} {
		fi, _ := os.Lstat(filepath.Join(root, p))
		if time.Since(fi.ModTime()) > time.Minute {
			t.Errorf("%s has no age but is %v old", p, time.Since(fi.ModTime()).Round(time.Hour))
		}
	}
	out, err := exec.Command("find", root, "-mtime", "+15", "-printf", "%P\n").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(out)); strings.Join(got, " ") != "old old/a.txt" && strings.Join(got, " ") != "old/a.txt old" {
		t.Errorf("find -mtime +15 saw %v, want old and old/a.txt", got)
	}
}

func TestMaterializeRefusesBadAges(t *testing.T) {
	for name, e := range map[string]Entry{
		"negative": {Path: "f", Kind: "file", Age: -1},
		"link":     {Path: "l", Kind: "link", Target: "f", Age: 60},
	} {
		if err := Materialize(t.TempDir(), []Entry{e}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// An aged entry whose path a later link entry escapes through stays inside
// the root: the age is set through os.Root.
func TestMaterializeAgeStaysInRoot(t *testing.T) {
	root, outside, victim := arena(t)
	old, _ := os.Lstat(victim)
	fx := []Entry{
		{Path: "esc", Kind: "link", Target: outside},
		{Path: "esc/victim", Kind: "file", Data: "x", Age: 3600},
	}
	if err := Materialize(root, fx); err == nil {
		t.Error("a file written through a link out of the root was accepted")
	}
	if now, _ := os.Lstat(victim); !now.ModTime().Equal(old.ModTime()) {
		t.Error("the victim's times were changed through the link")
	}
	outsideUntouched(t, outside, victim)
}
