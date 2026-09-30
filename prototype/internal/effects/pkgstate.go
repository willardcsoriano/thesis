package effects

import (
	"os"
	"regexp"
	"strings"
)

// Package and service state is not a path, so it has no pre-image to copy. What it
// has instead is an inverse: a command that puts the state back. This file models
// the two managers of a Debian system, apt/dpkg and systemd, by asking the tools
// themselves what would happen (apt-get -s, dpkg-query, systemctl is-*) rather than
// re-implementing their logic, and derives the inverse from the answer.
//
// The model covers package and unit state, not what maintainer scripts or a
// service's own start-up do besides. That limit is inherent: those are arbitrary
// programs. Other package managers (pip, npm, snap, ...) stay unrecoverable unless
// they are plainly read-only.

// StateChange is one reversible change to package or service state.
type StateChange struct {
	Subject string // "pkg:hello", "unit:cron.service"
	Change  string // what happens, for the confirmation text
	// Inverse is the commands that put the state back, in order. Empty means the
	// change needs no undo (it was already the case).
	Inverse [][]string
	// Sudo is true when the original ran with elevated privilege, so the undo must too.
	Sudo   bool
	Source string
}

// ---- apt / dpkg ----

var aptSim = regexp.MustCompile(`^(Inst|Remv|Purg) (\S+)(?: \[([^\]]*)\])?`)

// aptFlagsWithValue are options that take a separate value word.
var aptFlagsWithValue = setOf("-t --target-release -o --option -c --config-file -a --host-architecture")

func (st *state) aptTool(c *call, name string) {
	if c.aerr != nil {
		st.unrec(c, pkgWhy)
		return
	}
	verb, opts := aptVerb(c.args)
	switch verb {
	case "":
		st.unrec(c, pkgWhy)
	case "update", "clean", "autoclean", "download", "check", "changelog", "source", "build-dep-none":
		return // refreshes or trims caches; nothing a user owns
	case "install", "reinstall", "remove", "purge", "autoremove", "autopurge", "upgrade",
		"full-upgrade", "dist-upgrade", "build-dep", "satisfy":
		st.aptSimulate(c, name, verb, opts)
	default:
		st.unrec(c, pkgWhy)
	}
}

// aptVerb returns the subcommand and every other argument, dropping the flags that
// do not change what would be installed.
func aptVerb(args []string) (string, []string) {
	verb := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "-"):
			if aptFlagsWithValue[a] && i+1 < len(args) {
				rest = append(rest, a, args[i+1])
				i++
				continue
			}
			switch a {
			case "-y", "--yes", "--assume-yes", "-q", "-qq", "--quiet", "-f", "--fix-broken", "--force-yes":
				continue
			}
			rest = append(rest, a)
		case verb == "":
			verb = a
		default:
			rest = append(rest, a)
		}
	}
	return verb, rest
}

func (st *state) aptSimulate(c *call, name, verb string, opts []string) {
	for _, o := range opts {
		if o == "-o" || o == "--option" || o == "-c" || o == "--config-file" || strings.HasPrefix(o, "-o") && len(o) > 2 || strings.HasPrefix(o, "--option=") {
			st.unresolved(c, "an apt option changes what the simulation would do")
			return
		}
	}
	argv := append([]string{"apt-get", "-s", verb}, opts...)
	// A dry run needs no privilege, so it runs as the current user even under sudo.
	pc := *c
	pc.priv = false
	out, ok := st.dry(&pc, argv)
	if !ok {
		return
	}
	found := false
	for _, l := range lines(out) {
		m := aptSim.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		found = true
		st.aptChange(c, m[1], m[2], m[3], l)
	}
	if !found && strings.Contains(string(out), "E:") {
		st.unresolved(c, "apt could not plan this: "+firstErrLine(out))
	}
}

func firstErrLine(out []byte) string {
	for _, l := range lines(out) {
		if strings.HasPrefix(l, "E:") {
			return strings.TrimSpace(l[2:])
		}
	}
	return "error"
}

