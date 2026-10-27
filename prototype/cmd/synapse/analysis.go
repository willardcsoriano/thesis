package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"synapseos/internal/classifier"
	"synapseos/internal/effects"
	"synapseos/internal/gate"
)

// The effect analysis is on by default, in strict mode: the gate asks whenever a
// command is not recoverable without capture, which is every prompt the list gave
// before plus the ones it missed. SYNAPSE_ANALYSIS selects the mode:
//
//	strict   (default) ask unless the command is recoverable with no capture needed
//	capture  capture silently and ask only when a command is unrecoverable
//	off      the list classifier and legacy backups alone, as before the analysis
//
// In every mode the list is still consulted and a command the list calls
// irreversible still asks: the analysis can add a confirmation, never remove one.
func analysisPolicy() (gate.Policy, bool) {
	switch strings.ToLower(os.Getenv("SYNAPSE_ANALYSIS")) {
	case "off", "0", "false", "no":
		return "", false
	case "capture":
		return gate.Capture, true
	}
	return gate.Strict, true
}

// newAnalyzer builds the analyser for one command. Resolving run-time targets
// means running read-only commands before the user has consented, so by default it
// happens only where a read-only sandbox is available (SYNAPSE_RESOLVE=auto).
// "always" resolves without a sandbox, relying on the analysis's read-only proof
// alone; "never" leaves anything that needs resolving unresolved, which asks.
func newAnalyzer(wd string) *effects.Analyzer {
	an := effects.New(wd)
	switch strings.ToLower(os.Getenv("SYNAPSE_RESOLVE")) {
	case "never":
	case "always":
		an.Run = effects.DefaultRunner(10 * time.Second)
	default:
		if effects.SandboxAvailable() {
			an.Run = effects.DefaultRunner(10 * time.Second)
		}
	}
	return an
}

// analysisDecision returns the gate's decision for cmd, or nil when the analysis
// is off or the working directory is unknown.
func analysisDecision(ctx context.Context, cmd, wd string) *gate.Decision {
	p, on := analysisPolicy()
	if !on || wd == "" {
		return nil
	}
	return decideFailClosed(ctx, newAnalyzer(wd), cmd, p)
}

// decide is gate.Decide, a variable only so a test can make it panic.
var decide = gate.Decide

// decideFailClosed runs the analysis and turns a panic inside it into the
// fail-closed answer: unresolved and unrecoverable, which asks. The analysis
// runs on every command the model proposes, so without this one input it
// cannot handle would crash synapse mid-task instead of asking. The frozen
// analysis is not edited for such a defect; it is caught here and recorded.
func decideFailClosed(ctx context.Context, an *effects.Analyzer, cmd string, p gate.Policy) (d *gate.Decision) {
	defer func() {
		if r := recover(); r != nil {
			d = &gate.Decision{
				Verdict: effects.Verdict{Class: effects.Unrecoverable, Reasons: []string{fmt.Sprintf("the analysis failed on this command (%v), so it is treated as unrecoverable", r)}},
				Confirm: true,
			}
		}
	}()
	res := decide(ctx, an, cmd, p)
	return &res
}

// firstReason is the leading reason the analysis gave, for the prompt.
func firstReason(d *gate.Decision) string {
	if d == nil || len(d.Verdict.Reasons) == 0 {
		return "the effects of this command could not be determined"
	}
	return d.Verdict.Reasons[0]
}

// analysisClearsTextScan reports whether the analysis may clear a list
// confirmation whose reason is one of the list's character scans (D49). It
// may when it resolved everything (Confident) and found the command
// recoverable with nothing to capture, the same bar strict mode uses to run a
// command without asking. The list's command rules (rm, dd, git reset --hard,
// and the rest) are never cleared: for those the gate stays a union.
func analysisClearsTextScan(listReason string, d *gate.Decision) bool {
	return classifier.IsTextScanReason(listReason) &&
		d != nil && d.Confident && d.Verdict.Class == effects.Recoverable
}
