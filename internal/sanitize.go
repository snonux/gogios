package internal

import (
	"strings"
	"unicode/utf8"
)

// sanitizeOutput makes plugin (and peer or federated) output safe to put in
// every report: the JSON report, the HTML page, the text report and the mail.
// encoding/json already escapes control characters, so the JSON report is
// valid either way, but the raw bytes also land in the HTML page (invalid
// HTML, terminal escapes in the mail) and in any consumer that renders the
// output without escaping it. So the output is normalised once, where it
// enters the state:
//   - invalid UTF-8 becomes U+FFFD,
//   - ANSI CSI sequences (colours, cursor moves: ESC '[' ... final byte) are
//     dropped, as they carry no text,
//   - CRLF and a lone CR become a newline; tab and newline are kept,
//   - every other C0 control (NUL, ESC, ...), DEL and C1 control becomes
//     U+FFFD, so the output still shows that something was there.
func sanitizeOutput(s string) string {
	if isCleanOutput(s) {
		return s // the common case: nothing to copy
	}
	s = strings.ToValidUTF8(s, string(utf8.RuneError))
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\x1b' && i+1 < len(s) && s[i+1] == '[':
			i += csiLength(s[i:])
			continue
		case r == '\r':
			if i+1 >= len(s) || s[i+1] != '\n' {
				sb.WriteByte('\n') // a lone CR; a CR of CRLF is dropped
			}
		case r == '\t' || r == '\n' || !isControl(r):
			sb.WriteRune(r)
		default:
			sb.WriteRune(utf8.RuneError)
		}
		i += size
	}
	return sb.String()
}

// isCleanOutput reports whether s is valid UTF-8 without any control
// character other than tab and newline, i.e. sanitizeOutput would keep it.
func isCleanOutput(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r != '\t' && r != '\n' && isControl(r) {
			return false
		}
	}
	return true
}

// isControl reports C0 controls, DEL and C1 controls.
func isControl(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}

// csiLength returns the byte length of the ANSI CSI sequence at the start of
// s ("\x1b[" followed by parameter/intermediate bytes 0x20-0x3f and one final
// byte 0x40-0x7e). An unterminated sequence runs to the first byte that
// cannot belong to it, so a stray "ESC [" never swallows the text after it.
func csiLength(s string) int {
	for i := 2; i < len(s); i++ {
		c := s[i]
		if c >= 0x40 && c <= 0x7e {
			return i + 1
		}
		if c < 0x20 || c > 0x3f {
			return i
		}
	}
	return len(s)
}
