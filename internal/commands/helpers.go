package commands

import (
	"path/filepath"
	"strings"
)

func repeat(s string, n int) string {
	return strings.Repeat(s, n)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

func slashPath(p string) string {
	return filepath.ToSlash(p)
}

func bashBase(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "(empty)"
	}
	parts := strings.Fields(cmd)
	base := parts[0]
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, `\`); idx >= 0 {
		base = base[idx+1:]
	}
	return base
}

func bashPreview(cmd string, maxLen int) string {
	lines := strings.Split(cmd, "\n")
	var parts []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			parts = append(parts, l)
		}
	}
	joined := strings.Join(parts, " ; ")
	if len(joined) > maxLen-3 {
		return joined[:maxLen-3] + "..."
	}
	return joined
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
