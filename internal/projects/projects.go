package projects

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Andrevops/claude-stats/internal/config"
)

// ExtractProject returns a human-readable project name from a session file path.
// Claude Code encodes absolute paths in folder names by replacing separators with '-'.
func ExtractProject(path string) string {
	// Get the path relative to the projects dir
	rel, err := filepath.Rel(config.ProjectsDir, path)
	if err != nil {
		return "unknown"
	}
	// Use forward slashes for consistency
	rel = filepath.ToSlash(rel)
	parts := strings.SplitN(rel, "/", 2)
	folder := parts[0]

	home, _ := os.UserHomeDir()
	homeEncoded := encodeHome(home)

	if strings.HasPrefix(folder, homeEncoded+"-") {
		encoded := folder[len(homeEncoded)+1:]
		if encoded == "" {
			return "unknown"
		}
		return decodePath(home, encoded)
	}
	if strings.HasPrefix(folder, homeEncoded) {
		encoded := strings.TrimLeft(folder[len(homeEncoded):], "-")
		if encoded == "" {
			return "unknown"
		}
		return decodePath(home, encoded)
	}
	if folder == "" {
		return "unknown"
	}
	return folder
}

// decodePath reconstructs a human-readable path from a Claude-encoded string
// (where \, /, :, and . were all replaced with -) by checking the real filesystem.
// Falls back to the raw encoded string if the path can't be resolved.
func decodePath(base, encoded string) string {
	tokens := strings.Split(encoded, "-")
	var parts []string
	cur := base
	i := 0
	for i < len(tokens) {
		matched := false
		// Try longest match first (greedy from right)
		for j := len(tokens); j > i; j-- {
			candidate := strings.Join(tokens[i:j], "-")
			if candidate == "" {
				continue
			}
			// Try as-is, then with leading dash replaced by dot (e.g. -claude → .claude)
			for _, name := range candidates(candidate) {
				if info, err := os.Stat(filepath.Join(cur, name)); err == nil && info.IsDir() {
					parts = append(parts, name)
					cur = filepath.Join(cur, name)
					i = j
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			// Can't resolve further — return what we have plus the remainder
			remainder := strings.Join(tokens[i:], "-")
			parts = append(parts, remainder)
			break
		}
	}
	return strings.Join(parts, "/")
}

// candidates returns the name variants to try when decoding a path segment.
// A leading dash may represent a dot (e.g. "-claude" → ".claude").
func candidates(name string) []string {
	if strings.HasPrefix(name, "-") {
		return []string{name, "." + name[1:]}
	}
	return []string{name}
}

// encodeHome converts a home directory path to its Claude Code encoded form.
// Claude Code replaces \, /, :, and . with dashes.
func encodeHome(home string) string {
	r := strings.ReplaceAll(home, `\`, "-")
	r = strings.ReplaceAll(r, "/", "-")
	r = strings.ReplaceAll(r, ":", "-")
	r = strings.ReplaceAll(r, ".", "-")
	return r
}

// ShortenPath replaces the home directory prefix with ~.
func ShortenPath(fp string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(fp, home) {
		return "~" + fp[len(home):]
	}
	return fp
}

// GetExt returns the file extension including the dot (e.g., ".py", ".ts").
func GetExt(fp string) string {
	// Use forward-slash split for consistency
	clean := filepath.ToSlash(fp)
	base := clean
	if idx := strings.LastIndex(clean, "/"); idx >= 0 {
		base = clean[idx+1:]
	}
	if idx := strings.LastIndex(base, "."); idx > 0 {
		return "." + base[idx+1:]
	}
	return "(none)"
}
