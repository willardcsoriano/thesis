package effects

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Rules for the compressors (gzip, bzip2, xz, zstd and their un- names) and for zip.
// Each tool is parsed against the complete set of options its rule understands; an
// option outside that set may change what the command touches (an output directory, a
// list of names read from a file, a different format and so a different suffix), so it
// makes the command unresolved, which asks.

// Long option kinds for optSpec.long.
const (
	longFlag     = iota // takes no value
	longValue           // takes a value, attached with = or as the next argument
	longOptValue        // takes a value only when attached with =
)

// optSpec is every option a rule understands for one tool.
type optSpec struct {
	flags    string // single-letter options without a value
	values   string // single-letter options with a value: the rest of the cluster, or the next argument
	attached string // single-letter options whose value can only be attached (zstd -T0)
	long     map[string]int
}

// strictOpts parses args against sp. It returns the parsed options and, if some option
// is not in sp, that option, in which case the rest of the result must not be used.
func strictOpts(args []string, sp optSpec) (optSet, string) {
	o := optSet{flags: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			o.operands = append(o.operands, args[i+1:]...)
			return o, ""
		case strings.HasPrefix(a, "--"):
			name, val, has := strings.Cut(a[2:], "=")
			kind, known := sp.long[name]
			switch {
			case !known, kind == longFlag && has:
				return o, a
			case kind == longValue && !has:
				if i+1 >= len(args) {
					return o, a
				}
				i++
				val = args[i]
			}
			o.flags[name] = val
		case len(a) > 1 && a[0] == '-':
			cl := a[1:]
			for j := 0; j < len(cl); j++ {
				ch := string(cl[j])
				switch {
				case strings.Contains(sp.flags, ch):
					o.flags[ch] = ""
				case strings.Contains(sp.attached, ch):
					if cl[j+1:] == "" {
						return o, a
					}
					o.flags[ch] = cl[j+1:]
					j = len(cl)
				case strings.Contains(sp.values, ch):
					v := cl[j+1:]
					if v == "" {
						if i+1 >= len(args) {
							return o, a
						}
						i++
						v = args[i]
					}
					o.flags[ch] = v
					j = len(cl)
				default:
					return o, a
				}
			}
		default:
			o.operands = append(o.operands, a)
		}
	}
	return o, ""
}

func longs(kind int, names string) map[string]int {
	m := map[string]int{}
	for _, n := range strings.Fields(names) {
		m[n] = kind
	}
	return m
}

