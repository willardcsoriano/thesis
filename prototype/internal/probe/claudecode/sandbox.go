package claudecode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"synapseos/internal/probe"
)

// TokenEnv is the variable that carries the long-lived token from
// `claude setup-token` (NOTES.md, "Authentication"). It reaches the CLI
// through the environment only, never argv or a file.
const TokenEnv = "CLAUDE_CODE_OAUTH_TOKEN"

// Paths inside the sandbox. They are fixed, so nothing in the agent's view
// names the operator's home, username or temp folders.
//
// The fixture is the agent's home folder and its working directory
// (probe.LayoutHome), so "my downloads" is ~/Downloads as on a person's own
// machine. The CLI's own files (sessions, settings, .claude.json) go to
// SandboxState instead, through the CLI's config-folder variable, so the
// fixture holds only what the task and the agent put there.
const (
	SandboxHome  = "/home/agent"
	SandboxCwd   = SandboxHome
	SandboxState = "/run/agentprobe-state"
	// ConfigDirEnv moves every file Claude Code keeps under ~ (~/.claude and
	// ~/.claude.json) into its value; checked against 2.1.310 by running it
	// with an empty HOME, which stayed empty.
	ConfigDirEnv = "CLAUDE_CONFIG_DIR"
	toolRoot     = "/run/agentprobe"
	toolPkg      = toolRoot + "/claude-code"
	toolNode     = toolRoot + "/node"
	sandboxUser  = "agent"
	sandboxHost  = "agentprobe"
)

// hidden are host directories replaced by an empty tmpfs: the operator's
// homes, other sessions' temp files, runtime sockets (/run/user holds the
// D-Bus, SSH-agent and Claude Code sockets), and removable media.
var hidden = []string{"/home", "/root", "/tmp", "/run", "/var/tmp", "/mnt", "/media", "/srv"}

// Install is where the host's Claude Code lives, resolved from PATH.
type Install struct {
	PackageDir string // host directory mounted read-only at toolPkg
	Exe        string // executable, relative to PackageDir
	NodePrefix string // host node prefix, mounted only when Exe is a script
}

// ResolveInstall finds the CLI behind `claude` on PATH. A package directory
// (one with package.json naming @anthropic-ai/claude-code) is mounted whole,
// so files the binary loads relative to itself are there; otherwise only
// the executable's directory is. A script executable also needs node.
func ResolveInstall() (Install, error) {
	p, err := exec.LookPath("claude")
	if err != nil {
		return Install{}, fmt.Errorf("claude is not on PATH: %w", err)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return Install{}, err
	}
	in := Install{PackageDir: filepath.Dir(real), Exe: filepath.Base(real)}
	for d := filepath.Dir(real); d != filepath.Dir(d); d = filepath.Dir(d) {
		if isClaudePackage(d) {
			rel, err := filepath.Rel(d, real)
			if err != nil {
				return Install{}, err
			}
			in.PackageDir, in.Exe = d, rel
			break
		}
	}
	if !isELF(real) {
		node, err := exec.LookPath("node")
		if err != nil {
			return Install{}, fmt.Errorf("%s is a script and node is not on PATH: %w", real, err)
		}
		nodeReal, err := filepath.EvalSymlinks(node)
		if err != nil {
			return Install{}, err
		}
		in.NodePrefix = filepath.Dir(filepath.Dir(nodeReal))
	}
	return in, nil
}

// Freeze copies a CLI's package directory to a private folder under the
// operator's cache and returns it, with a function that deletes it. A batch
// mounts the copy, so nothing that updates or reinstalls the CLI on the host
// while the batch runs (an auto-update, npm, another session) can change or
// break the instrument between tasks: on 2026-10-24 a reinstall of the live
// package during batch2 made 24 of its 60 task runs fail to start the CLI.
// The copy is made with cp -a, so modes and links are kept.
func Freeze(pkgDir string) (string, func(), error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(filepath.Join(base, "agentprobe"), 0o700); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(filepath.Join(base, "agentprobe"), "cli-")
	if err != nil {
		return "", nil, err
	}
	done := func() { _ = os.RemoveAll(dir) }
	dst := filepath.Join(dir, "pkg")
	if out, err := exec.Command("cp", "-a", "--", pkgDir, dst).CombinedOutput(); err != nil {
		done()
		return "", nil, fmt.Errorf("freezing %s: %v: %s", pkgDir, err, bytes.TrimSpace(out))
	}
	return dst, done, nil
}

