package geminicli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"synapseos/internal/probe"
	"synapseos/internal/probe/claudecode"
)

// APIKeyEnv carries an AI Studio key (NOTES.md, "Authentication"). It
// reaches the CLI through the environment only, never argv or a file.
const APIKeyEnv = "GEMINI_API_KEY"

// ProjectEnv names a Google Cloud project. Personal Google accounts do not
// need one; Workspace and Code Assist licences do. Passed through when set.
const ProjectEnv = "GOOGLE_CLOUD_PROJECT"

// Auth types, as the CLI's security.auth.selectedType setting names them.
const (
	AuthOAuthPersonal = "oauth-personal" // "Sign in with Google": the personal subscription
	AuthAPIKey        = "gemini-api-key" // AI Studio key, billed or free tier, not the subscription
)

// Paths inside the sandbox. They are fixed, so nothing in the agent's view
// names the operator's home, username or temp folders. The task workdir is
// claudecode's, so the layout is the same for both CLI drivers.
const (
	SandboxHome = claudecode.SandboxHome
	SandboxCwd  = claudecode.SandboxCwd
	toolRoot    = "/run/agentprobe"
	toolPkg     = toolRoot + "/gemini-cli"
	toolNode    = toolRoot + "/node/bin/node"
	sandboxUser = "agent"
	sandboxHost = "agentprobe"
)

// Install is where the host's Gemini CLI lives, resolved from PATH.
type Install struct {
	PackageDir string // host directory mounted read-only at toolPkg
	Exe        string // entry point, relative to PackageDir
	Node       string // host node binary, mounted read-only when Exe is a script
	Version    string // package.json version; the stream does not carry it
}

// ResolveInstall finds the CLI behind `gemini` on PATH. The package
// directory (the one whose package.json names @google/gemini-cli) is mounted
// whole, so the bundle's chunks and native modules are there. The entry
// point is a node script, so node is resolved too.
func ResolveInstall() (Install, error) {
	p, err := exec.LookPath("gemini")
	if err != nil {
		return Install{}, fmt.Errorf("gemini is not on PATH: %w", err)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return Install{}, err
	}
	var in Install
	for d := filepath.Dir(real); d != filepath.Dir(d); d = filepath.Dir(d) {
		if v, ok := geminiPackage(d); ok {
			rel, err := filepath.Rel(d, real)
			if err != nil {
				return Install{}, err
			}
			in = Install{PackageDir: d, Exe: rel, Version: v}
			break
		}
	}
	if in.PackageDir == "" {
		return Install{}, fmt.Errorf("%s is not inside an @google/gemini-cli package", real)
	}
	if !isELF(real) {
		node, err := exec.LookPath("node")
		if err != nil {
			return Install{}, fmt.Errorf("%s is a script and node is not on PATH: %w", real, err)
		}
		if in.Node, err = filepath.EvalSymlinks(node); err != nil {
			return Install{}, err
		}
	}
	return in, nil
}

func geminiPackage(dir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return "", false
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &pkg) != nil || pkg.Name != "@google/gemini-cli" {
		return "", false
	}
	return pkg.Version, true
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

// Auth is the one credential the sandboxed CLI gets. Exactly one of APIKey
// and OAuthCreds is set.
type Auth struct {
	APIKey     string // GEMINI_API_KEY, placed in the child's environment only
	OAuthCreds []byte // contents of an oauth_creds.json, copied into each task's throwaway home
	Project    string // GOOGLE_CLOUD_PROJECT, optional
}

