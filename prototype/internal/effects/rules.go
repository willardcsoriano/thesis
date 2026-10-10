package effects

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ---- option parsing ----

type optSet struct {
	flags    map[string]string
	operands []string
}

// parseOpts splits GNU-style arguments. short lists the single-letter options that
// take a value; long lists the long options that do.
func parseOpts(args []string, short string, long ...string) optSet {
	o := optSet{flags: map[string]string{}}
	takes := map[string]bool{}
	for _, l := range long {
		takes[l] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			o.operands = append(o.operands, args[i+1:]...)
			return o
		case strings.HasPrefix(a, "--"):
			name, val, has := strings.Cut(a[2:], "=")
			if !has && takes[name] && i+1 < len(args) {
				i++
				val = args[i]
			}
			o.flags[name] = val
		case len(a) > 1 && a[0] == '-':
			cl := a[1:]
			for j := 0; j < len(cl); j++ {
				ch := string(cl[j])
				if strings.Contains(short, ch) {
					val := cl[j+1:]
					if val == "" && i+1 < len(args) {
						i++
						val = args[i]
					}
					o.flags[ch] = val
					break
				}
				o.flags[ch] = ""
			}
		default:
			o.operands = append(o.operands, a)
		}
	}
	return o
}

func (o optSet) has(keys ...string) bool {
	for _, k := range keys {
		if _, ok := o.flags[k]; ok {
			return true
		}
	}
	return false
}

func (o optSet) val(keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := o.flags[k]; ok {
			return v, true
		}
	}
	return "", false
}

func setOf(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

// ---- the read-only allowlist ----

// roAlways are commands whose every form is read-only for the filesystem. Editors
// and pagers are here because opening a file in one is the user's own act, not the
// system's. The list is deliberately an allowlist: a command not named here, and
// not modelled by a rule, is unknown, and unknown asks.
var roAlways = setOf(`ls cat head tail wc cut tr paste join comm nl rev tac fold fmt expand unexpand
basename dirname realpath readlink stat file which whereis type whoami id groups uname uptime cal free
ps pgrep top htop lsof ss netstat ping traceroute tracepath dig nslookup host printenv echo printf pwd
true false test [ expr bc seq yes sleep wait jobs md5sum sha1sum sha224sum sha256sum sha384sum sha512sum
b2sum cksum sum od hexdump strings nm ldd lsblk blkid lscpu lsusb lspci dmesg history alias unalias help
info man apropos whatis locate tree column jq less more most vi vim nano emacs code subl mate gedit kate
pstree rgrep set shopt unset shift return exit break continue trap umask ulimit hash read mapfile readarray getopts let : diff cmp sdiff
du df egrep fgrep grep rg ag ack zgrep zegrep zfgrep zcat bzcat xzcat lzcat zstdcat pgrep pidof nproc
tty stty clear reset tput sync date hostname env printenv arch lsmod groups logname users w who last
lastlog finger getent nslookup whois iostat vmstat mpstat sar uptime free lsattr getfacl`)

var (
	sedWriteCmd  = regexp.MustCompile(`(?m)(^|[;{}\n])\s*([0-9$]+(,[0-9$]+)?|/[^/]*/)?!?\s*[wWe](\s|$)`)
	sedWriteFlag = sedFlagPatterns()
	awkUnsafe    = regexp.MustCompile(`system\s*\(|\bprintf?\b[^;}\n]*[>|]|\|\s*&?\s*getline`)
)

// sedFlagPatterns matches an s command whose flags include w (write file) or e
// (execute). Go's regexp has no backreferences, so there is one pattern per
// common delimiter.
func sedFlagPatterns() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, d := range []string{"/", "|", "#", ",", ":", "@"} {
		q := regexp.QuoteMeta(d)
		part := `(?:[^` + q + `\\]|\\.)*`
		out = append(out, regexp.MustCompile(`s`+q+part+q+part+q+`[gpiImM0-9]*[we](?:\s|;|\}|$)`))
	}
	return out
}

func matchesAny(res []*regexp.Regexp, s string) bool {
	for _, r := range res {
		if r.MatchString(s) {
			return true
		}
	}
	return false
}

// readOnlyForm reports whether name invoked with args cannot change the
// filesystem. argsKnown is false when the arguments could not be expanded, in
// which case only commands read-only in every form qualify.
func readOnlyForm(name string, args []string, argsKnown bool) bool {
	if roAlways[name] {
		switch name {
		case "date":
			return !argsKnown || !hasAny(args, "-s", "--set") && !hasPrefix(args, "-s") && !hasPrefix(args, "--set=")
		case "hostname":
			return !argsKnown || len(parseOpts(args, "").operands) == 0
		}
		return true
	}
	if !argsKnown {
		return false
	}
	o := parseOpts(args, "")
	switch name {
	case "sed":
		in, writes, _ := sedInfo(args)
		return !in && !writes
	case "awk", "gawk", "mawk", "nawk":
		in, prog, _ := awkInfo(args)
		return !in && prog != "" && !awkUnsafe.MatchString(prog)
	case "sort":
		return !o.has("o", "output")
	case "uniq":
		return len(o.operands) <= 1
	case "xxd":
		return len(o.operands) <= 1 && !o.has("r", "revert")
	case "tar":
		m, _, _, _ := tarArgs(args)
		return m == 't' || m == 'd'
	case "unzip":
		return o.has("l", "Z", "t", "v", "p", "z", "c")
	case "curl":
		return !curlWrites(args) && !curlSends(args)
	case "wget":
		v, _ := o.val("O", "output-document")
		return o.has("spider") || v == "-"
	case "git":
		return gitReadOnly(args)
	case "base64", "base32":
		return !o.has("o")
	}
	return false
}

