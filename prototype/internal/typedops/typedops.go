// Package typedops defines a small, fixed set of typed file operations —
// the schema Layer 4 (testing-plan.md, build-order.md F4) offers the model
// via Ollama's tool-calling API as an alternative to raw bash strings. Each
// op is dispatched directly through Go's os/io stdlib: no MCP server, no
// Node.js dependency, no shell subprocess. This package answers the F4
// question empirically (is a typed path more reliable than raw-bash +
// classifier) — it is deliberately NOT wired into runLoop; see
// cmd/synapse/layer4_test.go for the experiment that decides whether it
// should be.
package typedops

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"synapseos/internal/classifier"
	"synapseos/internal/ollama"
)

// Result is what a dispatched Op actually did, for task-success scoring —
// the set of paths it read, moved, deleted, renamed, or created.
type Result struct {
	AffectedPaths []string
}

// Op is one supported typed operation: its JSON-Schema parameters (the
// shape both Ollama's tools API and this package's freeform-JSON fallback
// parser expect), a safety classifier mirroring internal/classifier's
// Reversible/Irreversible contract, and the dispatch function that actually
// performs it. Classify can inspect filesystem state via args the same way
// classifier.ClassifyForDir does for cp — a structural advantage a raw
// string never has: it knows dst is a path argument, not a substring to
// pattern-match.
type Op struct {
	Name        string
	Description string
	Parameters  map[string]any
	Classify    func(args map[string]any) (classifier.Verdict, string)
	Dispatch    func(args map[string]any) (Result, error)
}

// ToTool converts op into the shape Ollama's /api/chat tools field expects.
func (op Op) ToTool() ollama.Tool {
	return ollama.Tool{
		Type: "function",
		Function: ollama.ToolFunction{
			Name:        op.Name,
			Description: op.Description,
			Parameters:  op.Parameters,
		},
	}
}

// Registry is the fixed set of typed operations offered to the model, sized
// to Layer 4's scope (testing-plan.md's file-manipulation task category),
// not a general-purpose typed filesystem API.
var Registry = []Op{findFilesOp, moveFilesOp, deleteFilesOp, renameFilesOp, copyFileOp}

// Tools returns Registry converted to Ollama's tool-calling schema, ready
// to pass to Client.Chat.
func Tools() []ollama.Tool {
	tools := make([]ollama.Tool, len(Registry))
	for i, op := range Registry {
		tools[i] = op.ToTool()
	}
	return tools
}

// Lookup returns the Op named name, or nil if it isn't in Registry.
func Lookup(name string) *Op {
	for i := range Registry {
		if Registry[i].Name == name {
			return &Registry[i]
		}
	}
	return nil
}

func stringArg(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("argument %q must be a non-empty string", key)
	}
	return s, nil
}

// intArg reads an optional integer argument, returning fallback when
// absent. JSON numbers decode as float64 through encoding/json's
// map[string]any, which is the shape both Chat's ToolCall.Arguments and the
// freeform-JSON fallback parser produce.
func intArg(args map[string]any, key string, fallback int) (int, error) {
	v, ok := args[key]
	if !ok {
		return fallback, nil
	}
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, fmt.Errorf("argument %q must be a number", key)
	}
}

