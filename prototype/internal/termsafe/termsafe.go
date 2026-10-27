// Package termsafe makes text from outside synapse safe to write to a
// terminal. The CLI's text mode and the TUI both use it, so a command's output
// is shown, not obeyed, in every interface (docs/cli-breaking.md).
package termsafe

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Display makes text from outside synapse (a command's output, the model's
// command and reply) safe to write to a terminal in text mode. Control bytes
// are shown, not obeyed: ESC becomes ^[ and the rest of C0 and DEL take caret
// notation, C1 controls and invalid UTF-8 become visible escapes, and a lone
// carriage return becomes ^M. Without this, output can clear the screen,
// retitle the window, hide text, or draw a fake prompt over the real one.
// Newlines and tabs pass through, and CRLF becomes a newline. --json is not
// filtered: it carries the raw bytes, escaped by the encoder.
func Display(s string) string {
	clean := true
	for i := 0; i < len(s); i++ {
		if b := s[i]; (b < 0x20 && b != '\n' && b != '\t') || b >= 0x7f {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[0])
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20:
			b.WriteByte('^')
			b.WriteByte(byte(r) + '@')
		case r == 0x7f:
			b.WriteString("^?")
		case r >= 0x80 && r <= 0x9f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(s[:size])
		}
		s = s[size:]
	}
	return b.String()
}