func hasAny(args []string, keys ...string) bool {
	for _, a := range args {
		for _, k := range keys {
			if a == k {
				return true
			}
		}
	}
	return false
}

func hasPrefix(args []string, p string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, p) && !strings.HasPrefix(a, "--") {
			return true
		}
	}
	return false
}

// sedInfo reports whether the sed invocation edits in place, whether its script
// contains a write or execute command, and which operands are files.
func sedInfo(args []string) (inPlace, writes bool, files []string) {
	var scripts []string
	var operands []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--in-place" || strings.HasPrefix(a, "--in-place="):
			inPlace = true
		case a == "--expression":
			if i+1 < len(args) {
				i++
				scripts = append(scripts, args[i])
			}
		case strings.HasPrefix(a, "--expression="):
			scripts = append(scripts, strings.TrimPrefix(a, "--expression="))
		case a == "--file" || strings.HasPrefix(a, "--file="):
			writes = true // script text unknown
			if a == "--file" {
				i++
			}
		case strings.HasPrefix(a, "--"):
		case len(a) > 1 && a[0] == '-':
			cl := a[1:]
		cluster:
			for j := 0; j < len(cl); j++ {
				switch cl[j] {
				case 'i':
					inPlace = true
					break cluster
				case 'e':
					if rest := cl[j+1:]; rest != "" {
						scripts = append(scripts, rest)
					} else if i+1 < len(args) {
						i++
						scripts = append(scripts, args[i])
					}
					break cluster
				case 'f':
					writes = true
					if cl[j+1:] == "" {
						i++
					}
					break cluster
				case 'l':
					if cl[j+1:] == "" {
						i++
					}
					break cluster
				}
			}
		default:
			operands = append(operands, a)
		}
	}
	if len(scripts) == 0 && len(operands) > 0 {
		scripts = operands[:1]
		operands = operands[1:]
	}
	files = operands
	for _, s := range scripts {
		if sedWriteCmd.MatchString(s) || matchesAny(sedWriteFlag, s) {
			writes = true
		}
	}
	return
}

// awkInfo reports whether awk edits in place, its program text, and its file operands.
func awkInfo(args []string) (inPlace bool, prog string, files []string) {
	var operands []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-i" || a == "--include":
			if i+1 < len(args) && args[i+1] == "inplace" {
				inPlace = true
			}
			i++
		case a == "-f" || a == "--file":
			prog = "system(" // program text unknown: treat as unsafe
			i++
		case a == "-F" || a == "-v":
			i++
		case strings.HasPrefix(a, "-F") || strings.HasPrefix(a, "-v") || strings.HasPrefix(a, "--"):
		case a == "-":
			operands = append(operands, a)
		case len(a) > 1 && a[0] == '-':
		default:
			operands = append(operands, a)
		}
	}
	if prog == "" && len(operands) > 0 {
		prog, operands = operands[0], operands[1:]
	}
	for _, o := range operands {
		if !strings.Contains(o, "=") {
			files = append(files, o)
		}
	}
	return
}

func curlWrites(args []string) bool {
	for _, a := range args {
		switch {
		case a == "-o" || a == "-O" || a == "-J" || a == "-D" || a == "-c" || a == "-K" || a == "-T" && false:
			return true
		case strings.HasPrefix(a, "--output"), strings.HasPrefix(a, "--remote-name"), strings.HasPrefix(a, "--dump-header"),
			strings.HasPrefix(a, "--cookie-jar"), strings.HasPrefix(a, "--trace"), strings.HasPrefix(a, "--stderr"),
			strings.HasPrefix(a, "--config"):
			return true
		case len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsAny(a[1:], "oOJDcK"):
			// short cluster such as -sSLo; -H and -d take values that may contain these letters, so this is conservative
			if !strings.ContainsAny(a[1:], "HdXAuUeCmrxyYzbwQ") || strings.ContainsAny(a[1:], "oOJ") {
				return true
			}
		}
	}
	return false
}

// ---- name tables ----