var findFilesOp = Op{
	Name:        "find_files",
	Description: "List files directly under a directory whose name matches a glob pattern, optionally limited to files modified within the last N days.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":                  map[string]any{"type": "string", "description": "Directory to search"},
			"pattern":              map[string]any{"type": "string", "description": "Glob pattern to match filenames against, e.g. *.pdf"},
			"modified_within_days": map[string]any{"type": "integer", "description": "Only include files modified within this many days; omit for no limit"},
		},
		"required": []string{"dir", "pattern"},
	},
	Classify: func(args map[string]any) (classifier.Verdict, string) {
		return classifier.Reversible, "" // read-only
	},
	Dispatch: func(args map[string]any) (Result, error) {
		dir, err := stringArg(args, "dir")
		if err != nil {
			return Result{}, err
		}
		pattern, err := stringArg(args, "pattern")
		if err != nil {
			return Result{}, err
		}
		withinDays, err := intArg(args, "modified_within_days", 0)
		if err != nil {
			return Result{}, err
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return Result{}, fmt.Errorf("read dir %s: %w", dir, err)
		}

		cutoff := time.Now().AddDate(0, 0, -withinDays)
		var matches []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ok, err := filepath.Match(pattern, e.Name())
			if err != nil {
				return Result{}, fmt.Errorf("bad pattern %q: %w", pattern, err)
			}
			if !ok {
				continue
			}
			if withinDays > 0 {
				info, err := e.Info()
				if err != nil || info.ModTime().Before(cutoff) {
					continue
				}
			}
			matches = append(matches, filepath.Join(dir, e.Name()))
		}
		return Result{AffectedPaths: matches}, nil
	},
}

var moveFilesOp = Op{
	Name:        "move_files",
	Description: "Move every file directly under source_dir whose name matches a glob pattern into dest_dir, creating dest_dir if it doesn't exist.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_dir": map[string]any{"type": "string"},
			"pattern":    map[string]any{"type": "string", "description": "Glob pattern, e.g. *.png"},
			"dest_dir":   map[string]any{"type": "string"},
		},
		"required": []string{"source_dir", "pattern", "dest_dir"},
	},
	Classify: func(args map[string]any) (classifier.Verdict, string) {
		return classifier.Reversible, "" // a move, not a delete — internal/undo already covers moves/creates
	},
	Dispatch: func(args map[string]any) (Result, error) {
		src, err := stringArg(args, "source_dir")
		if err != nil {
			return Result{}, err
		}
		pattern, err := stringArg(args, "pattern")
		if err != nil {
			return Result{}, err
		}
		dst, err := stringArg(args, "dest_dir")
		if err != nil {
			return Result{}, err
		}

		if err := os.MkdirAll(dst, 0o755); err != nil {
			return Result{}, fmt.Errorf("create dest dir %s: %w", dst, err)
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return Result{}, fmt.Errorf("read source dir %s: %w", src, err)
		}

		var moved []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ok, err := filepath.Match(pattern, e.Name())
			if err != nil {
				return Result{AffectedPaths: moved}, fmt.Errorf("bad pattern %q: %w", pattern, err)
			}
			if !ok {
				continue
			}
			from := filepath.Join(src, e.Name())
			to := filepath.Join(dst, e.Name())
			if err := os.Rename(from, to); err != nil {
				return Result{AffectedPaths: moved}, fmt.Errorf("move %s: %w", from, err)
			}
			moved = append(moved, to)
		}
		return Result{AffectedPaths: moved}, nil
	},
}

var deleteFilesOp = Op{
	Name:        "delete_files",
	Description: "Delete every file directly under dir whose name matches a glob pattern and is older than older_than_days.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string"},
			"pattern":         map[string]any{"type": "string", "description": "Glob pattern, e.g. *.tmp"},
			"older_than_days": map[string]any{"type": "integer", "description": "Only delete files older than this many days; omit to delete all matches"},
		},
		"required": []string{"dir", "pattern"},
	},
	Classify: func(args map[string]any) (classifier.Verdict, string) {
		return classifier.Irreversible, "delete_files has no built-in undo, same as every rm-shaped command in internal/classifier"
	},
	Dispatch: func(args map[string]any) (Result, error) {
		dir, err := stringArg(args, "dir")
		if err != nil {
			return Result{}, err
		}
		pattern, err := stringArg(args, "pattern")
		if err != nil {
			return Result{}, err
		}
		olderThan, err := intArg(args, "older_than_days", 0)
		if err != nil {
			return Result{}, err
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return Result{}, fmt.Errorf("read dir %s: %w", dir, err)
		}

		cutoff := time.Now().AddDate(0, 0, -olderThan)
		var deleted []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ok, err := filepath.Match(pattern, e.Name())
			if err != nil {
				return Result{AffectedPaths: deleted}, fmt.Errorf("bad pattern %q: %w", pattern, err)
			}
			if !ok {
				continue
			}
			if olderThan > 0 {
				info, err := e.Info()
				if err != nil || info.ModTime().After(cutoff) {
					continue // not old enough
				}
			}
			p := filepath.Join(dir, e.Name())
			if err := os.Remove(p); err != nil {
				return Result{AffectedPaths: deleted}, fmt.Errorf("delete %s: %w", p, err)
			}
			deleted = append(deleted, p)
		}
		return Result{AffectedPaths: deleted}, nil
	},
}

