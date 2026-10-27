// Package probe holds the shared types of the agent command study
// (docs/agent-command-study.md): the tasks every driver runs and the record
// each captured command becomes. Drivers produce Records with the capture
// fields filled; scoring fills the rest afterwards from the stored command, so
// a later analyzer can re-score old runs without calling any model again.
package probe

import (
	"encoding/json"
	"io"

	"synapseos/internal/oracle"
)

// Tiers, from most to least technical. Every task carries a prompt for each.
const (
	TierTechnical = "technical"
	TierInterface = "interface"
	TierPlain     = "plain"
	TierNovice    = "novice"
)

// Partitions are reported apart and never pooled.
const (
	PartitionNL2Bash  = "S" // NL2Bash descriptions behind round 6m
	PartitionEveryday = "E" // everyday suite written for this study
	PartitionPersona  = "P" // multi-turn persona conversations
)

// Task is one line of the task suite.
type Task struct {
	ID        string            `json:"id"`
	Partition string            `json:"partition"`
	Category  string            `json:"category"`
	Fixture   []oracle.Entry    `json:"fixture"`
	Prompts   map[string]string `json:"prompts"`              // tier -> first message
	FollowUps []string          `json:"follow_ups,omitempty"` // later turns, persona tasks only
	Features  []string          `json:"features,omitempty"`   // novice-tier tags, e.g. "pronoun", "taglish"
	Sources   map[string]string `json:"sources"`              // tier -> provenance of that tier's wording (CheckSource)
}

// Layouts of the agent's sandbox (Record.Layout).
const (
	// LayoutHome: the fixture is the agent's home folder, which is also
	// where it starts, so "my downloads" is ~/Downloads as on a real
	// desktop. The CLI's own state lives outside it.
	LayoutHome = "home"
	// LayoutTaskFolder: the fixture is a folder named task in the home
	// folder. Every run before LayoutHome (batch1) used it.
	LayoutTaskFolder = ""
)

// Kinds of captured step.
const (
	KindBash = "bash" // a shell command
	// Any other Kind is the name of a file tool (read_file, Write, Edit, ...);
	// its arguments go in Command as JSON.
)

// Record is one captured command or file-tool call.
type Record struct {
	// Which run.
	Run          string `json:"run"`
	Driver       string `json:"driver"` // "api" or "cli"
	Model        string `json:"model"`
	ModelVersion string `json:"model_version,omitempty"`
	ToolVersion  string `json:"tool_version,omitempty"` // CLI version for the cli driver
	Profile      string `json:"profile"`                // "bash", "bash+files", or the CLI's own tool set ("native")
	// Agent names the CLI behind a cli-driver record (AgentClaudeCode,
	// AgentGeminiCLI). Replay needs it to track the shell's working directory
	// the way that CLI does; records without it are recognised by model name.
	Agent string `json:"agent,omitempty"`
	// Layout is where the agent's sandbox put the task's fixture, which
	// replay must reproduce: LayoutHome, or "" for runs before it
	// (LayoutTaskFolder), whose fixture sat in a folder below HOME.
	Layout string `json:"layout,omitempty"`

	// RunStatus is how the task run this record belongs to ended, from the
	// run's status.jsonl ("complete", "incomplete", "interrupted", "error",
	// "fatal"), or RunMissing when status.jsonl exists but has no line for the
	// run (the driver was killed during it). Empty when the run predates
	// status.jsonl. Filled by scoring.
	RunStatus string `json:"run_status,omitempty"`

	// Which task, and how it was asked.
	Task      string   `json:"task"`
	Partition string   `json:"partition"`
	Category  string   `json:"category"`
	Tier      string   `json:"tier"`
	Features  []string `json:"features,omitempty"`

	// Position.
	Turn int `json:"turn"` // 0 for the first message, 1.. for follow-ups
	Step int `json:"step"` // order within the agent's own loop for that turn

	// Capture.
	Kind    string `json:"kind"`
	Command string `json:"command"`
	// Dir is the shell call's own working-directory argument as the agent gave
	// it (the Gemini CLI's dir_path, "" when the call had none). Nil means the
	// driver did not record it, so the directory of a Gemini shell call is
	// unknown and its replay stops there. Claude Code's Bash has no such
	// argument and leaves it nil.
	Dir        *string  `json:"dir,omitempty"`
	Exit       *int     `json:"exit,omitempty"` // nil when the step never ran or reported no code
	DurationMS int64    `json:"duration_ms,omitempty"`
	Execve     []string `json:"execve,omitempty"` // argv[0] of every program launched, in order
	Lost       *bool    `json:"lost,omitempty"`   // oracle verdict over this step's effect; nil when not observed

	// Scoring, filled after capture.
	Shape        string   `json:"shape,omitempty"`
	Interpreters []string `json:"interpreters,omitempty"`
	Analysis     string   `json:"analysis,omitempty"`
	// Which filesystem state Analysis was taken on (StateReplayed,
	// StateUnverified, StateDiverged, StateUnreconstructable, StateInitial),
	// the directory it ran from as the agent saw it, and why the state is
	// not StateReplayed. Only StateReplayed verdicts are headline numbers.
	AnalysisState string `json:"analysis_state,omitempty"`
	AnalysisCwd   string `json:"analysis_cwd,omitempty"`
	AnalysisNote  string `json:"analysis_note,omitempty"`
}

// Append writes r as one JSON line.
func Append(w io.Writer, r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// Task-run statuses as scoring records them (RunStatus).
const (
	RunComplete = "complete"
	RunMissing  = "missing"
)
