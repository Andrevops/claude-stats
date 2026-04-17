package commands

import (
	"encoding/json"
	"fmt"
	"os"
"sort"
	"strings"
	"time"

	"github.com/Andrevops/claude-stats/internal/config"
	"github.com/Andrevops/claude-stats/internal/dates"
	"github.com/Andrevops/claude-stats/internal/format"
	"github.com/Andrevops/claude-stats/internal/projects"
	"github.com/Andrevops/claude-stats/internal/session"
)

type reportProj struct {
	messages, tools, errors, output int
	written, added, removed         int
	files                           map[string]bool
	sessions                        int
	models                          map[string]*tokenCounts
}

func loadBashPatterns() []string {
	data, err := os.ReadFile(config.SettingsFile)
	if err != nil {
		return nil
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil
	}
	perms, _ := settings["permissions"].(map[string]interface{})
	allow, _ := perms["allow"].([]interface{})
	var patterns []string
	for _, v := range allow {
		p, _ := v.(string)
		if strings.HasPrefix(p, "Bash(") && strings.HasSuffix(p, ")") {
			patterns = append(patterns, p[5:len(p)-1])
		}
	}
	return patterns
}

func isBashAllowed(cmd string, patterns []string) bool {
	for _, p := range patterns {
		if fnMatch(p, cmd) {
			return true
		}
	}
	return false
}