var renameFilesOp = Op{
	Name:        "rename_files",
	Description: "Rename every file directly under dir with extension old_ext to new_ext (extensions given with or without a leading dot).",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":     map[string]any{"type": "string"},
			"old_ext": map[string]any{"type": "string", "description": "Extension to match, e.g. .jpeg or jpeg"},
			"new_ext": map[string]any{"type": "string", "description": "Replacement extension, e.g. .jpg or jpg"},
		},
		"required": []string{"dir", "old_ext", "new_ext"},
	},
	Classify: func(args map[string]any) (classifier.Verdict, string) {
		return classifier.Reversible, "" // a rename is a move under the hood
	},
	Dispatch: func(args map[string]any) (Result, error) {
		dir, err := stringArg(args, "dir")
		if err != nil {
			return Result{}, err
		}
		oldExt, err := stringArg(args, "old_ext")
		if err != nil {
			return Result{}, err
		}
		newExt, err := stringArg(args, "new_ext")
		if err != nil {
			return Result{}, err
		}
		if !strings.HasPrefix(oldExt, ".") {
			oldExt = "." + oldExt
		}
		if !strings.HasPrefix(newExt, ".") {
			newExt = "." + newExt
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return Result{}, fmt.Errorf("read dir %s: %w", dir, err)
		}

		var renamed []string
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != oldExt {
				continue
			}
			from := filepath.Join(dir, e.Name())
			to := filepath.Join(dir, strings.TrimSuffix(e.Name(), oldExt)+newExt)
			if err := os.Rename(from, to); err != nil {
				return Result{AffectedPaths: renamed}, fmt.Errorf("rename %s: %w", from, err)
			}
			renamed = append(renamed, to)
		}
		return Result{AffectedPaths: renamed}, nil
	},
}

var copyFileOp = Op{
	Name:        "copy_file",
	Description: "Copy a single file from src to dst.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"src": map[string]any{"type": "string"},
			"dst": map[string]any{"type": "string"},
		},
		"required": []string{"src", "dst"},
	},
	Classify: func(args map[string]any) (classifier.Verdict, string) {
		dst, err := stringArg(args, "dst")
		if err != nil {
			return classifier.Reversible, "" // malformed args: Dispatch will surface the real error
		}
		if _, err := os.Stat(dst); err == nil {
			return classifier.Irreversible, fmt.Sprintf("copy_file overwrites an existing file (%s already exists and would be replaced with no built-in undo)", dst)
		}
		return classifier.Reversible, ""
	},
	Dispatch: func(args map[string]any) (Result, error) {
		src, err := stringArg(args, "src")
		if err != nil {
			return Result{}, err
		}
		dst, err := stringArg(args, "dst")
		if err != nil {
			return Result{}, err
		}

		in, err := os.Open(src)
		if err != nil {
			return Result{}, fmt.Errorf("open src %s: %w", src, err)
		}
		defer in.Close()

		out, err := os.Create(dst)
		if err != nil {
			return Result{}, fmt.Errorf("create dst %s: %w", dst, err)
		}
		defer out.Close()

		if _, err := io.Copy(out, in); err != nil {
			return Result{}, fmt.Errorf("copy %s to %s: %w", src, dst, err)
		}
		return Result{AffectedPaths: []string{dst}}, nil
	},
}