func mergeLongs(ms ...map[string]int) map[string]int {
	out := map[string]int{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// compressor describes one compression tool's naming: the suffix it adds, the suffixes
// it recognises when decompressing and what each becomes, and what it does by default
// with the input.
type compressor struct {
	ext string
	// decomp maps a recognised suffix to its replacement (.tgz becomes .tar).
	decomp map[string]string
	// unknownOut is the suffix bzip2 appends to a name it cannot derive the output from;
	// empty means the tool skips such a file with a warning.
	unknownOut string
	// keeps is set for zstd, which keeps the input unless --rm.
	keeps bool
	// customSuffix is the option that replaces ext (gzip and xz -S).
	customSuffix bool
	spec         optSpec
}

var (
	gzipLike = compressor{
		ext: ".gz", customSuffix: true,
		decomp: map[string]string{".gz": "", "-gz": "", ".z": "", "-z": "", "_z": "", ".Z": "", ".tgz": ".tar", ".taz": ".tar"},
		spec: optSpec{flags: "cdfhklLnNqrtvV0123456789", values: "S", long: mergeLongs(
			longs(longFlag, "stdout to-stdout decompress uncompress force help keep list license no-name name quiet recursive rsyncable synchronous test verbose version fast best"),
			longs(longValue, "suffix"))},
	}
	bzip2Like = compressor{
		ext: ".bz2", unknownOut: ".out",
		decomp: map[string]string{".bz2": "", ".bz": "", ".tbz2": ".tar", ".tbz": ".tar"},
		spec: optSpec{flags: "cdzkftqvsLV0123456789", long: longs(longFlag,
			"stdout decompress compress keep force test quiet verbose small license version fast best help")},
	}
	xzLike = compressor{
		ext: ".xz", customSuffix: true,
		decomp: map[string]string{".xz": "", ".txz": ".tar", ".lzma": "", ".tlz": ".tar"},
		spec: optSpec{flags: "zdtlkfceqvhHV0123456789", values: "SFCTM", long: mergeLongs(
			longs(longFlag, "compress decompress uncompress test list keep force stdout to-stdout extreme quiet verbose help long-help version no-sparse single-stream no-adjust robot ignore-check"),
			longs(longValue, "suffix format check threads memlimit memlimit-compress memlimit-decompress memory block-size"))},
	}
	zstdLike = compressor{
		ext: ".zst", keeps: true,
		// zstd decompresses the other formats too when it was built with them; counting
		// them can only make the analysis record more.
		decomp: map[string]string{".zst": "", ".tzst": ".tar", ".gz": "", ".tgz": ".tar", ".xz": "", ".txz": ".tar", ".lzma": "", ".lz4": ""},
		spec: optSpec{flags: "zdtlkfcqvhHVr0123456789", values: "oD", attached: "TMB", long: mergeLongs(
			longs(longFlag, "compress decompress uncompress test list keep rm force stdout quiet verbose help version ultra single-thread no-progress progress sparse no-sparse check no-check rsyncable exclude-compressed"),
			longs(longOptValue, "long fast adapt"),
			longs(longValue, "threads memory"))},
	}
	compressors = map[string]compressor{
		"gzip": gzipLike, "gunzip": gzipLike, "bzip2": bzip2Like, "bunzip2": bzip2Like,
		"xz": xzLike, "unxz": xzLike, "zstd": zstdLike, "unzstd": zstdLike,
	}
)

func ruleCompress(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	tool := compressors[c.name]
	o, bad := strictOpts(c.args, tool.spec)
	if bad != "" {
		st.unresolved(c, c.name+" option "+bad+" is not modelled, and may change which files it writes")
		return
	}
	if o.has("c", "stdout", "to-stdout", "t", "test", "l", "list", "h", "help", "H", "long-help", "V", "version", "L", "license") {
		return // writes only to standard output, or only reads
	}
	if f, ok := o.val("F", "format"); ok && f != "auto" && f != "xz" {
		st.unresolved(c, "xz in another format writes a different suffix")
		return
	}
	decompress := strings.HasPrefix(c.name, "un") || strings.HasPrefix(c.name, "gun") || strings.HasPrefix(c.name, "bun") || o.has("d", "decompress", "uncompress")
	if o.has("z", "compress") {
		decompress = false
	}
	if decompress && o.has("N", "name") && c.name != "bzip2" && c.name != "bunzip2" {
		st.unresolved(c, "gunzip -N takes the output name from inside the file")
		return
	}
	ext := tool.ext
	if s, ok := o.val("S", "suffix"); ok && tool.customSuffix {
		if s == "" || strings.Contains(s, "/") {
			st.unresolved(c, "the suffix is empty or names another directory")
			return
		}
		ext = s
	}
	out, hasOut := o.val("o")
	keep := o.has("k", "keep") || tool.keeps && !o.has("rm")
	recursive := o.has("r", "recursive")
	// one handles a single input file; a directory is skipped unless -r, as the tools do.
	one := func(p string) {
		var dst string
		switch {
		case hasOut:
			dst = c.abs(out)
		case decompress:
			var ok bool
			if dst, ok = decompressedName(p, ext, tool); !ok {
				return // the tool skips a name it cannot derive the output from
			}
		default:
			dst = p + ext
		}
		st.writeTo(c, dst)
		if !keep {
			// Not a move: the bytes at the destination are a transformation of the
			// original, so moving it back would return compressed data. The original
			// has to be captured like any other deletion.
			st.add(c, Effect{Kind: Remove, Path: p})
		}
	}
	for _, op := range o.operands {
		if op == "-" {
			continue // standard input to standard output
		}
		p := c.abs(op)
		fi, ok := st.lstat(p)
		if !ok {
			continue
		}
		if !fi.IsDir() {
			one(p)
			continue
		}
		if !recursive {
			continue
		}
		if _, made := st.virt.created[p]; made {
			st.unresolved(c, "the directory is made earlier on the line, so its files cannot be listed")
			continue
		}
		count := 0
		_ = filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if count++; count > st.a.MaxItems {
				st.unresolved(c, errTooManyItems.Error())
				return filepath.SkipAll
			}
			if st.exists(q) {
				one(q)
			}
			return nil
		})
	}
}

