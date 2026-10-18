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
	Prompts   map[string]string `json:"prompts"`             // tier -> first message
	FollowUps []string          `json:"follow_ups,omitempty"` // later turns, persona tasks only
	Features  []string          `json:"features,omitempty"`   // novice-tier tags, e.g. "pronoun", "taglish"
	Source    string            `json:"source"`              // provenance of the wording, e.g. "nl2bash:5943", "authored", a URL
}

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
	Kind       string   `json:"kind"`
	Command    string   `json:"command"`
	Exit       *int     `json:"exit,omitempty"` // nil when the step never ran or reported no code
	DurationMS int64    `json:"duration_ms,omitempty"`
	Execve     []string `json:"execve,omitempty"` // argv[0] of every program launched, in order
	Lost       *bool    `json:"lost,omitempty"`   // oracle verdict over this step's effect; nil when not observed

	// Scoring, filled after capture.
	Shape        string   `json:"shape,omitempty"`
	Interpreters []string `json:"interpreters,omitempty"`
	Analysis     string   `json:"analysis,omitempty"`
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
