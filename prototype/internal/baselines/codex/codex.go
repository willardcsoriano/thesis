// Package codex is a port of the POSIX dangerous-command check in OpenAI's Codex CLI,
// used as an external baseline for the recoverability evaluation. It is not part of
// SynapseOS's own safety path.
//
// Source: github.com/openai/codex, codex-rs/shell-command/src/command_safety/
// is_dangerous_command.rs and codex-rs/shell-command/src/bash.rs at commit
// 1f4c47343a1bff2d8cddc429c5d39503fb5a6c30 (2026-09-01), Apache-2.0. The logic below
// follows those files function by function; the Windows-only checks are omitted.
// Codex parses with tree-sitter-bash; this port uses mvdan.cc/sh, which this module
// already depends on. Codex's own unit tests are ported in codex_test.go.
package codex

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Match mirrors Codex's DangerousCommandMatch.
type Match int

const (
	None Match = iota
	ForcedRm
	Other
)

const maxWrapperDepth = 8

// Dangerous reports what Codex's check makes of a command given as an argv.
func Dangerous(command []string) Match { return matchWithDepth(command, 0) }

// DangerousScript evaluates a shell command line the way Codex receives a model's
// shell call: as ["bash", "-lc", script].
func DangerousScript(script string) Match {
	return Dangerous([]string{"bash", "-lc", script})
}

func matchWithDepth(command []string, depth int) Match {
	if depth > maxWrapperDepth {
		return Other
	}
	if m := matchForExec(command, depth); m != None {
		return m
	}
	if cmds, ok := shellLcLiteralCommands(command); ok {
		for _, c := range cmds {
			if m := matchWithDepth(c, depth+1); m != None {
				return m
			}
		}
	}
	return None
}

func matchForExec(command []string, depth int) Match {
	if len(command) == 0 {
		return None
	}
	name := command[0]
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	switch name {
	case "rm":
		if forceOption(command[1:]) {
			return ForcedRm
		}
	case "sudo":
		return matchWithDepth(command[1:], depth+1)
	case "env":
		return matchForEnv(command, depth)
	case "trap":
		return matchForTrap(command, depth)
	}
	return None
}

func matchForEnv(command []string, depth int) Match {
	i := 1
	for i < len(command) {
		arg := command[i]
		if arg == "--" {
			i++
			break
		}
		if arg == "-i" || arg == "--ignore-environment" {
			i++
			continue
		}
		if k, _, ok := strings.Cut(arg, "="); ok && k != "" && !strings.HasPrefix(k, "-") {
			i++
			continue
		}
		break
	}
	if i > len(command) {
		i = len(command)
	}
	return matchWithDepth(command[i:], depth+1)
}

func matchForTrap(command []string, depth int) Match {
	i := 1
	if len(command) > i && command[i] == "--" {
		i++
	}
	if len(command) <= i || strings.HasPrefix(command[i], "-") {
		return None
	}
	return matchWithDepth([]string{"sh", "-c", command[i]}, depth+1)
}

func forceOption(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--force" {
			return true
		}
		if flags, ok := strings.CutPrefix(a, "-"); ok && !strings.HasPrefix(flags, "-") && strings.Contains(flags, "f") {
			return true
		}
	}
	return false
}

// shellLcLiteralCommands mirrors parse_shell_lc_literal_commands: for [sh|bash|zsh,
// -c|-lc, script] it returns every simple command in the script, each reduced to its
// literal words. A script that does not parse yields ok=false, as in Codex.
func shellLcLiteralCommands(command []string) ([][]string, bool) {
	if len(command) != 3 || (command[1] != "-c" && command[1] != "-lc") {
		return nil, false
	}
	switch filepath.Base(command[0]) {
	case "bash", "zsh", "sh":
	default:
		return nil, false
	}
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command[2]), "")
	if err != nil {
		return nil, false
	}
	var out [][]string
	syntax.Walk(f, func(n syntax.Node) bool {
		if call, ok := n.(*syntax.CallExpr); ok && len(call.Args) > 0 {
			if words, ok := literalCommand(call); ok {
				out = append(out, words)
			}
		}
		return true
	})
	return out, true
}

// literalCommand mirrors parse_literal_command_from_node: the command name must be a
// literal word; later arguments that are not literal are skipped, not fatal.
func literalCommand(call *syntax.CallExpr) ([]string, bool) {
	name, ok := literalWord(call.Args[0])
	if !ok {
		return nil, false
	}
	words := []string{name}
	for _, w := range call.Args[1:] {
		if s, ok := literalWord(w); ok {
			words = append(words, s)
		}
	}
	return words, true
}

func literalWord(w *syntax.Word) (string, bool) {
	var b strings.Builder
	for i, p := range w.Parts {
		switch p := p.(type) {
		case *syntax.Lit:
			if strings.ContainsAny(p.Value, "{}*?[]\\~^#$`") || (i == 0 && strings.HasPrefix(p.Value, "=")) {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			if p.Dollar {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			if p.Dollar {
				return "", false
			}
			for _, q := range p.Parts {
				lit, ok := q.(*syntax.Lit)
				if !ok {
					return "", false
				}
				for _, esc := range []string{`\$`, "\\`", `\"`, `\\`, "\\\n"} {
					if strings.Contains(lit.Value, esc) {
						return "", false
					}
				}
				b.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}