var unrecoverableNames = map[string]string{
	"kill": "ends processes; process state cannot be captured", "killall": "ends processes; process state cannot be captured",
	"pkill": "ends processes; process state cannot be captured", "skill": "ends processes; process state cannot be captured",
	"xkill":  "ends processes; process state cannot be captured",
	"umount": "changes what is mounted", "mount": "changes what is mounted", "fdisk": "changes the partition table",
	"parted": "changes the partition table", "mkswap": "changes a swap area", "swapoff": "changes swap", "swapon": "changes swap",
	"losetup": "changes loop devices", "cryptsetup": "changes encrypted volumes", "wipefs": "destroys filesystem signatures",
	"blkdiscard": "discards device contents", "lvremove": "removes a logical volume", "vgremove": "removes a volume group",
	"pvremove": "removes a physical volume", "reboot": "restarts the machine", "shutdown": "stops the machine",
	"poweroff": "stops the machine", "halt": "stops the machine", "init": "changes runlevel", "telinit": "changes runlevel",
	"useradd": "changes accounts", "userdel": "changes accounts", "usermod": "changes accounts", "groupadd": "changes accounts",
	"groupdel": "changes accounts", "groupmod": "changes accounts", "passwd": "changes accounts", "chpasswd": "changes accounts",
	"adduser": "changes accounts", "deluser": "changes accounts", "modprobe": "changes kernel modules",
	"insmod": "changes kernel modules", "rmmod": "changes kernel modules", "at": "schedules jobs", "atrm": "removes scheduled jobs",
	"wipe": "destroys content on purpose", "srm": "destroys content on purpose",
}

var interpreters = setOf(`python python2 python3 perl ruby node nodejs php lua Rscript julia tclsh expect pwsh deno bun osascript`)

type stateTool struct {
	ro  func(args []string) bool
	why string
}

func firstOperand(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func operandAt(args []string, n int) string {
	i := 0
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if i == n {
			return a
		}
		i++
	}
	return ""
}

func roSub(subs ...string) func([]string) bool {
	set := setOf(strings.Join(subs, " "))
	return func(args []string) bool { return set[firstOperand(args)] }
}

func roSubOrFlag(flags []string, subs ...string) func([]string) bool {
	sub := roSub(subs...)
	return func(args []string) bool { return sub(args) || hasAny(args, flags...) }
}

const pkgWhy = "changes package or service state in a way that is not modelled"

var stateTools = map[string]stateTool{
	"apt":       {roSubOrFlag([]string{"-s", "--simulate", "--dry-run", "--just-print", "--no-act"}, "list", "search", "show", "policy", "depends", "rdepends", "showpkg", "madison", "changelog"), pkgWhy},
	"apt-get":   {roSubOrFlag([]string{"-s", "--simulate", "--dry-run", "--just-print", "--no-act"}, "check", "changelog"), pkgWhy},
	"aptitude":  {roSubOrFlag([]string{"-s", "--simulate"}, "search", "show", "why", "why-not"), pkgWhy},
	"apt-cache": {func([]string) bool { return true }, pkgWhy},
	"dpkg": {func(a []string) bool {
		return hasAny(a, "-l", "--list", "-L", "--listfiles", "-s", "--status", "-S", "--search", "-p", "--print-avail", "--get-selections", "--version", "-V", "--verify", "--audit", "-C")
	}, pkgWhy},
	"yum":    {roSub("list", "info", "search", "provides", "repolist", "check-update", "repoquery", "deplist", "history", "help"), pkgWhy},
	"dnf":    {roSub("list", "info", "search", "provides", "repolist", "check-update", "repoquery", "deplist", "history", "help"), pkgWhy},
	"zypper": {roSub("list-updates", "lu", "search", "se", "info", "if", "lr", "repos", "packages", "pa", "patches", "pch"), pkgWhy},
	"pacman": {func(a []string) bool {
		return len(a) > 0 && (strings.HasPrefix(a[0], "-Q") || a[0] == "-Ss" || a[0] == "-Si" || a[0] == "-Sl")
	}, pkgWhy},
	"snap":    {roSub("list", "info", "find", "version", "changes", "services"), pkgWhy},
	"flatpak": {roSub("list", "info", "search", "remotes", "history"), pkgWhy},
	"pip":     {roSub("list", "show", "freeze", "check", "search", "config", "index", "debug", "inspect", "help"), pkgWhy},
	"pip3":    {roSub("list", "show", "freeze", "check", "search", "config", "index", "debug", "inspect", "help"), pkgWhy},
	"npm":     {roSub("ls", "list", "view", "info", "outdated", "root", "prefix", "bin", "ping", "whoami", "explain", "why", "search", "help", "config"), pkgWhy},
	"yarn":    {roSub("list", "info", "why", "outdated", "help"), pkgWhy},
	"gem":     {roSub("list", "search", "info", "environment", "which", "contents", "dependency", "help"), pkgWhy},
	"cargo":   {roSub("search", "tree", "metadata", "version", "help", "locate-project"), pkgWhy},
	"brew":    {roSub("list", "info", "search", "outdated", "deps", "uses", "config", "doctor", "leaves", "--prefix", "--cellar"), pkgWhy},
	"systemctl": {roSubOrFlag([]string{"--version"}, "status", "is-active", "is-enabled", "is-failed", "is-system-running", "list-units",
		"list-unit-files", "list-timers", "list-sockets", "list-dependencies", "list-jobs", "show", "cat", "get-default", "help"), pkgWhy},
	"service": {func(a []string) bool { return hasAny(a, "--status-all") || (len(a) > 0 && a[len(a)-1] == "status") }, pkgWhy},
	"docker": {func(a []string) bool {
		f := firstOperand(a)
		if roSub("ps", "images", "inspect", "logs", "version", "info", "stats", "top", "port", "diff", "history", "search", "events")(a) {
			return true
		}
		switch f {
		case "image", "container", "volume", "network", "system":
			return setOf("ls list inspect history df")[operandAt(a, 1)]
		}
		return false
	}, pkgWhy},
	"kubectl": {roSub("get", "describe", "logs", "version", "top", "explain", "api-resources", "api-versions", "cluster-info"), pkgWhy},
	"journalctl": {func(a []string) bool {
		return !hasPrefixAny(a, "--vacuum", "--rotate", "--flush", "--sync", "--relinquish-var", "--setup-keys")
	}, pkgWhy},
	"crontab": {func(a []string) bool { return hasAny(a, "-l") }, "changes scheduled jobs"},
	"ip": {func(a []string) bool {
		for _, w := range a {
			switch w {
			case "add", "del", "delete", "set", "flush", "replace", "change", "append", "up", "down":
				return false
			}
		}
		return true
	}, "changes network state"},
	"ifconfig":  {func(a []string) bool { return len(parseOpts(a, "").operands) <= 1 }, "changes network state"},
	"ufw":       {roSub("status", "version", "show"), "changes the firewall"},
	"iptables":  {func(a []string) bool { return hasAny(a, "-L", "-S", "--list", "--list-rules") }, "changes the firewall"},
	"ip6tables": {func(a []string) bool { return hasAny(a, "-L", "-S", "--list", "--list-rules") }, "changes the firewall"},
	"nft":       {roSub("list"), "changes the firewall"},
	"sysctl": {func(a []string) bool {
		for _, w := range a {
			if strings.Contains(w, "=") || w == "-w" || w == "-p" || w == "--write" || w == "--load" {
				return false
			}
		}
		return true
	}, "changes kernel parameters"},
}

