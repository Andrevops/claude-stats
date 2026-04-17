package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Andrevops/claude-stats/internal/config"
	"github.com/Andrevops/claude-stats/internal/dates"
	"github.com/Andrevops/claude-stats/internal/format"
	"github.com/Andrevops/claude-stats/internal/projects"
	"github.com/Andrevops/claude-stats/internal/session"
)

// fnMatch implements Python's fnmatch.fnmatch: * matches everything including
// spaces and path separators, ** is the same as *, ? matches one char.
// This differs from filepath.Match where * does not match separators.
func fnMatch(pattern, name string) bool {
	return fnMatchImpl(pattern, name)
}

func fnMatchImpl(pattern, name string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			// Consume consecutive *
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true // trailing * matches everything
			}
			// Try matching rest of pattern at every position
			for i := 0; i <= len(name); i++ {
				if fnMatchImpl(pattern, name[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(name) == 0 {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
		default:
			if len(name) == 0 || pattern[0] != name[0] {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
		}
	}
	return len(name) == 0
}

type allowPatterns struct {
	bash, edit, write, read []string
	raw                     []string
}

func loadAllowPatterns() allowPatterns {
	var ap allowPatterns
	data, err := os.ReadFile(config.SettingsFile)
	if err != nil {
		return ap
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return ap
	}
	perms, _ := settings["permissions"].(map[string]interface{})
	allow, _ := perms["allow"].([]interface{})
	for _, v := range allow {
		p, _ := v.(string)
		ap.raw = append(ap.raw, p)
		switch {
		case strings.HasPrefix(p, "Bash(") && strings.HasSuffix(p, ")"):
			ap.bash = append(ap.bash, p[5:len(p)-1])
		case strings.HasPrefix(p, "Edit(") && strings.HasSuffix(p, ")"):
			ap.edit = append(ap.edit, p[5:len(p)-1])
		case strings.HasPrefix(p, "Write(") && strings.HasSuffix(p, ")"):
			ap.write = append(ap.write, p[6:len(p)-1])
		case strings.HasPrefix(p, "Read(") && strings.HasSuffix(p, ")"):
			ap.read = append(ap.read, p[5:len(p)-1])
		case p == "Read":
			ap.read = append(ap.read, "**")
		}
	}
	return ap
}

func isAllowed(name string, inp json.RawMessage, ap allowPatterns) bool {
	if config.AutoAllowedTools[name] {
		return true
	}
	switch name {
	case "Read":
		if len(ap.read) == 0 {
			return false
		}
		ri := session.ParseReadInput(inp)
		for _, p := range ap.read {
			if fnMatch(p, ri.FilePath) {
				return true
			}
		}
		return false
	case "Bash":
		bi := session.ParseBashInput(inp)
		for _, p := range ap.bash {
			if fnMatch(p, bi.Command) {
				return true
			}
		}
		return false
	case "Edit":
		ei := session.ParseEditInput(inp)
		for _, p := range ap.edit {
			if fnMatch(p, ei.FilePath) {
				return true
			}
		}
		return false
	case "Write":
		wi := session.ParseWriteInput(inp)
		for _, p := range ap.write {
			if fnMatch(p, wi.FilePath) {
				return true
			}
		}
		return false
	}
	return false
}

func suggestPattern(name string, inp json.RawMessage) string {
	switch name {
	case "Bash":
		bi := session.ParseBashInput(inp)
		base := bashBase(bi.Command)
		if config.DestructiveCmds[base] {
			return ""
		}
		switch base {
		case "chmod":
			return "Bash(chmod +x *)"
		case "mkdir":
			return "Bash(mkdir *)"
		case "jq":
			return "Bash(jq*)"
		}
		return fmt.Sprintf("Bash(%s *)", base)
	case "Edit":
		ei := session.ParseEditInput(inp)
		return suggestPathPattern("Edit", ei.FilePath)
	case "Write":
		wi := session.ParseWriteInput(inp)
		return suggestPathPattern("Write", wi.FilePath)
	}
	return ""
}

func suggestPathPattern(tool, fp string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(fp, home) {
		rel := fp[len(home)+1:]
		parts := strings.Split(strings.ReplaceAll(rel, "\\", "/"), "/")
		if len(parts) >= 2 {
			return fmt.Sprintf("%s(%s/%s/**)", tool, home, strings.Join(parts[:2], "/"))
		}
		return fmt.Sprintf("%s(%s/**)", tool, home)
	}
	if strings.HasPrefix(fp, "/tmp/") {
		return fmt.Sprintf("%s(/tmp/**)", tool)
	}
	return ""
}

func Prompts(args []string) {
	detailed := false
	filtered := []string{}
	for _, a := range args {
		if a == "--detailed" || a == "-d" {
			detailed = true
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered
	targetDates, label := dates.ParseArgs(args)
	if label == "" {
		return
	}
	ap := loadAllowPatterns()
	files := session.Find(targetDates, false)
	if len(files) == 0 {
		fmt.Printf("\n  No sessions found for %s\n", label)
		return
	}

	totalAuto, totalPrompted := 0, 0
	promptedByTool := map[string]int{}
	autoByTool := map[string]int{}
	bashPrompted := map[string]int{}
	bashAuto := map[string]int{}
	bashSamples := map[string][]string{}
	editPrompted := map[string]int{}
	writePrompted := map[string]int{}
	projPrompted := map[string]int{}
	projAuto := map[string]int{}
	suggestions := map[string]int{}

	for _, f := range files {
		projName := projects.ExtractProject(f)
		session.ScanLines(f, func(line session.LogLine) {
			if line.Type != "assistant" {
				return
			}
			msg, ok := session.ParseAssistantMsg(line.Message)
			if !ok {
				return
			}
			for _, b := range msg.Content {
				if b.Type != "tool_use" {
					continue
				}
				name := b.Name
				if isAllowed(name, b.Input, ap) {
					totalAuto++
					autoByTool[name]++
					projAuto[projName]++
					if name == "Bash" {
						bi := session.ParseBashInput(b.Input)
						bashAuto[bashBase(bi.Command)]++
					}
				} else {
					totalPrompted++
					promptedByTool[name]++
					projPrompted[projName]++

					switch name {
					case "Bash":
						bi := session.ParseBashInput(b.Input)
						base := bashBase(bi.Command)
						bashPrompted[base]++
						if len(bashSamples[base]) < 1 {
							bashSamples[base] = append(bashSamples[base], bashPreview(bi.Command, 55))
						}
					case "Edit":
						ei := session.ParseEditInput(b.Input)
						fp := projects.ShortenPath(ei.FilePath)
						parts := strings.Split(strings.ReplaceAll(fp, "\\", "/"), "/")
						key := fp
						if len(parts) >= 2 {
							key = strings.Join(parts[:2], "/") + "/**"
						}
						editPrompted[key]++
					case "Write":
						wi := session.ParseWriteInput(b.Input)
						fp := projects.ShortenPath(wi.FilePath)
						parts := strings.Split(strings.ReplaceAll(fp, "\\", "/"), "/")
						key := fp
						if len(parts) >= 2 {
							key = strings.Join(parts[:2], "/") + "/**"
						}
						writePrompted[key]++
					}

					sug := suggestPattern(name, b.Input)
					if sug != "" {
						suggestions[sug]++
					}
				}
			}
		})
	}

	total := totalAuto + totalPrompted
	if total == 0 {
		fmt.Printf("\n  No tool calls found for %s\n", label)
		return
	}

	format.Header(fmt.Sprintf("🔐  PERMISSION PROMPTS — %s", label), "═")
	autoPct := float64(totalAuto) / float64(total) * 100
	promptPct := float64(totalPrompted) / float64(total) * 100
	fmt.Printf("\n  Total %s calls · Auto %s (%.0f%%) · Prompted %s (%.0f%%)\n",
		format.Fmt(total),
		format.Fmt(totalAuto), autoPct,
		format.Fmt(totalPrompted), promptPct)
	fmt.Printf("  Auto      %s\n", format.Bar(float64(totalAuto), float64(total), 40))
	fmt.Printf("  Prompted  %s\n", format.Bar(float64(totalPrompted), float64(total), 40))

	if totalPrompted == 0 {
		fmt.Println("\n  🎉 Zero prompts! Your allowlist is perfectly tuned.")
		return
	}

	// ── By Tool
	format.Header("🔧  PROMPTS BY TOOL", "─")
	type toolE struct {
		name     string
		prompted int
	}
	var toolList []toolE
	for name, p := range promptedByTool {
		toolList = append(toolList, toolE{name, p})
	}
	sort.Slice(toolList, func(i, j int) bool { return toolList[i].prompted > toolList[j].prompted })
	maxTool := 0
	if len(toolList) > 0 {
		maxTool = toolList[0].prompted
	}

	fmt.Printf("\n  %-22s %8s %8s %7s  %s\n", "Tool", "Prompted", "Auto", "Rate", "")
	fmt.Printf("  %s %s %8s %s  %s\n",
		repeat("─", 22), repeat("─", 8), repeat("─", 8), repeat("─", 7), repeat("─", 15))
	for _, e := range toolList {
		a := autoByTool[e.name]
		fmt.Printf("  %-22s %8d %8d %s  %s\n",
			e.name, e.prompted, a,
			format.Pct(e.prompted, e.prompted+a),
			format.Bar(float64(e.prompted), float64(maxTool), 15))
	}

	// ── Bash Ranking
	if len(bashPrompted) > 0 {
		format.Header("🐚  BASH COMMANDS REQUIRING PROMPTS", "─")
		type bashE struct {
			cmd   string
			count int
		}
		var bashList []bashE
		for cmd, c := range bashPrompted {
			bashList = append(bashList, bashE{cmd, c})
		}
		sort.Slice(bashList, func(i, j int) bool { return bashList[i].count > bashList[j].count })
		if len(bashList) > 15 {
			bashList = bashList[:15]
		}
		maxBash := bashList[0].count

		fmt.Printf("\n  %-12s %5s %5s %6s  %s  Sample\n", "Cmd", "#", "Auto", "Rate", "")
		fmt.Printf("  %s %s %s %s  %s  %s\n",
			repeat("─", 12), repeat("─", 5), repeat("─", 5), repeat("─", 6), repeat("─", 12), repeat("─", 30))
		for _, e := range bashList {
			a := bashAuto[e.cmd]
			sample := ""
			if s, ok := bashSamples[e.cmd]; len(s) > 0 && ok {
				sample = s[0]
				if len(sample) > 38 {
					sample = sample[:35] + "..."
				}
			}
			fmt.Printf("  %-12s %5d %5d %6s  %s  %s\n",
				e.cmd, e.count, a,
				format.Pct(e.count, e.count+a),
				format.Bar(float64(e.count), float64(maxBash), 12),
				sample)
		}
	}

	// ── Edit/Write Paths (detailed only — nuclear option below covers the signal)
	if detailed && len(editPrompted)+len(writePrompted) > 0 {
		format.Header("✏️  EDIT/WRITE PATHS REQUIRING PROMPTS", "─")
		combined := map[string][2]int{} // [edit, write]
		for p, c := range editPrompted {
			v := combined[p]
			v[0] = c
			combined[p] = v
		}
		for p, c := range writePrompted {
			v := combined[p]
			v[1] = c
			combined[p] = v
		}
		type pathE struct {
			path  string
			edit  int
			write int
		}
		var pathList []pathE
		for p, v := range combined {
			pathList = append(pathList, pathE{p, v[0], v[1]})
		}
		sort.Slice(pathList, func(i, j int) bool {
			ti := pathList[i].edit + pathList[i].write
			tj := pathList[j].edit + pathList[j].write
			return ti > tj
		})
		if len(pathList) > 10 {
			pathList = pathList[:10]
		}
		maxPath := 0
		if len(pathList) > 0 {
			maxPath = pathList[0].edit + pathList[0].write
		}

		fmt.Printf("\n  %-35s %5s %6s %6s  %s\n", "Path", "Edit", "Write", "Total", "")
		fmt.Printf("  %s %s %s %s  %s\n",
			repeat("─", 35), repeat("─", 5), repeat("─", 6), repeat("─", 6), repeat("─", 12))
		for _, e := range pathList {
			t := e.edit + e.write
			fmt.Printf("  %-35s %5d %6d %6d  %s\n",
				truncate(e.path, 33), e.edit, e.write, t,
				format.Bar(float64(t), float64(maxPath), 12))
		}
	}

	// ── By Project (detailed only)
	if detailed && len(projPrompted) > 1 {
		format.Header("📁  PROMPTS BY PROJECT", "─")
		type projE struct {
			name   string
			count  int
		}
		var projList []projE
		for name, c := range projPrompted {
			projList = append(projList, projE{name, c})
		}
		sort.Slice(projList, func(i, j int) bool { return projList[i].count > projList[j].count })
		maxProj := projList[0].count

		fmt.Printf("\n  %-42s %7s %7s  %s\n", "Project", "Prompts", "Rate", "")
		fmt.Printf("  %s %s %s  %s\n",
			repeat("─", 42), repeat("─", 7), repeat("─", 7), repeat("─", 12))
		for _, e := range projList {
			a := projAuto[e.name]
			fmt.Printf("  %-42s %7d %s  %s\n",
				truncate(e.name, 40), e.count,
				format.Pct(e.count, e.count+a),
				format.Bar(float64(e.count), float64(maxProj), 12))
		}
	}

	// ── Suggestions
	format.Header("💡  SUGGESTED ALLOW PATTERNS", "─")
	bashSugs := map[string]int{}
	editSugs := map[string]int{}
	writeSugs := map[string]int{}
	for k, v := range suggestions {
		if v < 3 {
			continue
		}
		switch {
		case strings.HasPrefix(k, "Bash("):
			bashSugs[k] = v
		case strings.HasPrefix(k, "Edit("):
			editSugs[k] = v
		case strings.HasPrefix(k, "Write("):
			writeSugs[k] = v
		}
	}

	if len(bashSugs) > 0 {
		fmt.Println("\n  Bash commands:")
		fmt.Printf("  %-40s %6s\n", "Pattern", "Saves")
		fmt.Printf("  %s %s\n", repeat("─", 40), repeat("─", 6))
		type sugE struct {
			pat   string
			count int
		}
		var sugList []sugE
		for p, c := range bashSugs {
			already := false
			for _, existing := range ap.bash {
				if fmt.Sprintf("Bash(%s)", existing) == p {
					already = true
					break
				}
			}
			if !already {
				sugList = append(sugList, sugE{p, c})
			}
		}
		sort.Slice(sugList, func(i, j int) bool { return sugList[i].count > sugList[j].count })
		for _, e := range sugList {
			fmt.Printf("  %-40s ~%d\n", fmt.Sprintf("%q", e.pat), e.count)
		}
	}

	allPathSugs := map[string]int{}
	for k, v := range editSugs {
		allPathSugs[k] += v
	}
	for k, v := range writeSugs {
		allPathSugs[k] += v
	}
	if detailed && len(allPathSugs) > 0 {
		fmt.Println("\n  Edit/Write paths:")
		fmt.Printf("  %-58s %6s\n", "Pattern", "Saves")
		fmt.Printf("  %s %s\n", repeat("─", 58), repeat("─", 6))
		type sugE struct {
			pat   string
			count int
		}
		var sugList []sugE
		for p, c := range allPathSugs {
			if c >= 3 {
				sugList = append(sugList, sugE{p, c})
			}
		}
		sort.Slice(sugList, func(i, j int) bool { return sugList[i].count > sugList[j].count })
		for _, e := range sugList {
			fmt.Printf("  %-58s ~%d\n", fmt.Sprintf("%q", e.pat), e.count)
		}
	}

	totalEditWrite := 0
	for _, c := range editPrompted {
		totalEditWrite += c
	}
	for _, c := range writePrompted {
		totalEditWrite += c
	}
	if totalEditWrite > 20 {
		home, _ := os.UserHomeDir()
		fmt.Printf("\n  💣 Nuclear option (saves ~%d prompts):\n", totalEditWrite)
		fmt.Printf("     \"Edit(%s/**)\"\n", home)
		fmt.Printf("     \"Write(%s/**)\"\n", home)
	}

	// ── Impact
	format.Header("📊  IMPACT SUMMARY", "─")
	bashSaveable := 0
	for _, c := range bashSugs {
		bashSaveable += c
	}
	totalSaveable := bashSaveable + totalEditWrite
	fmt.Printf(`
  Bash patterns:    ~%d prompts saved
  Edit/Write nuke:  ~%d prompts saved
  ─────────────────────────────────
  Potential:        ~%d / %d  (%s)
  %s
`,
		bashSaveable, totalEditWrite,
		totalSaveable, totalPrompted, format.Pct(totalSaveable, totalPrompted),
		format.Bar(float64(totalSaveable), float64(totalPrompted), 30),
	)
}