func isClaudePackage(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Name string `json:"name"`
	}
	return json.Unmarshal(b, &pkg) == nil && pkg.Name == "@anthropic-ai/claude-code"
}

func isELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	n, _ := f.Read(head)
	return n == 4 && bytes.Equal(head, []byte("\x7fELF"))
}

// Workdir is one task's throwaway host folder:
//
//	Root/home        the fixture, mounted read-write at SandboxHome: the
//	                 CLI's HOME and working directory
//	Root/state       the CLI's own files, read-write at SandboxState
//	Root/etc         passwd and group naming the uid "agent", and the
//	                 isolated network's resolv.conf
//	Root/lifeline    locked by the driver while an isolated sandbox runs
type Workdir struct{ Root string }

func (w Workdir) Home() string     { return filepath.Join(w.Root, "home") }
func (w Workdir) Fixture() string  { return w.Home() }
func (w Workdir) State() string    { return filepath.Join(w.Root, "state") }
func (w Workdir) Lifeline() string { return filepath.Join(w.Root, "lifeline") }
func (w Workdir) etc(f string) string {
	return filepath.Join(w.Root, "etc", f)
}

// NewWorkdir creates the folders and the identity files. The fixture is
// left empty for the caller to materialize.
func NewWorkdir() (Workdir, error) {
	root, err := os.MkdirTemp("", "agentprobe-")
	if err != nil {
		return Workdir{}, err
	}
	w := Workdir{Root: root}
	for _, d := range []string{w.Fixture(), w.State(), filepath.Join(root, "etc")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return w, err
		}
	}
	uid, gid := os.Getuid(), os.Getgid()
	passwd := fmt.Sprintf("root:x:0:0:root:/root:/bin/bash\n%s:x:%d:%d:agent:%s:/bin/bash\nnobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n", sandboxUser, uid, gid, SandboxHome)
	group := fmt.Sprintf("root:x:0:\n%s:x:%d:\nnogroup:x:65534:\n", sandboxUser, gid)
	if err := os.WriteFile(w.etc("passwd"), []byte(passwd), 0o644); err != nil {
		return w, err
	}
	if err := os.WriteFile(w.etc("resolv.conf"), []byte("nameserver "+DNSForward+"\n"), 0o644); err != nil {
		return w, err
	}
	return w, os.WriteFile(w.etc("group"), []byte(group), 0o644)
}