func hasPrefixAny(args []string, prefixes ...string) bool {
	for _, a := range args {
		for _, p := range prefixes {
			if strings.HasPrefix(a, p) {
				return true
			}
		}
	}
	return false
}

func (st *state) stateTool(c *call, t stateTool) {
	if c.aerr == nil && t.ro(c.args) {
		return
	}
	switch c.name {
	case "apt", "apt-get":
		st.aptTool(c, c.name)
	case "dpkg":
		st.dpkgTool(c)
	case "systemctl":
		st.systemctlTool(c)
	case "service":
		st.serviceTool(c)
	default:
		st.unrec(c, t.why)
	}
}

// ---- mutating rules ----

type rule func(st *state, c *call)

var rules map[string]rule

func init() {
	rules = map[string]rule{
		"rm": ruleRm, "unlink": ruleRm, "rmdir": ruleRmdir, "mv": ruleMv, "cp": ruleCp, "install": ruleInstall,
		"ln": ruleLn, "mkdir": ruleMkdir, "touch": ruleTouch, "chmod": ruleChmod, "chown": ruleChown, "chgrp": ruleChown,
		"sed": ruleSed, "awk": ruleAwk, "gawk": ruleAwk, "mawk": ruleAwk, "nawk": ruleAwk, "truncate": ruleTruncate,
		"tee": ruleTee, "dd": ruleDd, "shred": ruleShred, "sort": ruleSort, "uniq": ruleUniq,
		"gzip": ruleCompress, "gunzip": ruleCompress, "bzip2": ruleCompress, "bunzip2": ruleCompress, "xz": ruleCompress,
		"unxz": ruleCompress, "zstd": ruleCompress, "unzstd": ruleCompress,
		"curl": ruleCurl, "wget": ruleWget, "tar": ruleTar, "unzip": ruleUnzip, "rsync": ruleRsync, "git": ruleGit,
		"rename": ruleRename, "prename": ruleRename,
	}
}

func (st *state) needArgs(c *call) bool {
	if c.aerr != nil {
		st.unresolved(c, "arguments could not be determined: "+c.aerr.Error())
		return false
	}
	return true
}

func isSystemTop(p string) bool { return p == "/" || filepath.Dir(p) == "/" }

func ruleRm(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "")
	rec := o.has("r", "R", "recursive")
	for _, op := range o.operands {
		p := c.abs(op)
		fi, ok := st.lstat(p)
		if !ok || (fi.IsDir() && !rec) {
			continue
		}
		if rec && isSystemTop(p) {
			st.unrec(c, "removes a top-level system directory "+p)
			continue
		}
		st.add(c, Effect{Kind: Remove, Path: p})
	}
}

func ruleRmdir(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	for _, op := range parseOpts(c.args, "").operands {
		p := c.abs(op)
		if fi, ok := st.lstat(p); ok && fi.IsDir() {
			if ents, err := os.ReadDir(p); err == nil && len(ents) == 0 {
				st.add(c, Effect{Kind: Remove, Path: p})
			}
		}
	}
}

