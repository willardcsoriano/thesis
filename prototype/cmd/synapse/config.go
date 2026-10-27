package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"synapseos/internal/ollama"
)

// envVar is one environment variable synapse reads. envVars is the single
// list: the help text is printed from it and checkEnv validates against it, so
// a variable cannot be documented one way and parsed another. (The test-only
// SYNAPSE_REQUIRE_SANDBOX belongs to internal/testenv and is not listed.)
type envVar struct {
	name, def, desc string
	// check returns an error for a set value the program would otherwise
	// misread or silently replace with its default. Nil accepts any value.
	check func(v string) error
}

// Accepted values of the two analysis switches. analysis.go switches on the
// same strings; TestEnvChoicesMatchTheirParsers keeps the two in step.
var (
	analysisChoices = []string{"strict", "capture", "off", "0", "false", "no"}
	resolveChoices  = []string{"auto", "always", "never"}
)

var envVars = []envVar{
	{"SYNAPSE_MODEL", defaultModel, "Ollama model to use", checkModelName},
	{"SYNAPSE_OLLAMA", ollama.DefaultBaseURL, "Ollama endpoint", checkURL},
	{"SYNAPSE_NUM_CTX", strconv.Itoa(defaultNumCtx), "model context window, in tokens (at least " + strconv.Itoa(minNumCtx) + ")", checkNumCtx},
	{"SYNAPSE_KEEP_ALIVE", ollama.DefaultKeepAlive, "how long Ollama keeps the model loaded (\"10m\", seconds, or -1 for always)", checkKeepAlive},
	{"SYNAPSE_ANALYSIS", "strict", "effect analysis: strict, capture, or off", oneOf(analysisChoices)},
	{"SYNAPSE_RESOLVE", "auto", "resolve run-time targets: auto (only in a sandbox), always, or never", oneOf(resolveChoices)},
	{"SYNAPSE_SESSION_LOG", "", "study telemetry log path; requires SYNAPSE_PARTICIPANT", checkLogPath},
	{"SYNAPSE_PARTICIPANT", "", "participant ID written into the study log", nil},
	{"SYNAPSE_TASK_ID", "", "study task ID for the events that follow", nil},
	{"SYNAPSE_AIDS", "all", "harness aids to use, for experiments: all, none, or a list of peek,tools,refs,lint,literal", checkAids},
}

// telemetryVars are the variables undo reads: it records its own event in
// the study log, so a broken log setting refuses undo too, with the same
// exit code as every other command (finding r2-atk8-05).
var telemetryVars = map[string]bool{"SYNAPSE_SESSION_LOG": true, "SYNAPSE_PARTICIPANT": true, "SYNAPSE_TASK_ID": true}

// checkTelemetryEnv is checkEnv restricted to telemetryVars.
func checkTelemetryEnv(getenv func(string) string) error {
	return checkEnv(func(k string) string {
		if telemetryVars[k] {
			return getenv(k)
		}
		return ""
	})
}

// checkEnv validates every set variable and reports all problems at once, so
// a misconfiguration is fixed in one pass rather than one error per run. It
// runs before anything that reads them; an unset variable is never an error.
func checkEnv(getenv func(string) string) error {
	var errs []error
	for _, e := range envVars {
		v := getenv(e.name)
		if v == "" || e.check == nil {
			continue
		}
		if err := e.check(v); err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: %w", e.name, v, err))
		}
	}
	if getenv("SYNAPSE_SESSION_LOG") != "" && getenv("SYNAPSE_PARTICIPANT") == "" {
		errs = append(errs, errors.New("SYNAPSE_SESSION_LOG is set but SYNAPSE_PARTICIPANT is empty; telemetry must be attributable"))
	}
	return errors.Join(errs...)
}

func oneOf(choices []string) func(string) error {
	return func(v string) error {
		for _, c := range choices {
			if strings.EqualFold(v, c) {
				return nil
			}
		}
		return fmt.Errorf("must be one of %s", strings.Join(choices, ", "))
	}
}

func checkURL(v string) error {
	u, err := url.Parse(v)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("must be an http:// or https:// URL")
	}
	if u.Host == "" {
		return errors.New("has no host")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return errors.New("has a port outside 1 to 65535")
		}
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return errors.New("must not contain ? or # (request paths are added to it)")
	}
	return nil
}

// minNumCtx is the smallest context window that holds the prompts with room
// for a task. Smaller ones were accepted and silently cut the prompt, so the
// model answered a different question (finding atk4-14).
const minNumCtx = 4096

func checkNumCtx(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < minNumCtx {
		return fmt.Errorf("must be a whole number of at least %d", minNumCtx)
	}
	return nil
}

// modelNameRE is the shape of an Ollama model name: name, optional tag,
// optional namespace or registry. A name with spaces or control characters
// reached the model and failed there (finding atk4-02).
var modelNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*(:[A-Za-z0-9._-]+)?$`)

func checkModelName(v string) error {
	if !modelNameRE.MatchString(v) {
		return errors.New(`must be an Ollama model name such as "qwen2.5-coder:3b"`)
	}
	return nil
}

// checkLogPath refuses a session log that cannot be opened for appending, so
// a study session cannot start and then fail (finding atk4-01). It creates
// nothing: it checks the path, and that its folder exists and is writable.
func checkLogPath(v string) error {
	if fi, err := os.Stat(v); err == nil {
		if fi.IsDir() {
			return errors.New("is a folder, not a file")
		}
		// A device such as /dev/full or /dev/null accepts the open and then
		// loses every event (finding r2-atk8-06).
		if !fi.Mode().IsRegular() {
			return errors.New("is not a regular file")
		}
		if syscall.Access(v, 2) != nil { // W_OK
			return errors.New("is not writable")
		}
		return nil
	}
	dir := filepath.Dir(v)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("its folder %s does not exist", dir)
	}
	if syscall.Access(dir, 2) != nil {
		return fmt.Errorf("its folder %s is not writable", dir)
	}
	return nil
}

// checkKeepAlive accepts what Ollama's keep_alive accepts: a Go duration
// ("10m", "1h30m") or a whole number of seconds, where a negative number
// keeps the model loaded indefinitely.
func checkKeepAlive(v string) error {
	if _, err := time.ParseDuration(v); err == nil {
		return nil
	}
	if _, err := strconv.Atoi(v); err == nil {
		return nil
	}
	return errors.New(`must be a duration such as "10m", or a whole number of seconds`)
}

// printEnvHelp writes the environment section of the help text.
func printEnvHelp(w io.Writer) {
	fmt.Fprintln(w, "Environment:")
	for _, e := range envVars {
		def := ""
		if e.def != "" {
			def = " (default " + e.def + ")"
		}
		fmt.Fprintf(w, "  %-21s %s%s\n", e.name, e.desc, def)
	}
}