// aptChange turns one simulated action into a state change with an inverse.
func (st *state) aptChange(c *call, act, pkg, oldVer, line string) {
	sc := StateChange{Subject: "pkg:" + pkg, Sudo: c.priv, Source: c.src}
	switch act {
	case "Inst":
		if oldVer == "" {
			sc.Change = "install " + pkg
			// Config a previous remove left behind (status rc) must survive the undo.
			verb := "purge"
			if s, _ := st.pkgStatus(c, pkg); s == "rc" {
				verb = "remove"
			}
			sc.Inverse = [][]string{{"apt-get", verb, "-y", pkg}}
		} else {
			sc.Change = "change " + pkg + " from " + oldVer
			if newVer := instNewVersion(line); newVer == oldVer {
				break // a reinstall of the same version
			}
			if !st.versionAvailable(c, pkg, oldVer) {
				st.unrec(c, "cannot restore "+pkg+" "+oldVer+": that version is no longer offered by any repository")
				return
			}
			sc.Inverse = [][]string{{"apt-get", "install", "-y", "-o", "Dpkg::Options::=--force-confold", "--allow-downgrades", pkg + "=" + oldVer}}
		}
	case "Remv", "Purg":
		if act == "Remv" {
			sc.Change = "remove " + pkg
		} else {
			sc.Change = "purge " + pkg
		}
		if oldVer == "" || !st.versionAvailable(c, pkg, oldVer) {
			st.unrec(c, "cannot reinstall "+pkg+": "+orUnknown(oldVer)+" is not offered by any repository")
			return
		}
		sc.Inverse = [][]string{{"apt-get", "install", "-y", "-o", "Dpkg::Options::=--force-confold", "--allow-downgrades", pkg + "=" + oldVer}}
		if act == "Purg" {
			// Purge also deletes the package's configuration files; those have no
			// repository copy, so they are captured as removals.
			if !st.captureConffiles(c, pkg) {
				return
			}
		}
	}
	st.an.States = append(st.an.States, sc)
}

func orUnknown(v string) string {
	if v == "" {
		return "its version"
	}
	return "version " + v
}

var instNew = regexp.MustCompile(`\((\S+) `)

func instNewVersion(line string) string {
	if m := instNew.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// pkgStatus is dpkg's two-letter state ("ii", "rc", ...), or "" if unknown.
func (st *state) pkgStatus(c *call, pkg string) (string, bool) {
	out, err := st.readOnly(c, []string{"dpkg-query", "-W", "-f=${db:Status-Abbrev}\n", pkg})
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// versionAvailable reports whether a repository still offers pkg at ver, using the
// version table of apt-cache policy: a version is available when some source other
// than the local dpkg status file lists it. A version line is "<version> <priority>";
// a source line is "<priority> <location>".
func (st *state) versionAvailable(c *call, pkg, ver string) bool {
	out, err := st.readOnly(c, []string{"apt-cache", "policy", pkg})
	if err != nil {
		return false
	}
	table, cur := false, ""
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "Version table:") {
			table = true
			continue
		}
		f := strings.Fields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "***")))
		if !table || len(f) < 2 {
			continue
		}
		if isDigits(f[1]) {
			cur = f[0]
		} else if cur == ver && isDigits(f[0]) && !strings.HasPrefix(f[1], "/var/lib/dpkg/status") {
			return true
		}
	}
	return false
}

// captureConffiles records the package's config files as removals so the existing
// capture machinery preserves them before a purge.
func (st *state) captureConffiles(c *call, pkg string) bool {
	out, err := st.readOnly(c, []string{"dpkg-query", "-W", "-f=${Conffiles}\n", pkg})
	if err != nil {
		st.unresolved(c, "could not list the configuration files of "+pkg+": "+err.Error())
		return false
	}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) == 0 || !strings.HasPrefix(f[0], "/") {
			continue
		}
		if _, err := os.Lstat(f[0]); err != nil {
			continue // already gone
		}
		st.add(c, Effect{Kind: Remove, Path: f[0]})
	}
	return true
}

