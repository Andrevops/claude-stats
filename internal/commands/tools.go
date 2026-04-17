package commands

import (
	"fmt"
	"sort"

	"github.com/Andrevops/claude-stats/internal/config"
	"github.com/Andrevops/claude-stats/internal/dates"
	"github.com/Andrevops/claude-stats/internal/format"
	"github.com/Andrevops/claude-stats/internal/projects"
	"github.com/Andrevops/claude-stats/internal/session"
)

type toolStat struct{ calls, errors int }

func classifyTool(name string) string {
	if config.ReadTools[name] {
		return "read"
	}
	if config.WriteTools[name] {
		return "write"
	}
	if config.AgentTools[name] {
		return "agent"
	}
	return "other"
}

func Tools(args []string) {
	targetDates, label := dates.ParseArgs(args)
	if label == "" {
		return
	}
	files := session.Find(targetDates, false)
	if len(files) == 0 {
		fmt.Printf("\n  No sessions found for %s\n", label)
		return
	}

	tools := map[string]*toolStat{}
	bashCmds := map[string]*toolStat{}
	bashPreviews := map[string][]string{}
	projTools := map[string]map[string]*toolStat{}
	toolPairs := map[[2]string]int{}
	totalCalls, totalErrors := 0, 0
	sessionCount := 0
	workflow := map[string]int{"read": 0, "write": 0, "agent": 0, "other": 0}

	for _, f := range files {
		projName := projects.ExtractProject(f)
		if _, ok := projTools[projName]; !ok {
			projTools[projName] = map[string]*toolStat{}
		}

		// Track call ordering within this session
		type callEntry struct {
			name    string
			isError bool
		}
		var callOrder []callEntry
		toolResults := map[string]bool{} // tid -> isError

		session.ScanLines(f, func(line session.LogLine) {
			if line.Type == "assistant" {
				msg, ok := session.ParseAssistantMsg(line.Message)
				if !ok {
					return
				}
				for _, b := range msg.Content {
					if b.Type == "tool_use" {
						callOrder = append(callOrder, callEntry{name: b.Name, isError: false})
					}
				}
			} else if line.Type == "user" {
				for _, tr := range session.ParseToolResults(line.Message) {
					toolResults[tr.ToolUseID] = tr.IsError
				}
			}
		})

		// Re-scan to pair tool_use IDs with results
		// (Since we need the IDs, do a second pass with IDs)
		type callWithID struct {
			id, name string
			inp      interface{}
		}
		var orderedCalls []callWithID
		session.ScanLines(f, func(line session.LogLine) {
			if line.Type != "assistant" {
				return
			}
			msg, ok := session.ParseAssistantMsg(line.Message)
			if !ok {
				return
			}
			for _, b := range msg.Content {
				if b.Type == "tool_use" {
					var inp interface{}
					if b.Name == "Bash" {
						inp = session.ParseBashInput(b.Input)
					}
					orderedCalls = append(orderedCalls, callWithID{b.ID, b.Name, inp})
				}
			}
		})
		_ = callOrder

		if len(orderedCalls) == 0 {
			continue
		}
		sessionCount++

		prevName := ""
		for _, c := range orderedCalls {
			name := c.name
			isError := toolResults[c.id]

			if _, ok := tools[name]; !ok {
				tools[name] = &toolStat{}
			}
			tools[name].calls++
			totalCalls++
			if isError {
				tools[name].errors++
				totalErrors++
			}

			pt := projTools[projName]
			if _, ok := pt[name]; !ok {
				pt[name] = &toolStat{}
			}
			pt[name].calls++
			if isError {
				pt[name].errors++
			}

			workflow[classifyTool(name)]++

			if name == "Bash" {
				if bi, ok := c.inp.(session.BashInput); ok {
					base := bashBase(bi.Command)
					if _, ok := bashCmds[base]; !ok {
						bashCmds[base] = &toolStat{}
					}
					bashCmds[base].calls++
					if isError {
						bashCmds[base].errors++
					}
					if len(bashPreviews[base]) < 2 {
						bashPreviews[base] = append(bashPreviews[base], bashPreview(bi.Command, 65))
					}
				}
			}

			if prevName != "" {
				toolPairs[[2]string{prevName, name}]++
			}
			prevName = name
		}
	}

	if totalCalls == 0 {
		fmt.Printf("\n  No tool calls found for %s\n", label)
		return
	}

	format.Header(fmt.Sprintf("🔧  TOOL ANALYTICS — %s", label), "═")
	fmt.Printf("\n  %-10s%-14d %-10s%s (avg %d/session)\n",
		"Sessions", sessionCount, "Calls", format.Fmt(totalCalls), totalCalls/sessionCount)
	fmt.Printf("  %-10s%s (%s)\n",
		"Errors", format.Fmt(totalErrors), format.Pct(totalErrors, totalCalls))

	// ── Workflow Balance
	format.Header("⚖️  WORKFLOW BALANCE", "─")
	icons := map[string]string{"read": "📖", "write": "✏️ ", "agent": "🤖", "other": "❓"}
	for _, cat := range []string{"read", "write", "agent", "other"} {
		n := workflow[cat]
		if n == 0 {
			continue
		}
		fmt.Printf("  %s %s%-8s %s %s  (%s)\n",
			icons[cat], "", cat,
			format.Bar(float64(n), float64(totalCalls), 25),
			format.Fmt(n),
			format.Pct(n, totalCalls))
	}

	// ── Tool Ranking
	format.Header("📊  TOOL RANKING", "─")
	type toolEntry struct {
		name string
		s    *toolStat
	}
	var toolList []toolEntry
	for name, s := range tools {
		toolList = append(toolList, toolEntry{name, s})
	}
	sort.Slice(toolList, func(i, j int) bool { return toolList[i].s.calls > toolList[j].s.calls })

	maxCalls := 0
	if len(toolList) > 0 {
		maxCalls = toolList[0].s.calls
	}

	fmt.Printf("\n  %-22s %6s %5s %6s  %s\n", "Tool", "Calls", "Err", "Rate", "")
	fmt.Printf("  %s %s %s %s  %s\n",
		repeat("─", 22), repeat("─", 6), repeat("─", 5), repeat("─", 6), repeat("─", 20))

	for _, e := range toolList {
		if e.s.calls < 2 && totalCalls > 100 {
			continue
		}
		errRate := "—"
		pct := 0
		if e.s.errors > 0 {
			pct = e.s.errors * 100 / e.s.calls
			errRate = fmt.Sprintf("%d%%", pct)
		}
		flag := ""
		if pct >= 10 {
			flag = " ⚠️"
		}
		fmt.Printf("  %-22s %6d %5d %6s  %s%s\n",
			e.name, e.s.calls, e.s.errors, errRate,
			format.Bar(float64(e.s.calls), float64(maxCalls), 20), flag)
	}

	// ── Bash Subcommands
	if len(bashCmds) > 0 {
		format.Header("🐚  BASH SUBCOMMANDS (top 15)", "─")
		type bashEntry struct {
			cmd string
			s   *toolStat
		}
		var bashList []bashEntry
		for cmd, s := range bashCmds {
			bashList = append(bashList, bashEntry{cmd, s})
		}
		sort.Slice(bashList, func(i, j int) bool { return bashList[i].s.calls > bashList[j].s.calls })
		if len(bashList) > 15 {
			bashList = bashList[:15]
		}
		maxBash := bashList[0].s.calls

		fmt.Printf("\n  %-14s %6s %5s  %s  Sample\n", "Cmd", "Calls", "Err", "")
		fmt.Printf("  %s %s %s  %s  %s\n",
			repeat("─", 14), repeat("─", 6), repeat("─", 5), repeat("─", 15), repeat("─", 30))

		for _, e := range bashList {
			flag := ""
			if e.s.errors > 0 {
				flag = " ⚠️"
			}
			sample := ""
			if previews, ok := bashPreviews[e.cmd]; len(previews) > 0 && ok {
				sample = previews[0]
				if len(sample) > 40 {
					sample = sample[:37] + "..."
				}
			}
			fmt.Printf("  %-14s %6d %5d  %s%s  %s\n",
				e.cmd, e.s.calls, e.s.errors,
				format.Bar(float64(e.s.calls), float64(maxBash), 15), flag, sample)
		}
	}

	// ── Tool Chains
	if len(toolPairs) > 0 {
		format.Header("🔗  COMMON TOOL CHAINS (top 10)", "─")
		type pairEntry struct {
			a, b  string
			count int
		}
		var pairList []pairEntry
		for p, c := range toolPairs {
			pairList = append(pairList, pairEntry{p[0], p[1], c})
		}
		sort.Slice(pairList, func(i, j int) bool { return pairList[i].count > pairList[j].count })
		if len(pairList) > 10 {
			pairList = pairList[:10]
		}
		maxPair := pairList[0].count

		fmt.Println()
		for _, e := range pairList {
			fmt.Printf("  %-14s → %-14s %5dx  %s\n",
				e.a, e.b, e.count,
				format.Bar(float64(e.count), float64(maxPair), 15))
		}
	}

	// ── Per-Project
	if len(projTools) > 1 {
		format.Header("📁  BY PROJECT", "─")
		type projEntry struct {
			name  string
			total int
			errs  int
			top3  string
		}
		var projList []projEntry
		for name, pt := range projTools {
			total, errs := 0, 0
			type te struct {
				n string
				c int
			}
			var tools2 []te
			for tn, ts := range pt {
				total += ts.calls
				errs += ts.errors
				tools2 = append(tools2, te{tn, ts.calls})
			}
			sort.Slice(tools2, func(i, j int) bool { return tools2[i].c > tools2[j].c })
			top := ""
			for i, t := range tools2 {
				if i >= 3 {
					break
				}
				if top != "" {
					top += " "
				}
				top += fmt.Sprintf("%s(%d)", t.n, t.c)
			}
			projList = append(projList, projEntry{name, total, errs, top})
		}
		sort.Slice(projList, func(i, j int) bool { return projList[i].total > projList[j].total })

		fmt.Printf("\n  %-42s %6s %4s %s\n", "Project", "Calls", "Err", "Top tools")
		fmt.Printf("  %s %s %s %s\n",
			repeat("─", 42), repeat("─", 6), repeat("─", 4), repeat("─", 28))

		for _, e := range projList {
			errStr := "—"
			if e.errs > 0 {
				errStr = fmt.Sprintf("%d", e.errs)
			}
			name := truncate(e.name, 40)
			fmt.Printf("  %-42s %6d %4s %28s\n",
				name, e.total, errStr, e.top3)
		}
	}

	// ── Insights
	format.Header("💡  INSIGHTS", "─")
	var insights []string

	reads := workflow["read"]
	writes := workflow["write"]
	if reads > 0 && writes > 0 {
		ratio := float64(reads) / float64(writes)
		if ratio > 3 {
			insights = append(insights, fmt.Sprintf("Heavy research mode: %.1f:1 read/write ratio.", ratio))
		} else if ratio < 0.5 {
			insights = append(insights, fmt.Sprintf("Heavy edit mode: %.1f:1 write/read ratio.", 1/ratio))
		} else {
			insights = append(insights, fmt.Sprintf("Balanced workflow: %.1f:1 read/write ratio.", ratio))
		}
	}

	if totalErrors > 0 {
		worst := ""
		worstCount := 0
		for name, s := range tools {
			if s.errors > worstCount {
				worstCount = s.errors
				worst = name
			}
		}
		insights = append(insights, fmt.Sprintf("Error rate %s — worst: %s (%d)",
			format.Pct(totalErrors, totalCalls), worst, worstCount))
	}

	agentCalls := workflow["agent"]
	if agentCalls > 5 {
		insights = append(insights, fmt.Sprintf("Active agent delegation: %d calls.", agentCalls))
	} else if totalCalls > 50 && agentCalls == 0 {
		insights = append(insights, "No subagent usage. Consider Task tool for parallel work.")
	}

	if editTool, ok := tools["Edit"]; ok && editTool.errors > 2 {
		insights = append(insights, fmt.Sprintf("Edit errored %dx — likely non-unique match strings.", editTool.errors))
	}

	if len(insights) == 0 {
		insights = append(insights, "Clean session. No notable patterns.")
	}

	for i, ins := range insights {
		fmt.Printf("  %d. %s\n", i+1, ins)
	}
	fmt.Println()
}
