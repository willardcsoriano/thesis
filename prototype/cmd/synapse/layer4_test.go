//go:build live

// Layer 4 of testing-plan.md (build-order.md F4): typed-operation
// reliability experiment. Answers the specific empirical question behind
// "should SynapseOS reimplement filesystem-MCP-style typed operations":
// fix a task set (the file-manipulation category Layer 3 already covers),
// run each task through two paths, and score both.
//
//   - Path (a) "raw_bash": today's shipped path — propose() gets a bash
//     string from the model, classifier.Classify judges it, and (for this
//     experiment only — runLoop's confirmation gate is a separate, already-
//     tested concern) it is executed unconditionally against a disposable
//     fixture directory so task success reflects what a real "yes" would
//     have done.
//   - Path (b) "typed_op": the model is offered internal/typedops' small
//     function schema via Ollama's native tool-calling API. Session 23
//     found empirically that qwen2.5-coder does not reliably populate the
//     tool_calls field (0/5 trials) even though its own Ollama template
//     instructs it to — so this harness measures that directly (NativeToolCall
//     per record) rather than assuming, and falls back to parsing the
//     message content as freeform JSON the same defensive way cleanCommand
//     already handles bash. typedops.Op.Dispatch then performs the call
//     through Go's os/io stdlib — no MCP server, no Node.js dependency.
//
// Both paths are scored on call-validity (did the model produce something
// executable at all), task-success (did it accomplish the request, checked
// against the fixture's real end state), and safety-classification
// agreement (would both paths have reached the same Reversible/Irreversible
// verdict for the same request). The result decides whether F4 becomes
// real runtime scope or stays documented as "tried, not worth it yet" — not
// decided in advance.
//
// Fixture paths are templated into each task's prompt (a real disposable
// t.TempDir(), not "~/Desktop" or "/tmp") deliberately: executing whatever
// the model proposes against the user's actual home directory or /tmp would
// not be a safe thing for an automated test to do, live model included.
//
// Run with:
//
//	go test -tags live ./cmd/synapse/... -run TestLayer4TypedOperationReliability -v
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synapseos/internal/classifier"
	"synapseos/internal/executor"
	"synapseos/internal/ollama"
	"synapseos/internal/typedops"
)

// typedOpSystemPrompt asks the model to prefer a native tool call, with an
// explicit freeform-JSON fallback shape for when it doesn't (or can't) —
// mirroring the defensive-parsing posture systemPrompt/cleanCommand already
// take toward raw bash.
const typedOpSystemPrompt = `You are the command translator for a Linux system running Debian 13 (Trixie).
You have access to a small set of typed file operations, offered to you as tools. Call exactly one tool that accomplishes the user's request.
If you cannot make a tool call for any reason, respond with nothing but a single JSON object of this exact shape, no prose and no markdown fences:
{"op": "<tool name>", "args": {<the tool's arguments>}}
If no available operation can accomplish the request, respond with exactly:
{"op": "UNSUPPORTED"}`