// Remove deletes the workdir, first making every directory writable so a
// fixture that removed its own permissions cannot block cleanup.
func (w Workdir) Remove() error {
	_ = filepath.WalkDir(w.Root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	return os.RemoveAll(w.Root)
}

// Sandbox is how the CLI is run.
type Sandbox struct {
	Bwrap   string  // bubblewrap binary
	Strace  string  // strace binary; empty disables execve capture
	Prlimit string  // prlimit binary, which applies Limits inside the sandbox; RunTask refuses to run without it
	Net     Network // the zero value is isolated, which RunTask refuses until ResolveNetwork has filled it
	Install Install
	// Mask are further host directories hidden behind an empty tmpfs, for
	// what lies outside the hidden directories yet must not be readable:
	// the runner adds its own output folder, whose raw streams hold earlier
	// tasks' traffic.
	Mask []string
}

// Argv is the full host command line (see Jail.Argv) with the CLI's
// package, and node when the CLI is a script, mounted read-only. It never
// contains a secret; the token goes in Env.
func (s Sandbox) Argv(w Workdir, straceOut string, inner []string) []string {
	ro := []Mount{{s.Install.PackageDir, toolPkg}}
	if s.Install.NodePrefix != "" {
		ro = append(ro, Mount{s.Install.NodePrefix, toolNode})
	}
	return Jail{Bwrap: s.Bwrap, Strace: s.Strace, Prlimit: s.Prlimit, Net: s.Net, Mask: s.Mask, ReadOnly: ro}.Argv(w, straceOut, inner)
}

// Mount is a host path bound at another path inside the sandbox.
type Mount struct{ Host, At string }

// Limits inside the sandbox. They are set in the sandbox's own user
// namespace, where RLIMIT_NPROC counts only the sandbox's processes and
// threads, not the operator's (set on bubblewrap from outside it would
// count every process the operator runs).
const (
	MaxProcs  = 1024      // processes and threads: a fork bomb stops here
	TmpfsSize = 256 << 20 // bytes in each tmpfs the sandbox mounts, so filling one cannot fill the host's memory
)

// Jail is the sandbox both CLI drivers share: everything but the CLI's own
// files, which come in ReadOnly.
type Jail struct {
	Bwrap, Strace, Prlimit string
	Net                    Network
	Mask                   []string
	ReadOnly               []Mount
}

// Argv is the full host command line: with the isolated network, pasta's
// namespace and its setup (Network.wrap), then optionally strace, then
// bwrap, then, inside, prlimit and inner (the program and its arguments,
// paths as seen inside the sandbox). An isolated sandbox lives only while
// the caller holds w.Lifeline() (HoldLifeline); Exec does.
//
// Mounts, in order: the host root read-only; a fresh /dev, read-only but
// for a size-capped /dev/shm; a fresh /proc; a size-capped empty tmpfs over
// every hidden directory that exists and every Mask directory outside them;
// with the host network, resolv.conf's target re-exposed if it lived under
// one, and with the isolated network, the workdir's resolv.conf naming
// DNSForward in its place; the workdir's home read-write at SandboxHome and
// its state folder at SandboxState;
// synthetic passwd and group; ReadOnly. All namespaces are unshared except
// the network, which is pasta's (isolated) or the host's, and the sandbox
// cannot create further user namespaces, so it cannot remount its view.
// Isolated, bwrap runs as root of pasta's user namespace, so it is told the
// operator's uid and gid for the agent and to drop every capability.
// prlimit caps processes at MaxProcs and core dumps at zero bytes, so a
// crash cannot write the CLI's memory, token included, to disk.
func (j Jail) Argv(w Workdir, straceOut string, inner []string) []string {
	var argv []string
	isolated := j.Net.Isolated()
	if isolated {
		argv = j.Net.wrap(w.Lifeline())
	}
	if j.Strace != "" && straceOut != "" {
		// No -v: without it strace abbreviates envp to a count, so the
		// token never reaches the trace file. -s 256 keeps argv[0] whole.
		argv = append(argv, j.Strace, "-f", "-qq", "-s", "256", "-e", "trace=execve", "-o", straceOut, "--")
	}
	size := strconv.Itoa(TmpfsSize)
	argv = append(argv, j.Bwrap,
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--size", size, "--tmpfs", "/dev/shm",
		"--remount-ro", "/dev",
		"--proc", "/proc")
	for _, d := range append(slices.Clone(hidden), j.Mask...) {
		if d != filepath.Clean(d) || !filepath.IsAbs(d) || d == "/" || underHidden(d) && !slices.Contains(hidden, d) {
			continue // relative, unclean, the root itself, or already hidden
		}
		if fi, err := os.Lstat(d); err == nil && fi.IsDir() {
			argv = append(argv, "--size", size, "--tmpfs", d)
		}
	}
	t, err := filepath.EvalSymlinks("/etc/resolv.conf")
	switch {
	case isolated:
		// Over the file the resolver reads, which is the link's target
		// when /etc/resolv.conf is a link.
		dst := "/etc/resolv.conf"
		if err == nil {
			dst = t
		}
		argv = append(argv, "--ro-bind", w.etc("resolv.conf"), dst)
	case err == nil && t != "/etc/resolv.conf" && underHidden(t):
		argv = append(argv, "--ro-bind", t, t)
	}
	argv = append(argv,
		"--bind", w.Home(), SandboxHome,
		"--bind", w.State(), SandboxState,
		"--ro-bind", w.etc("passwd"), "/etc/passwd",
		"--ro-bind", w.etc("group"), "/etc/group")
	for _, m := range j.ReadOnly {
		argv = append(argv, "--ro-bind", m.Host, m.At)
	}
	argv = append(argv,
		"--unshare-all", "--share-net",
		"--unshare-user", "--disable-userns")
	if isolated {
		argv = append(argv, "--uid", strconv.Itoa(os.Getuid()), "--gid", strconv.Itoa(os.Getgid()), "--cap-drop", "ALL")
	}
	argv = append(argv,
		"--hostname", sandboxHost,
		"--die-with-parent", "--new-session",
		"--chdir", SandboxCwd,
		"--")
	if j.Prlimit != "" {
		n := strconv.Itoa(MaxProcs)
		argv = append(argv, j.Prlimit, "--nproc="+n+":"+n, "--core=0:0", "--")
	}
	return append(argv, inner...)
}

// Prlimit is the prlimit binary, or empty when it is not installed. It runs
// inside the sandbox, so it must not live under a hidden directory.
func Prlimit() string {
	p, err := exec.LookPath("prlimit")
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil && underHidden(r) {
		return ""
	}
	return p
}

// MaskFor is the absolute, symlink-resolved form of dir for Sandbox.Mask,
// or "" when it is already hidden (it lies under a hidden directory) or
// cannot be resolved.
func MaskFor(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	if underHidden(abs) {
		return ""
	}
	return abs
}

func underHidden(p string) bool {
	for _, d := range hidden {
		if p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

// Exe is the CLI's path inside the sandbox.
func (s Sandbox) Exe() string { return filepath.Join(toolPkg, s.Install.Exe) }

// Env is the CLI's whole environment: nothing from the operator's shell
// except the token. bwrap passes it through unchanged.
func (s Sandbox) Env(token string) []string {
	path := "/usr/local/bin:/usr/bin:/bin"
	if s.Install.NodePrefix != "" {
		path = toolNode + "/bin:" + path
	}
	return []string{
		"PATH=" + path,
		"HOME=" + SandboxHome,
		"USER=" + sandboxUser,
		"LOGNAME=" + sandboxUser,
		"SHELL=/bin/bash",
		"TERM=dumb",
		"LANG=C.UTF-8", // fixed: the host locale would name the operator's region and vary between machines
		"DISABLE_AUTOUPDATER=1",
		ConfigDirEnv + "=" + SandboxState,
		TokenEnv + "=" + token,
	}
}

// Redacted is env with the token's value replaced, for printing.
func Redacted(env []string) []string {
	out := make([]string, len(env))
	for i, kv := range env {
		if strings.HasPrefix(kv, TokenEnv+"=") {
			kv = TokenEnv + "=<redacted>"
		}
		out[i] = kv
	}
	return out
}

// AllowedTools are approved up front, as a second line behind
// --dangerously-skip-permissions. CLAUDE_CODE_SUBPROCESS_ENV_SCRUB is not set:
// in the first live runs (CLI 2.1.302) it forced permission mode "default" and
// switched on the CLI's own Bash sandbox, which cannot start inside this one
// (it wanted socat), so no command ran. The scrub only hid the token from the
// agent's shell environment, and the agent can read it from /proc regardless
// (docs/agent-command-study.md, Known gaps); bubblewrap is the isolation.
var AllowedTools = []string{"Bash", "Read", "Write", "Edit", "NotebookEdit", "Glob", "Grep", "Task", "TodoWrite", "ToolSearch"}

// ClaudeArgs are the CLI's arguments for one turn. Turn 0 fixes the session
// id; later turns resume it, so follow-ups continue the same conversation.
func ClaudeArgs(prompt, sessionID string, turn int, model string, maxTurns int) ([]string, error) {
	if err := probe.CheckPrompt(prompt); err != nil {
		return nil, err
	}
	if err := CheckFlagValue("model", model); err != nil {
		return nil, err
	}
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions",
		"--allowedTools", strings.Join(AllowedTools, ",")}
	if model != "" {
		args = append(args, "--model", model)
	}
	if maxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprint(maxTurns))
	}
	if turn == 0 {
		args = append(args, "--session-id", sessionID)
	} else {
		args = append(args, "--resume", sessionID)
	}
	return append(args, prompt), nil
}

