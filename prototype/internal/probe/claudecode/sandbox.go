package claudecode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TokenEnv is the variable that carries the long-lived token from
// `claude setup-token` (NOTES.md, "Authentication"). It reaches the CLI
// through the environment only, never argv or a file.
const TokenEnv = "CLAUDE_CODE_OAUTH_TOKEN"

// Paths inside the sandbox. They are fixed, so nothing in the agent's view
// names the operator's home, username or temp folders.
const (
	SandboxHome = "/home/agent"
	SandboxCwd  = SandboxHome + "/task"
	toolRoot    = "/run/agentprobe"
	toolPkg     = toolRoot + "/claude-code"
	toolNode    = toolRoot + "/node"
	sandboxUser = "agent"
	sandboxHost = "agentprobe"
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
//	Root/home        mounted read-write at SandboxHome (the CLI's HOME)
//	Root/home/task   the fixture, the CLI's working directory
//	Root/etc         passwd and group naming the uid "agent"
type Workdir struct{ Root string }

func (w Workdir) Home() string    { return filepath.Join(w.Root, "home") }
func (w Workdir) Fixture() string { return filepath.Join(w.Root, "home", "task") }
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
	for _, d := range []string{w.Fixture(), filepath.Join(root, "etc")} {
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
	Bwrap   string // bubblewrap binary
	Strace  string // strace binary; empty disables execve capture
	Install Install
}

// Argv is the full host command line: optionally strace, then bwrap, then
// inner (the program and its arguments, paths as seen inside the sandbox).
// It never contains a secret; the token goes in Env.
//
// Mounts, in order: the host root read-only; a fresh /dev and /proc; an
// empty tmpfs over every hidden directory that exists; resolv.conf's target
// re-exposed if it lived under one; the workdir's home read-write at
// SandboxHome; synthetic passwd and group; the CLI's package read-only. All
// namespaces are unshared except the network, which the CLI needs.
func (s Sandbox) Argv(w Workdir, straceOut string, inner []string) []string {
	var argv []string
	if s.Strace != "" && straceOut != "" {
		// No -v: without it strace abbreviates envp to a count, so the
		// token never reaches the trace file. -s 256 keeps argv[0] whole.
		argv = append(argv, s.Strace, "-f", "-qq", "-s", "256", "-e", "trace=execve", "-o", straceOut, "--")
	}
	argv = append(argv, s.Bwrap,
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc")
	for _, d := range hidden {
		if fi, err := os.Lstat(d); err == nil && fi.IsDir() {
			argv = append(argv, "--tmpfs", d)
		}
	}
	if t, err := filepath.EvalSymlinks("/etc/resolv.conf"); err == nil && t != "/etc/resolv.conf" && underHidden(t) {
		argv = append(argv, "--ro-bind", t, t)
	}
	argv = append(argv,
		"--bind", w.Home(), SandboxHome,
		"--ro-bind", w.etc("passwd"), "/etc/passwd",
		"--ro-bind", w.etc("group"), "/etc/group",
		"--ro-bind", s.Install.PackageDir, toolPkg)
	if s.Install.NodePrefix != "" {
		argv = append(argv, "--ro-bind", s.Install.NodePrefix, toolNode)
	}
	argv = append(argv,
		"--unshare-all", "--share-net",
		"--hostname", sandboxHost,
		"--die-with-parent", "--new-session",
		"--chdir", SandboxCwd,
		"--")
	return append(argv, inner...)
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
	if strings.HasPrefix(prompt, "-") {
		return nil, errors.New("a prompt starting with '-' would be read as a flag")
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

// ShellQuote renders argv for display.
func ShellQuote(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r == '/' || r == '-' || r == '_' || r == '.' || r == '=' || r == ':' || r == ',' ||
				r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
		}) < 0 {
			q[i] = a
		} else {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(q, " ")
}
