package main

import (
	"context"
	"os"
	"strings"
	"time"

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
	d := gate.Decide(ctx, newAnalyzer(wd), cmd, p)
	return &d
}

// firstReason is the leading reason the analysis gave, for the prompt.
func firstReason(d *gate.Decision) string {
	if d == nil || len(d.Verdict.Reasons) == 0 {
		return "the effects of this command could not be determined"
	}
	return d.Verdict.Reasons[0]
}