// Secrets are the values the run must never write to disk: the API key,
// and the tokens inside the OAuth credential (what the agent would print if
// it read the file).
func (a Auth) Secrets() []string {
	var out []string
	if a.APIKey != "" {
		out = append(out, a.APIKey)
	}
	var creds map[string]any
	if json.Unmarshal(a.OAuthCreds, &creds) == nil {
		for _, k := range []string{"access_token", "refresh_token", "id_token"} {
			if v, ok := creds[k].(string); ok && v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// Type is the auth type the CLI is told to use, or "" when none is set.
func (a Auth) Type() string {
	switch {
	case len(a.OAuthCreds) > 0:
		return AuthOAuthPersonal
	case a.APIKey != "":
		return AuthAPIKey
	}
	return ""
}

// Validate refuses a missing or ambiguous credential.
func (a Auth) Validate() error {
	if len(a.OAuthCreds) > 0 && a.APIKey != "" {
		return fmt.Errorf("both an OAuth credential file and %s are set; pass only one", APIKeyEnv)
	}
	if a.Type() == "" {
		return errors.New("no credential configured")
	}
	return nil
}

// maxCredsSize bounds the credential file read; a real one is well under 4 KiB.
const maxCredsSize = 64 << 10

// LoadOAuthCreds reads the credential file the CLI writes after "Sign in
// with Google" (oauth_creds.json). It checks only that the file is a JSON
// object holding a refresh token; errors never quote the file's contents.
func LoadOAuthCreds(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxCredsSize {
		return nil, fmt.Errorf("%s is not a credential file (not a regular file under %d bytes)", path, maxCredsSize)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var creds struct {
		RefreshToken string `json:"refresh_token"`
	}
	if json.Unmarshal(b, &creds) != nil || creds.RefreshToken == "" {
		return nil, fmt.Errorf("%s is not a Gemini CLI OAuth credential file (no refresh_token in a JSON object)", path)
	}
	return b, nil
}

// Settings is the user settings.json written into each task's throwaway
// home: the auth type, auto-update off (the install is read-only anyway),
// and the per-invocation turn limit when maxTurns > 0. Nothing else from
// the operator's ~/.gemini is used.
func Settings(authType string, maxTurns int) []byte {
	s := map[string]any{
		"security": map[string]any{"auth": map[string]any{"selectedType": authType}},
		"general":  map[string]any{"enableAutoUpdate": false, "enableAutoUpdateNotification": false},
	}
	if maxTurns > 0 {
		s["model"] = map[string]any{"maxSessionTurns": maxTurns}
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return append(b, '\n')
}

// configDir is the CLI's user directory inside the throwaway home.
func configDir(w claudecode.Workdir) string { return filepath.Join(w.Home(), ".gemini") }

// PrepareHome writes the CLI's user directory into w's home: settings.json
// and, for the OAuth type, the credential copy. Both are mode 0600 in a
// directory only the operator can enter (the workdir is 0700). The copy is
// deleted with the workdir.
func PrepareHome(w claudecode.Workdir, a Auth, maxTurns int) error {
	dir := configDir(w)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), Settings(a.Type(), maxTurns), 0o600); err != nil {
		return err
	}
	if len(a.OAuthCreds) > 0 {
		return os.WriteFile(filepath.Join(dir, "oauth_creds.json"), a.OAuthCreds, 0o600)
	}
	return nil
}

// Sandbox is how the CLI is run.
type Sandbox struct {
	Bwrap   string             // bubblewrap binary
	Strace  string             // strace binary; empty disables execve capture
	Prlimit string             // prlimit binary (claudecode.Prlimit); RunTask refuses to run without it
	Net     claudecode.Network // see claudecode.Sandbox.Net
	Install Install
	Mask    []string // further host directories hidden behind an empty tmpfs; see claudecode.Sandbox
}

// Argv is the full host command line (claudecode.Jail.Argv: the same
// sandbox as the Claude Code driver's) with the CLI's package and the node
// binary mounted read-only. It never contains a secret; the API key goes in
// Env and the OAuth credential in the throwaway home.
func (s Sandbox) Argv(w claudecode.Workdir, straceOut string, inner []string) []string {
	ro := []claudecode.Mount{{Host: s.Install.PackageDir, At: toolPkg}}
	if s.Install.Node != "" {
		ro = append(ro, claudecode.Mount{Host: s.Install.Node, At: toolNode})
	}
	return claudecode.Jail{Bwrap: s.Bwrap, Strace: s.Strace, Prlimit: s.Prlimit, Net: s.Net, Mask: s.Mask, ReadOnly: ro}.Argv(w, straceOut, inner)
}

// Command is the CLI's invocation inside the sandbox, before its arguments:
// node and the entry script, or the entry itself when it is a binary.
func (s Sandbox) Command() []string {
	exe := filepath.Join(toolPkg, s.Install.Exe)
	if s.Install.Node != "" {
		return []string{toolNode, exe}
	}
	return []string{exe}
}

// Env is the CLI's whole environment: nothing from the operator's shell
// except the credential variables. bwrap passes it through unchanged.
//
// NO_BROWSER=true makes a missing or expired login fail at once
// ("Manual authorization is required but the current session is
// non-interactive") instead of waiting at a sign-in prompt.
func (s Sandbox) Env(a Auth) []string {
	env := []string{
		"PATH=" + filepath.Dir(toolNode) + ":/usr/local/bin:/usr/bin:/bin",
		"HOME=" + SandboxHome,
		"USER=" + sandboxUser,
		"LOGNAME=" + sandboxUser,
		"SHELL=/bin/bash",
		"TERM=dumb",
		"LANG=C.UTF-8", // fixed: the host locale would name the operator's region and vary between machines
		"NO_BROWSER=true",
	}
	if a.APIKey != "" {
		env = append(env, APIKeyEnv+"="+a.APIKey)
	}
	if a.Project != "" {
		env = append(env, ProjectEnv+"="+a.Project)
	}
	return env
}

// Redacted is env with the API key's value replaced, for printing.
func Redacted(env []string) []string {
	out := make([]string, len(env))
	for i, kv := range env {
		if strings.HasPrefix(kv, APIKeyEnv+"=") {
			kv = APIKeyEnv + "=<redacted>"
		}
		out[i] = kv
	}
	return out
}

// GeminiArgs are the CLI's arguments for one turn. Turn 0 fixes the session
// id; later turns resume it, so follow-ups continue the same conversation.
// Approvals are off (yolo): the sandbox is the isolation. --skip-trust
// trusts the task folder for the session, without which headless mode
// refuses to start in an untrusted folder. debug adds -d, which makes the
// shell tool's result carry its exit code (NOTES.md, "Stream schema").
func GeminiArgs(prompt, sessionID string, turn int, model string, debug bool) ([]string, error) {
	if err := probe.CheckPrompt(prompt); err != nil {
		return nil, err
	}
	if err := claudecode.CheckFlagValue("model", model); err != nil {
		return nil, err
	}
	args := []string{"--output-format", "stream-json", "--approval-mode", "yolo", "--skip-trust"}
	if model != "" {
		args = append(args, "--model", model)
	}
	if debug {
		args = append(args, "--debug")
	}
	if turn == 0 {
		args = append(args, "--session-id", sessionID)
	} else {
		args = append(args, "--resume", sessionID)
	}
	return append(args, "--prompt", prompt), nil
}