// readOnly runs a fixed read-only query. It fails closed: no runner, no answer.
func (st *state) readOnly(c *call, argv []string) ([]byte, error) {
	if st.a.Run == nil {
		return nil, errNoRunner
	}
	wd := c.sc.wd
	if wd == unknownWD || wd == "" {
		wd = "/"
	}
	return st.a.Run(st.ctx, wd, argv)
}

// dpkg handles the direct package tool. Installing a .deb reads its name and
// version from the file; removing or purging names the packages outright.
func (st *state) dpkgTool(c *call) {
	if c.aerr != nil {
		st.unrec(c, pkgWhy)
		return
	}
	var mode string
	var operands []string
	for _, a := range c.args {
		switch a {
		case "-i", "--install":
			mode = "install"
		case "-r", "--remove":
			mode = "remove"
		case "-P", "--purge":
			mode = "purge"
		default:
			if !strings.HasPrefix(a, "-") {
				operands = append(operands, a)
			} else if strings.HasPrefix(a, "--force") || strings.HasPrefix(a, "--root") || a == "--auto-deconfigure" {
				st.unrec(c, "dpkg option "+a+" bypasses its safety checks")
				return
			}
		}
	}
	if mode == "" || len(operands) == 0 {
		st.unrec(c, pkgWhy)
		return
	}
	for _, op := range operands {
		switch mode {
		case "install":
			st.dpkgInstall(c, c.abs(op))
		default:
			out, err := st.readOnly(c, []string{"dpkg-query", "-W", "-f=${Version}\n", op})
			ver := strings.TrimSpace(string(out))
			if err != nil || ver == "" {
				st.unresolved(c, "package "+op+" is not installed, or its version could not be read")
				continue
			}
			act := "Remv"
			if mode == "purge" {
				act = "Purg"
			}
			st.aptChange(c, act, op, ver, "")
		}
	}
}

func (st *state) dpkgInstall(c *call, deb string) {
	out, err := st.readOnly(c, []string{"dpkg-deb", "-f", deb, "Package", "Version"})
	if err != nil {
		st.unresolved(c, "could not read "+deb+": "+err.Error())
		return
	}
	var pkg, ver string
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "Package:"):
			pkg = strings.TrimSpace(l[len("Package:"):])
		case strings.HasPrefix(l, "Version:"):
			ver = strings.TrimSpace(l[len("Version:"):])
		}
	}
	if pkg == "" || ver == "" {
		st.unresolved(c, "could not read the package name and version from "+deb)
		return
	}
	old, _ := st.readOnly(c, []string{"dpkg-query", "-W", "-f=${Version}\n", pkg})
	oldVer := strings.TrimSpace(string(old))
	st.aptChange(c, "Inst", pkg, oldVer, "Inst "+pkg+" ["+oldVer+"] ("+ver+" local)")
}

// ---- systemd ----

// unitVerbs that only read, or that change nothing lasting.
var unitNoop = setOf("daemon-reload daemon-reexec reset-failed")

func (st *state) systemctlTool(c *call) {
	if c.aerr != nil {
		st.unrec(c, pkgWhy)
		return
	}
	var verb string
	var units, flags []string
	now := false
	for _, a := range c.args {
		switch {
		case a == "--user":
			flags = append(flags, a)
		case a == "--now":
			now = true
		case a == "--system" || a == "--no-block" || a == "--quiet" || a == "-q" || a == "--no-pager" || a == "--no-ask-password" || a == "--no-reload":
		case strings.HasPrefix(a, "-"):
			st.unresolved(c, "systemctl option "+a+" is not modelled")
			return
		case verb == "":
			verb = a
		default:
			units = append(units, a)
		}
	}
	if unitNoop[verb] {
		return
	}
	switch verb {
	case "start", "stop", "restart", "try-restart", "reload", "reload-or-restart", "try-reload-or-restart",
		"enable", "disable", "mask", "unmask":
	case "":
		st.unrec(c, pkgWhy)
		return
	default:
		st.unrec(c, "systemctl "+verb+" changes state that is not modelled or cannot be undone")
		return
	}
	if len(units) == 0 {
		st.unresolved(c, "no unit named")
		return
	}
	for _, u := range units {
		st.unitChange(c, verb, u, flags, now)
	}
}