// destFor works out, for mv, cp, and ln, where each source lands.
func (st *state) destFor(c *call, ops []string, o optSet) (srcs []string, dest string, destIsDir bool, ok bool) {
	if t, has := o.val("t", "target-directory"); has {
		dest, srcs = c.abs(t), ops
	} else {
		if len(ops) < 2 {
			return nil, "", false, false
		}
		dest, srcs = c.abs(ops[len(ops)-1]), ops[:len(ops)-1]
	}
	if st.isDir(dest) && !o.has("T", "no-target-directory") {
		destIsDir = true
	}
	return srcs, dest, destIsDir, true
}

func ruleMv(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "tS", "target-directory", "suffix")
	srcs, dest, destIsDir, ok := st.destFor(c, o.operands, o)
	if !ok {
		return
	}
	for _, s := range srcs {
		sp := c.abs(s)
		sfi, ok := st.lstat(sp)
		if !ok {
			continue
		}
		target := dest
		if destIsDir || len(srcs) > 1 {
			target = filepath.Join(dest, filepath.Base(sp))
		}
		if target == sp {
			continue
		}
		if _, exists := st.lstat(target); exists {
			if o.has("n", "no-clobber") {
				continue
			}
			st.add(c, Effect{Kind: Write, Path: target})
			if bp, ok := backupPath(c.args, target); ok {
				st.createOrOverwrite(c, bp)
			}
		} else {
			st.add(c, Effect{Kind: Create, Path: target, Dir: sfi.IsDir()})
		}
		st.add(c, Effect{Kind: Remove, Path: sp, MovedTo: target})
	}
}

func (st *state) copyInto(c *call, sp, target string, o optSet) {
	fi, ok := st.lstat(sp)
	if !ok {
		return
	}
	put := func(dst string) {
		if _, exists := st.lstat(dst); exists {
			if !o.has("n", "no-clobber") {
				st.add(c, Effect{Kind: Write, Path: dst})
			}
		} else {
			st.add(c, Effect{Kind: Create, Path: dst})
		}
	}
	if !fi.IsDir() {
		put(target)
		return
	}
	if !o.has("r", "R", "a", "recursive", "archive") {
		return
	}
	count := 0
	_ = filepath.WalkDir(sp, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if count++; count > st.a.MaxItems {
			st.unresolved(c, errTooManyItems.Error())
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(sp, p)
		dst := filepath.Join(target, rel)
		if d.IsDir() {
			if _, exists := st.lstat(dst); !exists {
				st.add(c, Effect{Kind: Create, Path: dst, Dir: true})
			}
			return nil
		}
		put(dst)
		return nil
	})
}

func ruleCp(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "tS", "target-directory", "suffix")
	srcs, dest, destIsDir, ok := st.destFor(c, o.operands, o)
	if !ok {
		return
	}
	for _, s := range srcs {
		sp := c.abs(s)
		target := dest
		if destIsDir || len(srcs) > 1 {
			target = filepath.Join(dest, filepath.Base(sp))
		}
		if hasAny(c.args, "--parents") {
			// The whole source path is recreated under the destination directory.
			target = filepath.Join(dest, strings.TrimLeft(filepath.Clean(s), "/"))
			var missing []string
			for d := filepath.Dir(target); d != dest && strings.HasPrefix(d, dest+"/"); d = filepath.Dir(d) {
				if _, exists := st.lstat(d); !exists {
					missing = append([]string{d}, missing...)
				}
			}
			for _, d := range missing {
				st.add(c, Effect{Kind: Create, Path: d, Dir: true})
			}
		}
		if bp, ok := backupPath(c.args, target); ok {
			if _, exists := st.lstat(target); exists {
				st.createOrOverwrite(c, bp)
			}
		}
		st.copyInto(c, sp, target, o)
	}
}

func ruleInstall(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "mgotS", "mode", "owner", "group", "target-directory", "suffix")
	if o.has("d", "directory") {
		for _, op := range o.operands {
			if _, ok := st.lstat(c.abs(op)); !ok {
				st.add(c, Effect{Kind: Create, Path: c.abs(op), Dir: true})
			}
		}
		return
	}
	srcs, dest, destIsDir, ok := st.destFor(c, o.operands, o)
	if !ok {
		return
	}
	for _, s := range srcs {
		sp := c.abs(s)
		target := dest
		if destIsDir || len(srcs) > 1 {
			target = filepath.Join(dest, filepath.Base(sp))
		}
		st.copyInto(c, sp, target, o)
	}
}

