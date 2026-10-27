package termsafe

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDisplay(t *testing.T) {
	for in, want := range map[string]string{
		"plain text\n\tindented":     "plain text\n\tindented",
		"\x1b[2Jcleared":             "^[[2Jcleared",
		"\x1b]0;title\x07":           "^[]0;title^G",
		"windows\r\nline":            "windows\nline",
		"progress 10%\rprogress 20%": "progress 10%^Mprogress 20%",
		"del\x7f":                    "del^?",
		"c1 \u009b31m":               `c1 \u009b31m`,
		"bad \xff byte":              `bad \xff byte`,
		"nul\x00":                    "nul^@",
		"unicode é 日本 ✓":             "unicode é 日本 ✓",
	} {
		if got := Display(in); got != want {
			t.Errorf("Display(%q) = %q, want %q", in, got, want)
		}
	}
}

func FuzzDisplay(f *testing.F) {
	for _, s := range []string{"", "a\x1b[31mb", "\r\n\r", "\xff\xfe", "\u009b", "ok\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Display(in)
		if !utf8.ValidString(out) {
			t.Fatalf("Display(%q) is not valid UTF-8", in)
		}
		for _, r := range out {
			if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Fatalf("Display(%q) = %q still holds control %U", in, out, r)
			}
		}
		if strings.Count(out, "\n") > strings.Count(in, "\n") {
			t.Fatalf("Display(%q) added lines", in)
		}
	})
}