func (st *state) unitState(c *call, flags []string, sub, unit string) (string, bool) {
	argv := append([]string{"systemctl"}, flags...)
	argv = append(argv, sub, unit)
	out, err := st.readOnly(c, argv)
	s := strings.TrimSpace(string(out))
	if err != nil || s == "" {
		return "", false
	}
	return strings.Fields(s)[0], true
}

func (st *state) unitChange(c *call, verb, unit string, flags []string, now bool) {
	active, ok1 := st.unitState(c, flags, "is-active", unit)
	enabled, ok2 := st.unitState(c, flags, "is-enabled", unit)
	if !ok1 && !ok2 {
		st.unresolved(c, "could not read the state of "+unit)
		return
	}
	if enabled == "not-found" || enabled == "" && active == "inactive" && !ok2 {
		st.unresolved(c, "unit "+unit+" was not found")
		return
	}
	wasActive := active == "active" || active == "activating" || active == "reloading"
	cmd := func(v string) []string { return append(append([]string{"systemctl"}, flags...), v, unit) }
	sc := StateChange{Subject: "unit:" + unit, Sudo: c.priv, Source: c.src, Change: verb + " " + unit}
	add := func(v string) { sc.Inverse = append(sc.Inverse, cmd(v)) }

	switch verb {
	case "start":
		if !wasActive {
			add("stop")
		}
	case "stop":
		if wasActive {
			add("start")
		}
	case "restart", "try-restart", "reload", "reload-or-restart", "try-reload-or-restart":
		if !wasActive && (verb == "restart" || verb == "reload-or-restart") {
			add("stop")
		}
	case "enable", "disable", "mask", "unmask":
		if !st.unitFileChange(c, &sc, verb, unit, enabled, wasActive, now, cmd) {
			return
		}
	}
	st.an.States = append(st.an.States, sc)
}

// unitFileChange sets the inverse of an enable/disable/mask/unmask from the unit's
// current enablement, and folds in the start or stop --now performs.
func (st *state) unitFileChange(c *call, sc *StateChange, verb, unit, enabled string, wasActive, now bool, cmd func(string) []string) bool {
	add := func(v string) { sc.Inverse = append(sc.Inverse, cmd(v)) }
	switch verb {
	case "enable":
		switch enabled {
		case "enabled", "enabled-runtime", "static", "alias", "indirect", "linked":
		case "disabled":
			add("disable")
		default:
			st.unrec(c, "cannot enable "+unit+" from state "+enabled)
			return false
		}
		if now && !wasActive {
			add("stop")
		}
	case "disable":
		switch enabled {
		case "enabled", "enabled-runtime":
			add("enable")
		case "disabled", "static", "alias", "indirect", "linked":
		default:
			st.unrec(c, "cannot disable "+unit+" from state "+enabled)
			return false
		}
		if now && wasActive {
			add("start")
		}
	case "mask":
		switch enabled {
		case "masked", "masked-runtime":
		case "enabled", "enabled-runtime":
			add("unmask")
			add("enable")
		default:
			add("unmask")
		}
	case "unmask":
		if enabled == "masked" || enabled == "masked-runtime" {
			add("mask")
		}
	}
	return true
}

// serviceTool maps the SysV `service NAME ACTION` form onto systemctl.
func (st *state) serviceTool(c *call) {
	ops := parseOpts(c.args, "").operands
	if c.aerr != nil || len(ops) != 2 {
		st.unrec(c, pkgWhy)
		return
	}
	sc := *c
	sc.args = []string{ops[1], ops[0]}
	st.systemctlTool(&sc)
}
