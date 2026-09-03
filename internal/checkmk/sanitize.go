package checkmk

import "strings"

// clean strips terminal control characters from server-supplied text.
// Check output is written by whatever runs on a monitored host, and a
// stray or hostile ESC sequence in it would otherwise reach the user's
// terminal verbatim (OSC 52 clipboard writes, title changes, screen
// clears, ...). Newlines are kept; everything else in C0, DEL and the
// C1 range (0x80-0x9f, e.g. a bare CSI) is dropped. Tabs become a space.
func clean(s string) string {
	needsWork := false
	for _, r := range s {
		if (r < 0x20 && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			needsWork = true
			break
		}
	}
	if !needsWork {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return -1
		}
		return r
	}, s)
}
