package format

import (
	"fmt"
	"math"
	"strings"
)

// Fmt formats an integer with comma separators.
func Fmt(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		s = s[1:]
		return "-" + insertCommas(s)
	}
	return insertCommas(s)
}

func insertCommas(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// Pct formats n/total as a percentage string.
func Pct(n, total int) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", float64(n)/float64(total)*100)
}

// Bar renders a colored progress bar with a teal→blue gradient.
func Bar(n, maxVal float64, width int) string {
	if maxVal == 0 {
		return Dim + strings.Repeat("░", width) + Reset
	}
	ratio := n / maxVal
	if ratio > 1 {
		ratio = 1
	}
	filled := int(math.Round(ratio * float64(width)))
	empty := width - filled

	if filled == 0 {
		return Dim + strings.Repeat("░", width) + Reset
	}

	var b strings.Builder
	prevColor := ""
	for i := 0; i < filled; i++ {
		t := float64(i) / float64(maxInt(width-1, 1))
		c := gradientColor(t)
		if c != prevColor {
			b.WriteString(c)
			prevColor = c
		}
		b.WriteString("█")
	}
	b.WriteString(Reset)
	if empty > 0 {
		b.WriteString(Dim)
		b.WriteString(strings.Repeat("░", empty))
		b.WriteString(Reset)
	}
	return b.String()
}

// Header prints a single-line, rule-underlined section header.
// The `char` parameter selects the fill style — "═" for top-level titles,
// "─" for subsections. Width pads the line to a consistent 70 columns.
func Header(text, char string) {
	const w = 70
	// visibleLen ignores ANSI escapes; text here is plain.
	visible := len([]rune(text))
	// Account for `── ` prefix (3 runes) and a trailing space before the fill.
	pad := w - visible - 5
	if pad < 3 {
		pad = 3
	}
	fill := strings.Repeat(char, pad)
	fmt.Printf("\n  %s%s %s%s%s %s%s%s\n",
		Dim, strings.Repeat(char, 2), Reset,
		Bold, text, Reset,
		Dim, fill+Reset)
}

// FmtTokens formats token counts as human-readable (e.g., 1.2M, 45K).
func FmtTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// FmtDuration formats seconds as a compact duration string.
func FmtDuration(secs float64) string {
	if secs < 3600 {
		return fmt.Sprintf("%.0fm", secs/60)
	}
	return fmt.Sprintf("%.1fh", secs/3600)
}

// FriendlyModel shortens a Claude model ID for display.
func FriendlyModel(model string) string {
	r := model
	r = strings.ReplaceAll(r, "claude-", "")
	r = strings.ReplaceAll(r, "-20251001", "")
	r = strings.ReplaceAll(r, "-20251101", "")
	r = strings.ReplaceAll(r, "-20250929", "")
	return r
}