// CheckFlagValue refuses a flag's value that the CLI would misread: one
// starting with '-' (taken as the next flag) or holding a NUL.
func CheckFlagValue(flag, v string) error {
	if strings.HasPrefix(v, "-") || strings.IndexByte(v, 0) >= 0 {
		return fmt.Errorf("--%s %q would not reach the CLI as a value", flag, v)
	}
	return nil
}

// ShellQuote renders argv for display.
func ShellQuote(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		switch {
		case a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r == '/' || r == '-' || r == '_' || r == '.' || r == '=' || r == ':' || r == ',' ||
				r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
		}) < 0:
			q[i] = a
		case strings.IndexFunc(a, func(r rune) bool { return r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0:
			// Bash's $'...' with every unprintable character escaped, so a
			// prompt cannot drive the terminal a dry run is printed to and
			// the line still pastes back as the same argument.
			var b strings.Builder
			b.WriteString("$'")
			for j := 0; j < len(a); {
				r, n := utf8.DecodeRuneInString(a[j:])
				switch {
				case r == utf8.RuneError && n <= 1:
					fmt.Fprintf(&b, `\x%02x`, a[j])
				case r == '\'' || r == '\\':
					b.WriteByte('\\')
					b.WriteRune(r)
				case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
					fmt.Fprintf(&b, `\u%04x`, r)
				default:
					b.WriteString(a[j : j+n])
				}
				j += n
			}
			q[i] = b.String() + "'"
		default:
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(q, " ")
}