func Report(args []string) {
	outputJSON := false
	filtered := []string{}
	for _, a := range args {
		if a == "--json" {
			outputJSON = true
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered

	targetDates, label := dates.ParseReportArgs(args)
	if label == "" {
		return
	}
	files := session.Find(targetDates, false)
	bashPatterns := loadBashPatterns()

	if len(files) == 0 {
		fmt.Printf("\n  No sessions found for %s\n", label)
		return
	}

	totalMessages, totalTurns, totalToolCalls, totalErrors := 0, 0, 0, 0
	mainSessions, subagentCount := 0, 0
	totalOutput := 0
	modelsGlobal := map[string]*tokenCounts{}
	totalWritten, totalAdded, totalRemoved := 0, 0, 0
	filesTouched := map[string]bool{}
	toolCounts := map[string]int{}
	toolErrors := map[string]int{}
	bashCmds := map[string]int{}
	proj := map[string]*reportProj{}
	dailyMessages := map[string]int{}
	dailyOutput := map[string]int{}
	hourly := map[int]int{}
	promptedCmds := map[string]int{}
	totalPrompted, totalAuto := 0, 0
	type rptSession struct {
		project   string
		start     time.Time
		duration  float64
		messages  int
		ctxGrowth float64
	}
	var sessionData []rptSession

	for _, f := range files {
		projName := projects.ExtractProject(f)
		isSub := containsSubagentsPath(f)
		if isSub {
			subagentCount++
		} else {
			mainSessions++
		}
		if _, ok := proj[projName]; !ok {
			proj[projName] = &reportProj{
				files:  map[string]bool{},
				models: map[string]*tokenCounts{},
			}
		}
		p := proj[projName]
		if !isSub {
			p.sessions++
		}

		var sessFirstTS, sessLastTS time.Time
		var sessPrevTS time.Time
		sessActiveSecs := 0.0
		sessMsgs := 0
		var sessCtx []float64
		pendingTools := map[string]string{}

		session.ScanLines(f, func(line session.LogLine) {
			localDT, ok := dates.ParseTS(line.Timestamp)
			msgType := line.Type

			if session.IsConversation(line) {
				totalMessages++
				sessMsgs++
				p.messages++
				if ok {
					dailyMessages[localDT.Format("2006-01-02")]++
					hourly[localDT.Hour()]++
					if sessFirstTS.IsZero() {
						sessFirstTS = localDT
					}
					if !sessPrevTS.IsZero() {
						gap := localDT.Sub(sessPrevTS).Seconds()
						if gap > 0 && gap < 900 {
							sessActiveSecs += gap
						}
					}
					sessPrevTS = localDT
					sessLastTS = localDT
				}
			}

			if session.IsUserTurn(line) {
				totalTurns++
			}
			if msgType == "user" {
				for _, tr := range session.ParseToolResults(line.Message) {
					if tr.IsError {
						tname := pendingTools[tr.ToolUseID]
						totalErrors++
						toolErrors[tname]++
						p.errors++
					}
				}
			}
			if msgType == "assistant" {
				msg, parsed := session.ParseAssistantMsg(line.Message)
				if !parsed {
					return
				}
				model := msg.Model
				if msg.Usage != nil {
					if _, ok2 := modelsGlobal[model]; !ok2 {
						modelsGlobal[model] = &tokenCounts{}
					}
					modelsGlobal[model].add(msg.Usage)
					if _, ok2 := p.models[model]; !ok2 {
						p.models[model] = &tokenCounts{}
					}
					p.models[model].add(msg.Usage)
					out := msg.Usage.OutputTokens
					totalOutput += out
					p.output += out
					cr := msg.Usage.CacheReadInputTokens + msg.Usage.CacheCreationInputTokens
					sessCtx = append(sessCtx, float64(cr))
					if ok {
						dailyOutput[localDT.Format("2006-01-02")] += out
					}
				}
				for _, b := range msg.Content {
					if b.Type != "tool_use" {
						continue
					}
					name := b.Name
					tid := b.ID
					totalToolCalls++
					toolCounts[name]++
					p.tools++
					pendingTools[tid] = name

					switch name {
					case "Bash":
						bi := session.ParseBashInput(b.Input)
						cmd := strings.TrimSpace(bi.Command)
						base := bashBase(cmd)
						bashCmds[base]++
						if !isBashAllowed(cmd, bashPatterns) {
							promptedCmds[fmt.Sprintf("Bash(%s)", base)]++
							totalPrompted++
						} else {
							totalAuto++
						}
					case "Edit", "Write":
						totalPrompted++
					default:
						if config.AutoAllowedTools[name] || name == "Read" {
							totalAuto++
						} else {
							totalPrompted++
						}
					}

					switch name {
					case "Write":
						wi := session.ParseWriteInput(b.Input)
						lines := session.CountLines(wi.Content)
						totalWritten += lines
						p.written += lines
						if wi.FilePath != "" {
							filesTouched[wi.FilePath] = true
							p.files[wi.FilePath] = true
						}
					case "Edit":
						ei := session.ParseEditInput(b.Input)
						newL := session.CountLines(ei.NewString)
						oldL := session.CountLines(ei.OldString)
						totalAdded += newL
						totalRemoved += oldL
						p.added += newL
						p.removed += oldL
						if ei.FilePath != "" {
							filesTouched[ei.FilePath] = true
							p.files[ei.FilePath] = true
						}
					}
				}
			}
		})

		if !sessFirstTS.IsZero() && !sessLastTS.IsZero() && sessMsgs >= 4 && !isSub {
			duration := sessActiveSecs
			ctxGrowth := 0.0
			if len(sessCtx) >= 4 {
				q := len(sessCtx) / 4
				firstQ := avg(sessCtx[:q])
				lastQ := avg(sessCtx[len(sessCtx)-q:])
				if firstQ > 0 {
					ctxGrowth = (lastQ/firstQ - 1) * 100
				}
			}
			sessionData = append(sessionData, rptSession{
				project: projName, start: sessFirstTS,
				duration: duration, messages: sessMsgs, ctxGrowth: ctxGrowth,
			})
		}
	}

	netLines := totalWritten + totalAdded - totalRemoved
	totalCost := 0.0
	for m, tc := range modelsGlobal {
		totalCost += calcCost(*tc, m)
	}
	errRate := errorRate(totalErrors, totalToolCalls)
	linesPerTurn := 0.0
	if totalTurns > 0 {
		linesPerTurn = float64(netLines) / float64(totalTurns)
	}

	if outputJSON {
		type rptProjJSON struct {
			Name     string  `json:"name"`
			Sessions int     `json:"sessions"`
			Messages int     `json:"messages"`
			Lines    int     `json:"net_lines"`
			Files    int     `json:"files"`
			ErrPct   float64 `json:"error_pct"`
		}
		type rptToolJSON struct {
			Name   string `json:"name"`
			Calls  int    `json:"calls"`
			Errors int    `json:"errors"`
		}
		var pjs []rptProjJSON
		for name, p := range proj {
			pNet := p.written + p.added - p.removed
			ep := 0.0
			if p.tools > 0 {
				ep = float64(p.errors) / float64(p.tools) * 100
			}
			pjs = append(pjs, rptProjJSON{name, p.sessions, p.messages, pNet, len(p.files), ep})
		}
		sort.Slice(pjs, func(i, j int) bool { return pjs[i].Lines > pjs[j].Lines })

		var tjs []rptToolJSON
		for name, count := range toolCounts {
			tjs = append(tjs, rptToolJSON{name, count, toolErrors[name]})
		}
		sort.Slice(tjs, func(i, j int) bool { return tjs[i].Calls > tjs[j].Calls })

		out := map[string]interface{}{
			"label":          label,
			"sessions":       mainSessions,
			"subagents":      subagentCount,
			"projects_count": len(proj),
			"messages":       totalMessages,
			"turns":          totalTurns,
			"tool_calls":     totalToolCalls,
			"errors":         totalErrors,
			"error_rate":     errRate,
			"files":          len(filesTouched),
			"cost":           totalCost,
			"net_lines":      netLines,
			"lines_written":  totalWritten,
			"lines_added":    totalAdded,
			"lines_removed":  totalRemoved,
			"lines_per_turn": linesPerTurn,
			"projects":       pjs,
			"tools":          tjs,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	format.Header(fmt.Sprintf("📊  CLAUDE CODE — %s", label), "═")
	sessStr := fmt.Sprintf("%d", mainSessions)
	if subagentCount > 0 {
		sessStr = fmt.Sprintf("%d (+%d sub)", mainSessions, subagentCount)
	}
	fmt.Printf("\n  %-14s%-20s %-14s%s\n", "Sessions", sessStr, "Projects", format.Fmt(len(proj)))
	fmt.Printf("  %-14s%-20s %-14s%s\n", "Messages", format.Fmt(totalMessages), "Turns", format.Fmt(totalTurns))
	fmt.Printf("  %-14s%-20s %-14s%s (%.1f%%)\n", "Tool calls", format.Fmt(totalToolCalls), "Errors", format.Fmt(totalErrors), errRate)
	fmt.Printf("  %-14s%-20d %-14s$%.0f\n", "Files", len(filesTouched), "Est. cost", totalCost)
	fmt.Printf("\n  Net lines %s  (+%s new · +%s edits · -%s)  · %.1f/turn\n",
		format.Fmt(netLines),
		format.Fmt(totalWritten), format.Fmt(totalAdded), format.Fmt(totalRemoved),
		linesPerTurn)

	// ── Daily Activity (tight, last 7 days only — heatmap covers full history)
	if len(dailyMessages) > 1 {
		format.Header("📅  DAILY ACTIVITY", "─")
		var sortedDates []string
		for d := range dailyMessages {
			sortedDates = append(sortedDates, d)
		}
		sort.Strings(sortedDates)
		if len(sortedDates) > 7 {
			sortedDates = sortedDates[len(sortedDates)-7:]
		}
		maxDaily := 0
		for _, d := range sortedDates {
			if dailyMessages[d] > maxDaily {
				maxDaily = dailyMessages[d]
			}
		}
		fmt.Println()
		for _, date := range sortedDates {
			msgs := dailyMessages[date]
			out := dailyOutput[date]
			dt, _ := time.Parse("2006-01-02", date)
			day := config.Days[int(dt.Weekday()+6)%7]
			weekend := ""
			if dt.Weekday() == time.Saturday || dt.Weekday() == time.Sunday {
				weekend = " 🏖"
			}
			fmt.Printf("  %s %s  %5d  %6s  %s%s\n",
				date, day, msgs, format.FmtTokens(out),
				format.Bar(float64(msgs), float64(maxDaily), 22), weekend)
		}
	}

	// ── Project Leaderboard
	format.Header("🏆  PROJECT LEADERBOARD", "─")
	type projRankEntry struct {
		name    string
		p       *reportProj
		pNet    int
		lpm     float64
		err     float64
	}
	var projList []projRankEntry
	for name, p := range proj {
		pNet := p.written + p.added - p.removed
		lpm := 0.0
		if p.messages > 0 {
			lpm = float64(pNet) / float64(p.messages)
		}
		er := 0.0
		if p.tools > 0 {
			er = float64(p.errors) / float64(p.tools) * 100
		}
		projList = append(projList, projRankEntry{name, p, pNet, lpm, er})
	}
	sort.Slice(projList, func(i, j int) bool {
		ti := projList[i].p.written + projList[i].p.added
		tj := projList[j].p.written + projList[j].p.added
		return ti > tj
	})

	fmt.Printf("\n  %-36s %4s %5s %7s %6s %5s %5s\n",
		"Project", "Sess", "Msgs", "Lines", "L/Msg", "Err%", "Files")
	fmt.Printf("  %s %s %s %s %s %s %s\n",
		repeat("─", 36), repeat("─", 4), repeat("─", 5), repeat("─", 7),
		repeat("─", 6), repeat("─", 5), repeat("─", 5))
	for _, e := range projList {
		warn := ""
		if e.err > 10 {
			warn = " ⚠️"
		}
		fmt.Printf("  %-36s %4d %5d %7d %6.1f %4.0f%% %5d%s\n",
			truncate(e.name, 34), e.p.sessions, e.p.messages, e.pNet,
			e.lpm, e.err, len(e.p.files), warn)
	}

	// ── Top Tools
	format.Header("🔧  TOP TOOLS", "─")
	type toolEntry struct {
		name  string
		count int
	}
	var toolList []toolEntry
	for name, count := range toolCounts {
		toolList = append(toolList, toolEntry{name, count})
	}
	sort.Slice(toolList, func(i, j int) bool { return toolList[i].count > toolList[j].count })
	if len(toolList) > 10 {
		toolList = toolList[:10]
	}
	maxTool := 0
	if len(toolList) > 0 {
		maxTool = toolList[0].count
	}

	fmt.Printf("\n  %-18s %6s %5s %5s  %s\n", "Tool", "Calls", "Err", "%", "")
	fmt.Printf("  %s %s %s %s  %s\n",
		repeat("─", 18), repeat("─", 6), repeat("─", 5), repeat("─", 5), repeat("─", 15))
	for _, e := range toolList {
		errs := toolErrors[e.name]
		pct := float64(e.count) / float64(totalToolCalls) * 100
		flag := ""
		if errs > 5 {
			flag = " ⚠️"
		}
		fmt.Printf("  %-18s %6d %5d %4.0f%%  %s%s\n",
			e.name, e.count, errs, pct,
			format.Bar(float64(e.count), float64(maxTool), 15), flag)
	}

	// ── Top Bash Commands
	format.Header("🐚  TOP BASH COMMANDS", "─")
	type bashEntry struct {
		cmd   string
		count int
	}
	var bashList []bashEntry
	for cmd, count := range bashCmds {
		bashList = append(bashList, bashEntry{cmd, count})
	}
	sort.Slice(bashList, func(i, j int) bool { return bashList[i].count > bashList[j].count })
	if len(bashList) > 10 {
		bashList = bashList[:10]
	}
	maxBash := 0
	if len(bashList) > 0 {
		maxBash = bashList[0].count
	}

	fmt.Printf("\n  %-14s %6s  %s\n", "Cmd", "Calls", "")
	fmt.Printf("  %s %s  %s\n", repeat("─", 14), repeat("─", 6), repeat("─", 12))
	for _, e := range bashList {
		fmt.Printf("  %-14s %6d  %s\n", e.cmd, e.count,
			format.Bar(float64(e.count), float64(maxBash), 12))
	}

	// ── Health & permissions (combined compact section)
	totalPerm := totalPrompted + totalAuto
	if len(sessionData) > 0 || totalPerm > 0 {
		format.Header("🏥  HEALTH & PERMISSIONS", "─")
		fmt.Println()
		if len(sessionData) > 0 {
			var highGrowth []rptSession
			totalDur := 0.0
			for _, s := range sessionData {
				if s.ctxGrowth > 200 {
					highGrowth = append(highGrowth, s)
				}
				totalDur += s.duration
			}
			avgDur := totalDur / float64(len(sessionData))
			marker := "✅"
			if len(highGrowth) > 0 {
				marker = "⚠️"
			}
			fmt.Printf("  Sessions %d · avg active %s · bloated %d %s\n",
				len(sessionData), format.FmtDuration(avgDur), len(highGrowth), marker)
			if len(highGrowth) > 0 {
				sort.Slice(highGrowth, func(i, j int) bool {
					return highGrowth[i].ctxGrowth > highGrowth[j].ctxGrowth
				})
				for _, s := range highGrowth[:min3(len(highGrowth), 2)] {
					fmt.Printf("    %s +%.0f%% ctx · %s · %s\n",
						s.start.Format("2006-01-02"), s.ctxGrowth,
						format.FmtDuration(s.duration), truncate(s.project, 32))
				}
			}
		}
		if totalPerm > 0 {
			promptPct := float64(totalPrompted) / float64(totalPerm) * 100
			fmt.Printf("  Permission prompts %.0f%% (%s auto · %s prompted)\n",
				promptPct, format.Fmt(totalAuto), format.Fmt(totalPrompted))
			if len(promptedCmds) > 0 && promptPct > 10 {
				type pcEntry struct {
					cmd   string
					count int
				}
				var pcList []pcEntry
				for cmd, c := range promptedCmds {
					pcList = append(pcList, pcEntry{cmd, c})
				}
				sort.Slice(pcList, func(i, j int) bool { return pcList[i].count > pcList[j].count })
				if len(pcList) > 3 {
					pcList = pcList[:3]
				}
				for _, e := range pcList {
					fmt.Printf("    %-32s ~%d\n", e.cmd, e.count)
				}
			}
		}
	}

	// ── Scorecard
	format.Header("📊  SCORECARD", "─")
	scores := map[string]float64{}
	if totalTurns > 0 {
		scores["Productivity"] = min100(linesPerTurn / 5 * 100)
	}
	if totalToolCalls > 0 {
		scores["Reliability"] = max0((1 - errRate/15) * 100)
	} else {
		scores["Reliability"] = 100
	}
	promptRate := 0.0
	if totalPerm > 0 {
		promptRate = float64(totalPrompted) / float64(totalPerm) * 100
	}
	scores["Prompt Efficiency"] = max0((1 - promptRate/30) * 100)

	if len(sessionData) > 0 {
		highGrowthCount := 0
		for _, s := range sessionData {
			if s.ctxGrowth > 200 {
				highGrowthCount++
			}
		}
		highPct := float64(highGrowthCount) / float64(len(sessionData)) * 100
		scores["Session Health"] = max0((1 - highPct/20) * 100)
	} else {
		scores["Session Health"] = 100
	}
	if totalTurns > 0 {
		outPerTurn := float64(totalOutput) / float64(totalTurns)
		scores["Output Density"] = min100(outPerTurn / 100 * 100)
	}

	overall := 0.0
	for _, v := range scores {
		overall += v
	}
	if len(scores) > 0 {
		overall /= float64(len(scores))
	}

	type scoreEntry struct {
		name  string
		score float64
	}
	var scoreList []scoreEntry
	for name, score := range scores {
		scoreList = append(scoreList, scoreEntry{name, score})
	}
	sort.Slice(scoreList, func(i, j int) bool { return scoreList[i].score < scoreList[j].score })

	fmt.Println()
	for _, e := range scoreList {
		grade := "🔴"
		if e.score >= 70 {
			grade = "🟢"
		} else if e.score >= 40 {
			grade = "🟡"
		}
		fmt.Printf("  %s %-18s %3.0f  %s\n",
			grade, e.name, e.score,
			format.BarWith(e.score, 100, 18, format.ScoreColor(e.score)))
	}
	gradeEmoji, gradeText := scoreGrade(overall)
	fmt.Printf("\n  %s  Overall %3.0f/100 · %s  %s\n",
		gradeEmoji, overall, gradeText,
		format.BarWith(overall, 100, 18, format.ScoreColor(overall)))

	// ── Action Items
	format.Header("🎯  ACTION ITEMS", "─")
	var actions []string
	if errRate > 8 {
		actions = append(actions, fmt.Sprintf("🔴 Error rate is %.1f%% — run `claude-stats tools --week` to identify failing tools.", errRate))
	}
	if promptRate > 20 {
		actions = append(actions, fmt.Sprintf("🟡 %.0f%% of tool calls need prompts — run `claude-stats prompts --week` and apply suggestions.", promptRate))
	}
	highGrowthCount := 0
	for _, s := range sessionData {
		if s.ctxGrowth > 200 {
			highGrowthCount++
		}
	}
	if highGrowthCount > 2 {
		actions = append(actions, fmt.Sprintf("🟡 %d sessions had context bloat — restart sessions more frequently.", highGrowthCount))
	}
	if linesPerTurn < 2 && totalTurns > 50 {
		actions = append(actions, fmt.Sprintf("🟡 Low output (%.1f lines/turn) — consider Plan mode for complex tasks.", linesPerTurn))
	}
	for _, e := range projList {
		if e.p.tools > 10 && e.err > 10 {
			actions = append(actions, fmt.Sprintf("🟡 High error rate in %s — check CLAUDE.md and allowlist.", truncate(e.name, 20)))
			break
		}
	}
	if len(actions) == 0 {
		actions = append(actions, "🟢 Everything looks good! No urgent optimizations needed.")
	}
	for i, action := range actions {
		fmt.Printf("  %d. %s\n", i+1, action)
	}
	fmt.Println()
}

func min100(v float64) float64 {
	if v > 100 {
		return 100
	}
	return v
}

func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func min3(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func scoreGrade(score float64) (string, string) {
	switch {
	case score >= 80:
		return "🏆", "Excellent"
	case score >= 60:
		return "👍", "Good"
	case score >= 40:
		return "⚠️", "Needs Work"
	default:
		return "🔧", "Optimize"
	}
}
