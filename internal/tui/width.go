package tui

import (
	"strings"
	"unicode"
)

// RuneWidth returns the terminal cell width (0, 1, or 2) of a Unicode rune
// without pulling external dependencies into go.mod.
func RuneWidth(r rune) int {
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if r >= 0xFE00 && r <= 0xFE0F {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

// isWideRune covers East Asian Wide / Fullwidth blocks (CJK ideographs,
// punctuation, Hangul, Kana) and common supplementary-plane emoji blocks.
func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F:
		return true
	case r == 0x2329 || r == 0x232A:
		return true
	case r >= 0x2E80 && r <= 0xA4CF && r != 0x303F:
		return true
	case r >= 0xAC00 && r <= 0xD7A3:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0xFE10 && r <= 0xFE19:
		return true
	case r >= 0xFE30 && r <= 0xFE6F:
		return true
	case r >= 0xFF01 && r <= 0xFF60:
		return true
	case r >= 0xFFE0 && r <= 0xFFE6:
		return true
	case r >= 0x1F300 && r <= 0x1F64F:
		return true
	case r >= 0x1F680 && r <= 0x1F6FF:
		return true
	case r >= 0x1F900 && r <= 0x1F9FF:
		return true
	case r >= 0x1FA70 && r <= 0x1FAFF:
		return true
	case r >= 0x20000 && r <= 0x3FFFD:
		return true
	default:
		return false
	}
}

// DisplayWidth returns the visible terminal column width of s, ignoring ANSI
// CSI escape sequences (`\x1b[...m`).
func DisplayWidth(s string) int {
	cols := 0
	inANSI := false
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			inANSI = true
			i += 2
			continue
		}
		if inANSI {
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
				inANSI = false
			}
			i++
			continue
		}
		r, size := decodeRuneInString(s[i:])
		cols += RuneWidth(r)
		i += size
	}
	return cols
}

func decodeRuneInString(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 1
}

// TruncateWidth truncates plain text s so its terminal cell width never exceeds
// maxCols, appending "…" (1 column) when truncated. Safe for any maxCols <= 0
// and for double-width CJK / emoji characters.
func TruncateWidth(s string, maxCols int) string {
	if maxCols <= 0 || s == "" {
		return ""
	}
	if DisplayWidth(s) <= maxCols {
		return s
	}
	if maxCols == 1 {
		for _, r := range s {
			if RuneWidth(r) <= 1 {
				return string(r)
			}
			break
		}
		return "…"
	}
	limit := maxCols - 1 // reserve 1 column for "…"
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := RuneWidth(r)
		if used+w > limit {
			break
		}
		b.WriteRune(r)
		used += w
	}
	b.WriteRune('…')
	return b.String()
}

// PadRightWidth truncates s to width columns if needed and pads with trailing
// ASCII spaces so the returned string occupies exactly width terminal columns.
func PadRightWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	truncated := TruncateWidth(s, width)
	pad := width - DisplayWidth(truncated)
	if pad <= 0 {
		return truncated
	}
	return truncated + strings.Repeat(" ", pad)
}

// WrapWidth wraps multi-line plain text so every line fits within maxCols
// terminal columns without splitting UTF-8 runes or overflowing CJK characters.
func WrapWidth(text string, maxCols int) []string {
	if maxCols < 4 {
		maxCols = 4
	}
	rawLines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		expanded := strings.ReplaceAll(raw, "\t", "  ")
		if expanded == "" {
			out = append(out, "")
			continue
		}
		if DisplayWidth(expanded) <= maxCols {
			out = append(out, expanded)
			continue
		}
		var line strings.Builder
		cols := 0
		for _, r := range expanded {
			w := RuneWidth(r)
			if cols+w > maxCols && cols > 0 {
				out = append(out, line.String())
				line.Reset()
				cols = 0
			}
			line.WriteRune(r)
			cols += w
		}
		if line.Len() > 0 {
			out = append(out, line.String())
		}
	}
	return out
}