// freeformTypedCall is the shape proposeTypedOp parses out of a chat
// response's Content when the model didn't populate ToolCalls. Op/Args is
// the shape typedOpSystemPrompt actually instructs; Name/Arguments is
// accepted too — live testing found qwen2.5-coder sometimes falls back to
// OpenAI's function-calling key names even when told otherwise, presumably
// because that shape is far more common in its training data than any
// custom schema this prompt could specify.
type freeformTypedCall struct {
	Op        string         `json:"op"`
	Args      map[string]any `json:"args"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (f freeformTypedCall) resolve() (op string, args map[string]any) {
	if f.Op != "" {
		return f.Op, f.Args
	}
	return f.Name, f.Arguments
}

// proposeTypedOp asks model to perform task via the typed-operation tool
// schema and returns whichever call it produced. native reports whether
// Ollama's own tool_calls field was populated (the thing Session 23 found
// unreliable) as opposed to the freeform-JSON fallback. raw is the
// call/JSON text, kept for the report regardless of whether it parsed.
func proposeTypedOp(ctx context.Context, client *ollama.Client, model, task string) (opName string, args map[string]any, native bool, raw string, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	messages := []ollama.ChatMessage{
		{Role: "system", Content: typedOpSystemPrompt},
		{Role: "user", Content: task},
	}
	resp, err := client.Chat(reqCtx, model, messages, typedops.Tools(), map[string]any{"temperature": 0})
	if err != nil {
		return "", nil, false, "", err
	}

	if len(resp.Message.ToolCalls) > 0 {
		call := resp.Message.ToolCalls[0]
		return call.Function.Name, call.Function.Arguments, true, fmt.Sprintf("%s(%v)", call.Function.Name, call.Function.Arguments), nil
	}

	content := cleanCommand(resp.Message.Content)
	var parsed freeformTypedCall
	if jsonErr := json.Unmarshal([]byte(content), &parsed); jsonErr != nil {
		// Not a harness error: an unparseable response is itself the
		// call-validity finding this experiment measures.
		return "", nil, false, content, nil
	}
	op, callArgs := parsed.resolve()
	return op, callArgs, false, content, nil
}

// layer4Task is one fixed scenario run through both paths. Task templates
// the fixture root into the prompt text; SetupFixture and the two
// path-specific Check funcs let a read-only listing task (evidence is
// output text/returned paths) and a mutating task (evidence is on-disk end
// state) share the same harness loop.
type layer4Task struct {
	Category     string
	Task         func(dir string) string
	SetupFixture func(t *testing.T, dir string)
	CheckRawBash func(t *testing.T, dir, stdout string) bool
	CheckTypedOp func(t *testing.T, dir string, affected []string) bool
}

func writeFixtureFile(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture file %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes fixture file %s: %v", path, err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func basenames(paths []string) []string {
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	return names
}

// checkMovedScreenshots and its siblings below are the shared mutating-task
// checks: both paths mutate the same fixture shape, so the on-disk end
// state is checked identically regardless of which path produced it.
func checkMovedScreenshots(dir string) bool {
	return fileExists(filepath.Join(dir, "Screenshots", "shot1.png")) &&
		fileExists(filepath.Join(dir, "Screenshots", "shot2.png")) &&
		fileExists(filepath.Join(dir, "notes.txt"))
}

func checkDeletedOldTmp(dir string) bool {
	return !fileExists(filepath.Join(dir, "old.tmp")) &&
		fileExists(filepath.Join(dir, "new.tmp")) &&
		fileExists(filepath.Join(dir, "old.log"))
}

func checkRenamedJpeg(dir string) bool {
	return fileExists(filepath.Join(dir, "a.jpg")) && fileExists(filepath.Join(dir, "b.jpg")) &&
		!fileExists(filepath.Join(dir, "a.jpeg")) && !fileExists(filepath.Join(dir, "b.jpeg")) &&
		fileExists(filepath.Join(dir, "c.png"))
}

func checkBackedUpConfig(dir string) bool {
	original, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil || string(original) != "port: 8080" {
		return false // original must survive unmutated
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	// Loose on purpose: the task never names the backup file, so any
	// additional file next to the untouched original counts as success.
	return len(entries) >= 2
}

var layer4Tasks = []layer4Task{
	{
		Category: "file search & organization",
		Task: func(dir string) string {
			return fmt.Sprintf("find all PDF files in %s modified in the last 7 days", dir)
		},
		SetupFixture: func(t *testing.T, dir string) {
			now := time.Now()
			writeFixtureFile(t, filepath.Join(dir, "a.pdf"), now)
			writeFixtureFile(t, filepath.Join(dir, "b.pdf"), now.AddDate(0, 0, -10))
			writeFixtureFile(t, filepath.Join(dir, "c.txt"), now)
		},
		CheckRawBash: func(t *testing.T, dir, stdout string) bool {
			return strings.Contains(stdout, "a.pdf") && !strings.Contains(stdout, "b.pdf") && !strings.Contains(stdout, "c.txt")
		},
		CheckTypedOp: func(t *testing.T, dir string, affected []string) bool {
			names := basenames(affected)
			return len(names) == 1 && names[0] == "a.pdf"
		},
	},
	{
		Category: "file search & organization",
		Task: func(dir string) string {
			return fmt.Sprintf("move every screenshot in %s into a folder inside it called Screenshots", dir)
		},
		SetupFixture: func(t *testing.T, dir string) {
			now := time.Now()
			writeFixtureFile(t, filepath.Join(dir, "shot1.png"), now)
			writeFixtureFile(t, filepath.Join(dir, "shot2.png"), now)
			writeFixtureFile(t, filepath.Join(dir, "notes.txt"), now)
		},
		CheckRawBash: func(t *testing.T, dir, stdout string) bool { return checkMovedScreenshots(dir) },
		CheckTypedOp: func(t *testing.T, dir string, affected []string) bool { return checkMovedScreenshots(dir) },
	},
	{
		Category: "file search & organization",
		Task: func(dir string) string {
			return fmt.Sprintf("delete every .tmp file in %s older than a day", dir)
		},
		SetupFixture: func(t *testing.T, dir string) {
			now := time.Now()
			writeFixtureFile(t, filepath.Join(dir, "old.tmp"), now.AddDate(0, 0, -2))
			writeFixtureFile(t, filepath.Join(dir, "new.tmp"), now)
			writeFixtureFile(t, filepath.Join(dir, "old.log"), now.AddDate(0, 0, -2))
		},
		CheckRawBash: func(t *testing.T, dir, stdout string) bool { return checkDeletedOldTmp(dir) },
		CheckTypedOp: func(t *testing.T, dir string, affected []string) bool { return checkDeletedOldTmp(dir) },
	},
	{
		Category: "file search & organization",
		Task: func(dir string) string {
			return fmt.Sprintf("rename all .jpeg files in %s to .jpg", dir)
		},
		SetupFixture: func(t *testing.T, dir string) {
			now := time.Now()
			writeFixtureFile(t, filepath.Join(dir, "a.jpeg"), now)
			writeFixtureFile(t, filepath.Join(dir, "b.jpeg"), now)
			writeFixtureFile(t, filepath.Join(dir, "c.png"), now)
		},
		CheckRawBash: func(t *testing.T, dir, stdout string) bool { return checkRenamedJpeg(dir) },
		CheckTypedOp: func(t *testing.T, dir string, affected []string) bool { return checkRenamedJpeg(dir) },
	},
	{
		Category: "file search & organization",
		Task: func(dir string) string {
			return fmt.Sprintf("copy %s to a backup before I edit it", filepath.Join(dir, "config.yaml"))
		},
		SetupFixture: func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("port: 8080"), 0o644); err != nil {
				t.Fatalf("write config.yaml: %v", err)
			}
		},
		CheckRawBash: func(t *testing.T, dir, stdout string) bool { return checkBackedUpConfig(dir) },
		CheckTypedOp: func(t *testing.T, dir string, affected []string) bool { return checkBackedUpConfig(dir) },
	},
}

// layer4Record is one path's attempt at one task, for human review.
type layer4Record struct {
	Model          string  `json:"model"`
	Category       string  `json:"category"`
	Task           string  `json:"task"`
	Path           string  `json:"path"` // "raw_bash" or "typed_op"
	RawOutput      string  `json:"raw_output,omitempty"`
	CallValid      bool    `json:"call_valid"`
	NativeToolCall bool    `json:"native_tool_call,omitempty"` // typed_op only
	Verdict        string  `json:"verdict,omitempty"`
	Reason         string  `json:"reason,omitempty"`
	TaskSucceeded  bool    `json:"task_succeeded"`
	Err            string  `json:"err,omitempty"`
	LatencySec     float64 `json:"latency_sec,omitempty"`
}

// layer4Summary aggregates layer4Record across one model's full task set —
// this is the row that actually answers Layer 4's question.
type layer4Summary struct {
	Model                     string  `json:"model"`
	RawBashCallValidity       float64 `json:"raw_bash_call_validity"`
	RawBashTaskSuccess        float64 `json:"raw_bash_task_success"`
	TypedOpCallValidity       float64 `json:"typed_op_call_validity"`
	TypedOpTaskSuccess        float64 `json:"typed_op_task_success"`
	TypedOpNativeToolCallRate float64 `json:"typed_op_native_tool_call_rate"`
	SafetyAgreementRate       float64 `json:"safety_agreement_rate"`
}

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func TestLayer4TypedOperationReliability(t *testing.T) {
	client := ollama.New(os.Getenv("SYNAPSE_OLLAMA"))
	ctx := context.Background()
	if err := client.Ping(ctx); err != nil {
		t.Skipf("Ollama not reachable, skipping Layer 4: %v", err)
	}

	var records []layer4Record
	var summaries []layer4Summary

	for _, model := range liveModels() {
		total := len(layer4Tasks)
		var rawValid, rawSuccess, typedValid, typedSuccess, typedNative, comparable, agree int

		for _, task := range layer4Tasks {
			// --- path (a): raw bash + classifier ---
			rawDir := t.TempDir()
			task.SetupFixture(t, rawDir)
			rawTaskText := task.Task(rawDir)

			start := time.Now()
			_, cmd, err := propose(ctx, client, model, rawTaskText)
			rawRec := layer4Record{Model: model, Category: task.Category, Task: rawTaskText, Path: "raw_bash", LatencySec: time.Since(start).Seconds()}
			if err != nil {
				rawRec.Err = err.Error()
			} else {
				rawRec.RawOutput = cmd
				rawRec.CallValid = cmd != "" && cmd != "UNSUPPORTED"
				if rawRec.CallValid {
					rawValid++
					verdict, reason := classifier.Classify(cmd)
					rawRec.Verdict, rawRec.Reason = verdict.String(), reason

					execCtx, execCancel := context.WithTimeout(ctx, 30*time.Second)
					result := executor.Run(execCtx, cmd)
					execCancel()
					if result.TimedOut {
						rawRec.Err = "raw-bash execution exceeded the 30s fixture-run timeout"
					} else {
						rawRec.TaskSucceeded = task.CheckRawBash(t, rawDir, result.Stdout)
						if rawRec.TaskSucceeded {
							rawSuccess++
						}
					}
				}
			}
			records = append(records, rawRec)

			// --- path (b): typed operation via tool-calling ---
			typedDir := t.TempDir()
			task.SetupFixture(t, typedDir)
			typedTaskText := task.Task(typedDir)

			start = time.Now()
			opName, args, native, raw, err := proposeTypedOp(ctx, client, model, typedTaskText)
			typedRec := layer4Record{Model: model, Category: task.Category, Task: typedTaskText, Path: "typed_op", LatencySec: time.Since(start).Seconds(), NativeToolCall: native}
			if err != nil {
				typedRec.Err = err.Error()
			} else {
				typedRec.RawOutput = raw
				op := typedops.Lookup(opName)
				typedRec.CallValid = op != nil
				if typedRec.CallValid {
					typedValid++
					if native {
						typedNative++
					}
					verdict, reason := op.Classify(args)
					typedRec.Verdict, typedRec.Reason = verdict.String(), reason

					if res, dispatchErr := op.Dispatch(args); dispatchErr != nil {
						typedRec.Err = dispatchErr.Error()
					} else {
						typedRec.TaskSucceeded = task.CheckTypedOp(t, typedDir, res.AffectedPaths)
						if typedRec.TaskSucceeded {
							typedSuccess++
						}
					}
				}
			}
			records = append(records, typedRec)

			if rawRec.CallValid && typedRec.CallValid {
				comparable++
				if rawRec.Verdict == typedRec.Verdict {
					agree++
				}
			}
		}

		summary := layer4Summary{
			Model:                     model,
			RawBashCallValidity:       rate(rawValid, total),
			RawBashTaskSuccess:        rate(rawSuccess, total),
			TypedOpCallValidity:       rate(typedValid, total),
			TypedOpTaskSuccess:        rate(typedSuccess, total),
			TypedOpNativeToolCallRate: rate(typedNative, total),
			SafetyAgreementRate:       rate(agree, comparable),
		}
		summaries = append(summaries, summary)
		t.Logf("Layer 4 summary for %s: raw_bash call-validity %.0f%% / task-success %.0f%% — typed_op call-validity %.0f%% / task-success %.0f%% (native tool_calls %.0f%%) — safety agreement %.0f%% (n=%d comparable)",
			model, summary.RawBashCallValidity*100, summary.RawBashTaskSuccess*100,
			summary.TypedOpCallValidity*100, summary.TypedOpTaskSuccess*100, summary.TypedOpNativeToolCallRate*100,
			summary.SafetyAgreementRate*100, comparable)
	}

	out, err := json.MarshalIndent(struct {
		Records   []layer4Record  `json:"records"`
		Summaries []layer4Summary `json:"summaries"`
	}{records, summaries}, "", "  ")
	if err != nil {
		t.Fatalf("could not marshal report: %v", err)
	}
	const reportPath = "layer4_report.json"
	if err := os.WriteFile(reportPath, out, 0o644); err != nil {
		t.Fatalf("could not write report: %v", err)
	}
	t.Logf("wrote %d records across %d model(s) to %s", len(records), len(summaries), reportPath)
}
