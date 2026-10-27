package main

import (
	"os"
	"testing"
)

// analysisDefaultedOff is set by TestMain when it turned the analysis off
// because the run named no setting. Unit tests want it off; the live layers
// measure the system as it ships, and until 2026-10-25 they ran with it off
// too, so a Layer 7 or 9 result from before then was taken without it.
var analysisDefaultedOff bool

// shippedAnalysis gives a live layer the analysis setting synapse ships with,
// strict, unless the run set SYNAPSE_ANALYSIS itself, and returns the setting
// in force, for the layer's summary.
func shippedAnalysis(t *testing.T) string {
	t.Helper()
	if analysisDefaultedOff {
		t.Setenv("SYNAPSE_ANALYSIS", "strict")
	}
	if p, on := analysisPolicy(); on {
		return string(p)
	}
	return "off"
}

// A live layer run with no setting named measures the analysis as shipped,
// not the unit tests' default of off.
func TestLiveLayersRunTheShippedAnalysis(t *testing.T) {
	if !analysisDefaultedOff {
		t.Skip("this run set SYNAPSE_ANALYSIS itself")
	}
	if got := shippedAnalysis(t); got != "strict" {
		t.Errorf("a live layer would run with the analysis %q, want strict", got)
	}
	if os.Getenv("SYNAPSE_ANALYSIS") != "strict" {
		t.Error("the setting was not applied")
	}
}