func ruleLn(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "tS", "target-directory", "suffix")
	ops := o.operands
	if len(ops) == 0 {
		return
	}
	var links []string
	if t, has := o.val("t", "target-directory"); has {
		for _, op := range ops {
			links = append(links, filepath.Join(c.abs(t), filepath.Base(op)))
		}
	} else if len(ops) == 1 {
		links = []string{filepath.Join(c.sc.wd, filepath.Base(ops[0]))}
	} else {
		last := c.abs(ops[len(ops)-1])
		if st.isDir(last) {
			for _, op := range ops[:len(ops)-1] {
				links = append(links, filepath.Join(last, filepath.Base(op)))
			}
		} else {
			links = []string{last}
		}
	}
	for _, l := range links {
		if _, exists := st.lstat(l); exists {
			if o.has("f", "force") {
				// The link replaces the file with a different inode. Writing the old bytes
				// back through the new link would corrupt what it points at, so the old
				// file is captured whole (a hardlink to its inode) and put back by rename.
				st.add(c, Effect{Kind: Remove, Path: l})
			}
			continue
		}
		st.add(c, Effect{Kind: Create, Path: l})
	}
}

func ruleMkdir(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "m", "mode")
	for _, op := range o.operands {
		p := c.abs(op)
		var missing []string
		for q := p; ; q = filepath.Dir(q) {
			if _, ok := st.lstat(q); ok || q == "/" || q == "." {
				break
			}
			missing = append(missing, q)
			if !o.has("p", "parents") {
				break
			}
		}
		for i := len(missing) - 1; i >= 0; i-- {
			st.add(c, Effect{Kind: Create, Path: missing[i], Dir: true})
		}
	}
}

func ruleTouch(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "dtrD", "date", "reference")
	if o.has("c", "no-create") {
		return
	}
	for _, op := range o.operands {
		if p := c.abs(op); !st.exists(p) {
			st.add(c, Effect{Kind: Create, Path: p})
		}
	}
}

var chmodFlag = regexp.MustCompile(`^-[RfvcHLP]+$`)

var octalMode = regexp.MustCompile(`^[0-7]{3,4}$`)

func ruleChmod(st *state, c *call) { metaRule(st, c, false) }
func ruleChown(st *state, c *call) { metaRule(st, c, true) }

func metaRule(st *state, c *call, owner bool) {
	if !st.needArgs(c) {
		return
	}
	rec := false
	var rest []string
	for i, a := range c.args {
		switch {
		case a == "--":
			rest = append(rest, c.args[i+1:]...)
			i = len(c.args)
		case a == "--recursive":
			rec = true
		case strings.HasPrefix(a, "--") && !strings.HasPrefix(a, "--reference"):
		case chmodFlag.MatchString(a):
			if strings.ContainsAny(a, "R") {
				rec = true
			}
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) < 2 {
		return
	}
	// An octal mode equal to the file's current permission bits changes nothing.
	unchanged := func(q string) bool {
		if owner || !octalMode.MatchString(rest[0]) {
			return false
		}
		fi, err := os.Lstat(q)
		if err != nil || fi.Mode()&fs.ModeSymlink != 0 {
			return false
		}
		want, _ := strconv.ParseUint(rest[0], 8, 32)
		return uint64(fi.Mode().Perm()) == want&0o777 && want <= 0o777
	}
	for _, op := range rest[1:] { // rest[0] is the mode or owner
		p := c.abs(op)
		if _, ok := st.lstat(p); !ok {
			continue
		}
		if !rec {
			if !unchanged(p) {
				st.add(c, Effect{Kind: Meta, Path: p})
			}
			continue
		}
		count := 0
		_ = filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if count++; count > st.a.MaxItems {
				st.unresolved(c, errTooManyItems.Error())
				return filepath.SkipAll
			}
			if !unchanged(q) {
				st.add(c, Effect{Kind: Meta, Path: q})
			}
			return nil
		})
	}
}

func ruleSed(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	in, writes, files := sedInfo(c.args)
	if writes && !in {
		st.opaque(c, "the sed script writes files or runs commands")
		return
	}
	if !in {
		return
	}
	suffix := sedBackupSuffix(c.args)
	if strings.ContainsAny(suffix, "*/") {
		st.unresolved(c, "the sed backup suffix names a pattern or another directory")
		return
	}
	for _, f := range files {
		if p := c.abs(f); st.exists(p) {
			st.add(c, Effect{Kind: Write, Path: p})
			if suffix != "" {
				st.createOrOverwrite(c, p+suffix)
			}
		}
	}
}

// sedBackupSuffix is the suffix sed -i.SUF or --in-place=SUF appends to make a backup copy,
// or "" when it makes none.
func sedBackupSuffix(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return ""
		case strings.HasPrefix(a, "--in-place="):
			return strings.TrimPrefix(a, "--in-place=")
		case strings.HasPrefix(a, "--"):
		case len(a) > 1 && a[0] == '-':
			cl := a[1:]
			for j := 0; j < len(cl); j++ {
				switch cl[j] {
				case 'i':
					return cl[j+1:]
				case 'e', 'f', 'l':
					j = len(cl)
				}
			}
		}
	}
	return ""
}

// createOrOverwrite records that a command leaves a file at p: a new one if nothing is there,
// otherwise an overwrite.
func (st *state) createOrOverwrite(c *call, p string) {
	if _, exists := st.lstat(p); exists {
		st.add(c, Effect{Kind: Write, Path: p})
	} else {
		st.add(c, Effect{Kind: Create, Path: p})
	}
}