// decompressedName is where a decompressor writes p: the recognised suffix replaced,
// or bzip2's NAME.out, and false when the tool would skip p.
func decompressedName(p, ext string, tool compressor) (string, bool) {
	base := filepath.Base(p)
	if strings.HasSuffix(base, ext) && len(base) > len(ext) {
		repl := tool.decomp[ext]
		return strings.TrimSuffix(p, ext) + repl, true
	}
	for suf, repl := range tool.decomp {
		if strings.HasSuffix(base, suf) && len(base) > len(suf) {
			return strings.TrimSuffix(p, suf) + repl, true
		}
	}
	if tool.unknownOut != "" {
		return p + tool.unknownOut, true
	}
	return "", false
}

// ---- zip ----

// zipFlags are the single-letter zip options the rule understands. zip also has
// two-letter short options (-dc, -sf, -TT, ...), so a cluster is accepted only when
// it contains none of them (zipPairs); anything else asks.
const zipFlags = "rqjmufdDyXokvT0123456789"

var zipPairs = strings.Fields("AC AS db dc dd DF dg ds du dv FF FI FS fz ic la lf li ll MM nw RE sb sc sd sf so sp su sU sv tt TT UN ws")

var zipLongs = map[string]byte{
	"recurse-paths": 'r', "quiet": 'q', "junk-paths": 'j', "move": 'm', "update": 'u', "freshen": 'f',
	"delete": 'd', "no-dir-entries": 'D', "symlinks": 'y', "no-extra": 'X', "latest-time": 'o', "test": 'T', "verbose": 'v',
}

// zipArgs splits a zip command line into the archive, the input paths, and the
// option letters. -x and -i take a list of patterns up to the next option. bad names
// the first option the rule does not understand.
func zipArgs(args []string) (archive string, inputs []string, fl map[byte]bool, bad string) {
	fl = map[byte]bool{}
	var ops []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			ops = append(ops, args[i+1:]...)
			i = len(args)
		case a == "-x" || a == "-i" || a == "--exclude" || a == "--include":
			for i+1 < len(args) && !(strings.HasPrefix(args[i+1], "-") && len(args[i+1]) > 1) {
				i++
			}
		case strings.HasPrefix(a, "--"):
			ch, ok := zipLongs[a[2:]]
			if !ok {
				return "", nil, nil, a
			}
			fl[ch] = true
		case len(a) > 1 && a[0] == '-':
			for _, p := range zipPairs {
				if strings.Contains(a[1:], p) {
					return "", nil, nil, a
				}
			}
			for j := 1; j < len(a); j++ {
				if !strings.ContainsRune(zipFlags, rune(a[j])) {
					return "", nil, nil, a
				}
				fl[a[j]] = true
			}
		default:
			ops = append(ops, a)
		}
	}
	if len(ops) == 0 {
		return "", nil, fl, ""
	}
	return ops[0], ops[1:], fl, ""
}

// zipArchivePath is the file zip writes: the name given, with .zip added when its last
// component has no suffix, as Info-ZIP zip does.
func zipArchivePath(c *call, name string) string {
	if !strings.Contains(filepath.Base(name), ".") {
		name += ".zip"
	}
	return c.abs(name)
}

func ruleZip(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	archive, inputs, fl, bad := zipArgs(c.args)
	if bad != "" {
		st.unresolved(c, "zip option "+bad+" is not modelled, and may change which files it writes")
		return
	}
	if archive == "" {
		return // zip with no archive only prints its usage
	}
	ap := ""
	if archive != "-" {
		ap = zipArchivePath(c, archive)
		// An existing archive is rewritten (entries added, replaced, or deleted), so its
		// bytes are captured; a new one is a creation.
		if !fl['d'] || st.exists(ap) {
			st.writeTo(c, ap)
		}
	}
	if fl['d'] || !fl['m'] {
		return // -d names entries inside the archive, not files
	}
	// -m deletes each input once it is in the archive. The archive holds a copy, not
	// the same bytes, so each removal is captured like any other.
	for _, in := range inputs {
		if in == "-" {
			continue
		}
		if strings.ContainsAny(in, "*?[") {
			st.unresolved(c, "zip -m with a pattern it matches itself: which files it deletes cannot be listed")
			return
		}
		p := c.abs(in)
		if p == ap {
			continue
		}
		fi, ok := st.lstat(p)
		if !ok {
			continue
		}
		if fi.IsDir() && !fl['r'] && !emptyDir(p) {
			continue // without -r only the directory entry is added; a non-empty one stays
		}
		st.add(c, Effect{Kind: Remove, Path: p})
	}
}

func emptyDir(p string) bool {
	ents, err := os.ReadDir(p)
	return err == nil && len(ents) == 0
}