// backupPath is where mv -b, cp -b and their kin put the copy of a destination they are about
// to replace, and whether they make one. GNU semantics: a simple suffix (~ unless -S), or
// numbered .~N~ names when asked for or when numbered backups already exist.
func backupPath(args []string, dest string) (string, bool) {
	on, control, suffix := false, "", "~"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			i = len(args)
		case a == "--backup":
			on = true
		case strings.HasPrefix(a, "--backup="):
			on, control = true, strings.TrimPrefix(a, "--backup=")
		case a == "-S" && i+1 < len(args):
			i++
			suffix = args[i]
		case strings.HasPrefix(a, "--suffix="):
			suffix = strings.TrimPrefix(a, "--suffix=")
		case len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.Trim(a[1:], "abdfilnprsuvxPRTHLUZ") == "" && strings.Contains(a, "b"):
			on = true
		}
	}
	if !on {
		return "", false
	}
	numbered := func() string {
		max := 0
		entries, _ := os.ReadDir(filepath.Dir(dest))
		for _, e := range entries {
			var n int
			if _, err := fmt.Sscanf(e.Name(), filepath.Base(dest)+".~%d~", &n); err == nil && n > max {
				max = n
			}
		}
		return fmt.Sprintf("%s.~%d~", dest, max+1)
	}
	switch control {
	case "none", "off":
		return "", false
	case "numbered", "t":
		return numbered(), true
	case "simple", "never":
		return dest + suffix, true
	}
	// "existing" (the default): numbered if numbered backups are already there.
	entries, _ := os.ReadDir(filepath.Dir(dest))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), filepath.Base(dest)+".~") {
			return numbered(), true
		}
	}
	return dest + suffix, true
}

func ruleAwk(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	in, prog, files := awkInfo(c.args)
	if !in {
		if awkUnsafe.MatchString(prog) || prog == "" {
			st.opaque(c, "the awk program writes files or runs commands")
		}
		return
	}
	for _, f := range files {
		if p := c.abs(f); st.exists(p) {
			st.add(c, Effect{Kind: Write, Path: p})
		}
	}
}

func (st *state) writeTo(c *call, p string) {
	if st.exists(p) {
		st.add(c, Effect{Kind: Write, Path: p})
	} else {
		st.add(c, Effect{Kind: Create, Path: p})
	}
}

func ruleTruncate(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "sr", "size", "reference")
	for _, op := range o.operands {
		p := c.abs(op)
		if st.exists(p) || !o.has("c", "no-create") {
			st.writeTo(c, p)
		}
	}
}

func ruleTee(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	for _, op := range parseOpts(c.args, "").operands {
		if op == "-" || strings.HasPrefix(c.abs(op), "/dev/") {
			continue
		}
		st.writeTo(c, c.abs(op))
	}
}

func ruleDd(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	for _, a := range c.args {
		if !strings.HasPrefix(a, "of=") {
			continue
		}
		p := c.abs(strings.TrimPrefix(a, "of="))
		if strings.HasPrefix(p, "/dev/") {
			if !deviceOK(p) {
				st.unrec(c, "writes to a raw device "+p)
			}
			return
		}
		st.writeTo(c, p)
	}
}

func ruleShred(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	for _, op := range parseOpts(c.args, "nsS", "iterations", "size", "random-source").operands {
		if st.exists(c.abs(op)) {
			st.unrec(c, "shred destroys content on purpose")
			return
		}
	}
}

func ruleSort(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "otkTSKt", "output", "temporary-directory", "key", "field-separator", "buffer-size")
	if v, ok := o.val("o", "output"); ok {
		st.writeTo(c, c.abs(v))
	}
}

func ruleUniq(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "fsw", "skip-fields", "skip-chars", "check-chars")
	if len(o.operands) >= 2 {
		st.writeTo(c, c.abs(o.operands[1]))
	}
}

var compressExt = map[string]string{"gzip": ".gz", "gunzip": ".gz", "bzip2": ".bz2", "bunzip2": ".bz2", "xz": ".xz", "unxz": ".xz", "zstd": ".zst", "unzstd": ".zst"}

func ruleCompress(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "S", "suffix")
	if o.has("c", "stdout", "to-stdout", "t", "test", "l", "list") {
		return
	}
	decompress := strings.HasPrefix(c.name, "un") || strings.HasPrefix(c.name, "gun") || strings.HasPrefix(c.name, "bun") || o.has("d", "decompress", "uncompress")
	ext := compressExt[c.name]
	keep := o.has("k", "keep")
	for _, op := range o.operands {
		p := c.abs(op)
		if _, ok := st.lstat(p); !ok {
			continue
		}
		var out string
		if decompress {
			if !strings.HasSuffix(p, ext) {
				continue
			}
			out = strings.TrimSuffix(p, ext)
		} else {
			out = p + ext
		}
		st.writeTo(c, out)
		if !keep {
			// Not a move: the bytes at the destination are a transformation of the
			// original, so moving it back would return compressed data. The original
			// has to be captured like any other deletion.
			st.add(c, Effect{Kind: Remove, Path: p})
		}
	}
}

// curlSends reports whether curl would send data or change state on the remote end:
// a method other than GET or HEAD, a request body, a form, or an upload. -G turns
// data options into a query string, which is a GET.
func curlSends(args []string) bool {
	get := false
	sends := false
	for i, a := range args {
		switch {
		case a == "-G" || a == "--get":
			get = true
		case a == "-X" || a == "--request":
			if i+1 < len(args) && args[i+1] != "GET" && args[i+1] != "HEAD" {
				sends = true
			}
		case strings.HasPrefix(a, "-X") && len(a) > 2 && a != "-XGET" && a != "-XHEAD",
			strings.HasPrefix(a, "--request=") && a != "--request=GET" && a != "--request=HEAD":
			sends = true
		case a == "-d" || strings.HasPrefix(a, "--data") || a == "-F" || strings.HasPrefix(a, "--form") ||
			a == "-T" || a == "--upload-file" || strings.HasPrefix(a, "--json"):
			sends = sends || !get || a == "-F" || a == "-T" || a == "--upload-file" || strings.HasPrefix(a, "--form")
		}
	}
	return sends
}

func ruleCurl(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	if curlSends(c.args) {
		st.unrec(c, "curl sends data to or changes state on a remote system")
	}
	o := parseOpts(c.args, "oHdXAuUeCmrxyYzbwQKDc", "output", "header", "data", "request", "user-agent", "user", "output-dir", "dump-header", "cookie-jar")
	dir := c.sc.wd
	if v, ok := o.val("output-dir"); ok {
		dir = c.abs(v)
	}
	if v, ok := o.val("o", "output"); ok && v != "-" {
		st.writeTo(c, filepath.Join(dir, v))
	}
	if o.has("O", "remote-name") {
		for _, u := range o.operands {
			base := filepath.Base(strings.SplitN(strings.SplitN(u, "?", 2)[0], "#", 2)[0])
			st.writeTo(c, filepath.Join(dir, base))
		}
	}
	if v, ok := o.val("D", "dump-header"); ok && v != "-" {
		st.writeTo(c, c.abs(v))
	}
	if v, ok := o.val("c", "cookie-jar"); ok && v != "-" {
		st.writeTo(c, c.abs(v))
	}
	if o.has("K", "config") || o.has("trace", "trace-ascii", "stderr") {
		st.unresolved(c, "curl writes to a file named in a config or trace option")
	}
}

func ruleWget(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "OPoaeUTtwlQ", "output-document", "directory-prefix", "output-file", "append-output", "quota")
	dir := c.sc.wd
	if v, ok := o.val("P", "directory-prefix"); ok {
		dir = c.abs(v)
	}
	if v, ok := o.val("O", "output-document"); ok {
		if v != "-" {
			st.writeTo(c, c.abs(v)) // -O is relative to the working directory, not -P
		}
		return
	}
	for _, f := range []string{"o", "output-file", "a", "append-output"} {
		if v, ok := o.val(f); ok && v != "" {
			st.writeTo(c, c.abs(v)) // wget's own log
		}
	}
	if o.has("r", "recursive", "m", "mirror", "p", "page-requisites", "i", "input-file") || hasAny(c.args, "--content-disposition", "--trust-server-names") {
		st.unresolved(c, "wget takes its file names from the server or another file")
		return
	}
	noClobber := hasAny(c.args, "-nc", "--no-clobber")
	overwrites := o.has("N", "timestamping", "c", "continue") && !noClobber
	for _, u := range o.operands {
		target := filepath.Join(dir, wgetName(u))
		if !st.exists(target) {
			st.add(c, Effect{Kind: Create, Path: target})
			continue
		}
		switch {
		case noClobber:
		case overwrites:
			st.add(c, Effect{Kind: Write, Path: target}) // -N replaces a newer copy, -c appends to it
		default:
			// wget never replaces an existing file by default: it saves to NAME.1, NAME.2,
			// and so on. Recording a creation of NAME here would make undo delete the
			// file that was already there.
			for n := 1; ; n++ {
				if p := fmt.Sprintf("%s.%d", target, n); !st.exists(p) {
					st.add(c, Effect{Kind: Create, Path: p})
					break
				}
			}
		}
	}
}

// wgetName is the file name wget saves a URL to: the last path segment, or index.html
// when the path is empty or ends in a slash.
func wgetName(u string) string {
	p := strings.SplitN(strings.SplitN(u, "?", 2)[0], "#", 2)[0]
	if i := strings.Index(p, "://"); i >= 0 {
		p = p[i+3:]
		if j := strings.IndexByte(p, '/'); j >= 0 {
			p = p[j:]
		} else {
			p = ""
		}
	}
	if p == "" || strings.HasSuffix(p, "/") {
		return "index.html"
	}
	return filepath.Base(p)
}
