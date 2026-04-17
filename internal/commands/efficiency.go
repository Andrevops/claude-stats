package commands

import (
	"fmt"
	"sort"

	"github.com/Andrevops/claude-stats/internal/dates"
	"github.com/Andrevops/claude-stats/internal/format"
	"github.com/Andrevops/claude-stats/internal/projects"
	"github.com/Andrevops/claude-stats/internal/session"
)

type projEfficiency struct {
	turns, tools, errors, outputTokens int
	written, added, removed            int
	files                              map[string]bool
}

func Efficiency(args []string) {
	targetDates, label := dates.ParseArgs(args)
	if label == "" {
		return
	}
	files := session.Find(targetDates, false)
	if len(files) == 0 {
		fmt.Printf("\n  No sessions found for %s\n", label)
		return
	}

	totalHumanTurns, totalAssistantTurns := 0, 0
	totalToolCalls, totalErrors := 0, 0
	totalOutputTokens := 0
	totalWritten, totalAdded, totalRemoved := 0, 0, 0
	filesTouched := map[string]bool{}
	proj := map[string]*projEfficiency{}
	toolTypeCounts := map[string]int{}
	productiveTurns, researchTurns, overheadTurns := 0, 0, 0

	for _, f := range files {
		projName := projects.ExtractProject(f)
		if _, ok := proj[projName]; !ok {
			proj[projName] = &projEfficiency{files: map[string]bool{}}
		}
		p := proj[projName]

		// Track per-turn state
		turnHasWrite, turnHasError := false, false
		inAssistantTurn := false

		session.ScanLines(f, func(line session.LogLine) {
			msgType := line.Type

			// Real user turn: count it and flush the prior assistant turn.
			if session.IsUserTurn(line) {
				if inAssistantTurn {
					switch {
					case turnHasError:
						overheadTurns++
					case turnHasWrite:
						productiveTurns++
					default:
						researchTurns++
					}
				}
				turnHasWrite, turnHasError = false, false
				inAssistantTurn = false

				totalHumanTurns++
				p.turns++
			} else if msgType == "user" {
				// Tool-result envelope — count errors but do not inflate turns.
				for _, tr := range session.ParseToolResults(line.Message) {
					if tr.IsError {
						totalErrors++
						p.errors++
						turnHasError = true
					}
				}
			} else if msgType == "assistant" {
				inAssistantTurn = true
				totalAssistantTurns++
				msg, ok := session.ParseAssistantMsg(line.Message)
				if !ok {
					return
				}
				if msg.Usage != nil {
					out := msg.Usage.OutputTokens
					totalOutputTokens += out
					p.outputTokens += out
				}
				for _, b := range msg.Content {
					if b.Type != "tool_use" {
						continue
					}
					name := b.Name
					totalToolCalls++
					p.tools++
					toolTypeCounts[name]++

					switch name {
					case "Write":
						turnHasWrite = true
						wi := session.ParseWriteInput(b.Input)
						lines := session.CountLines(wi.Content)
						totalWritten += lines
						p.written += lines
						if wi.FilePath != "" {
							filesTouched[wi.FilePath] = true
							p.files[wi.FilePath] = true
						}
					case "Edit":
						turnHasWrite = true
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
		// Final turn flush
		if inAssistantTurn {
			switch {
			case turnHasError:
				overheadTurns++
			case turnHasWrite:
				productiveTurns++
			default:
				researchTurns++
			}
		}
	}

	netLines := totalWritten + totalAdded - totalRemoved
	totalTurns := totalHumanTurns

	if totalTurns == 0 {
		fmt.Printf("\n  No activity found for %s\n", label)
		return
	}

	linesPerTurn := float64(netLines) / float64(totalTurns)
	toolsPerTurn := float64(totalToolCalls) / float64(totalTurns)
	filesPerTurn := float64(len(filesTouched)) / float64(totalTurns)
	errRate := 0.0
	if totalToolCalls > 0 {
		errRate = float64(totalErrors) / float64(totalToolCalls) * 100
	}

	format.Header(fmt.Sprintf("⚡  EFFICIENCY — %s", label), "═")
	fmt.Printf("\n  %-12s%-14s %-12s%s (%.1f/turn)\n",
		"Turns", format.Fmt(totalTurns),
		"Tools", format.Fmt(totalToolCalls), toolsPerTurn)
	fmt.Printf("  %-12s%-14s %-12s%d\n",
		"Net lines", format.Fmt(netLines), "Files", len(filesTouched))
	fmt.Printf("\n  %.1f lines/turn · %.2f files/turn · %.0f out-tokens/turn · %.1f%% errors\n",
		linesPerTurn, filesPerTurn,
		float64(totalOutputTokens)/float64(totalTurns),
		errRate,
	)

	// ── Turn Classification
	format.Header("🎯  TURN CLASSIFICATION", "─")
	classified := productiveTurns + researchTurns + overheadTurns
	maxCat := productiveTurns
	if researchTurns > maxCat {
		maxCat = researchTurns
	}
	if overheadTurns > maxCat {
		maxCat = overheadTurns
	}
	if maxCat == 0 {
		maxCat = 1
	}

	if classified > 0 {
		fmt.Printf(`
  Productive (Write/Edit):  %6d  %s  (%.0f%% of classified)
  Research (Read/Search):   %6d  %s  (%.0f%%)
  Overhead (errors):        %6d  %s  (%.0f%%)`+"\n",
			productiveTurns, format.Bar(float64(productiveTurns), float64(maxCat), 25),
			float64(productiveTurns)/float64(classified)*100,
			researchTurns, format.Bar(float64(researchTurns), float64(maxCat), 25),
			float64(researchTurns)/float64(classified)*100,
			overheadTurns, format.Bar(float64(overheadTurns), float64(maxCat), 25),
			float64(overheadTurns)/float64(classified)*100,
		)
	}

	// ── Per-Project Efficiency
	format.Header("📁  PROJECT EFFICIENCY", "─")
	type projEffEntry struct {
		name    string
		turns   int
		lines   int
		lpt     float64
		files   int
		errRate float64
		tools   int
	}
	var projList []projEffEntry
	for name, p := range proj {
		if p.turns < 3 {
			continue
		}
		pNet := p.written + p.added - p.removed
		lpt := 0.0
		if p.turns > 0 {
			lpt = float64(pNet) / float64(p.turns)
		}
		er := 0.0
		if p.tools > 0 {
			er = float64(p.errors) / float64(p.tools) * 100
		}
		projList = append(projList, projEffEntry{name, p.turns, pNet, lpt, len(p.files), er, p.tools})
	}
	sort.Slice(projList, func(i, j int) bool { return projList[i].lpt > projList[j].lpt })

	if len(projList) > 0 {
		fmt.Printf("\n  %-38s %5s %7s %7s %5s %6s\n",
			"Project", "Turns", "Lines", "L/Turn", "Files", "Err%")
		fmt.Printf("  %s %s %s %s %s %s\n",
			repeat("─", 38), repeat("─", 5), repeat("─", 7), repeat("─", 7), repeat("─", 5), repeat("─", 6))

		for _, e := range projList {
			warn := ""
			if e.errRate > 10 {
				warn = " ⚠️"
			}
			fmt.Printf("  %-38s %5d %7d %7.1f %5d %5.1f%%%s\n",
				truncate(e.name, 36), e.turns, e.lines, e.lpt, e.files, e.errRate, warn)
		}
	}

	// ── Quota Impact
	format.Header("📊  QUOTA IMPACT", "─")
	if totalErrors > 0 {
		wastedTokens := 0
		if totalToolCalls > 0 {
			wastedTokens = int(float64(totalErrors) * float64(totalOutputTokens) / float64(totalToolCalls))
		}
		fmt.Printf(`
  Output tokens used:      %s
  Est. wasted on errors:   ~%s output tokens (%d errors)

  If you eliminated all errors, you'd save ~%.1f%% of your tool calls,
  freeing up quota for ~%d more lines of code.`+"\n",
			format.Fmt(totalOutputTokens),
			format.Fmt(wastedTokens), totalErrors,
			float64(totalErrors)/float64(totalToolCalls)*100,
			int(float64(totalErrors)*linesPerTurn),
		)
	} else {
		fmt.Printf(`
  Output tokens used:      %s
  Error rate:              0%% — no wasted quota!`+"\n",
			format.Fmt(totalOutputTokens))
	}

	// ── Tool Mix
	format.Header("🔧  TOOL MIX", "─")
	categories := map[string]int{
		"Code (Edit/Write)":       toolTypeCounts["Edit"] + toolTypeCounts["Write"],
		"Read (Read/Glob/Grep)":   toolTypeCounts["Read"] + toolTypeCounts["Glob"] + toolTypeCounts["Grep"],
		"Shell (Bash)":            toolTypeCounts["Bash"],
		"Search (Web)":            toolTypeCounts["WebSearch"] + toolTypeCounts["WebFetch"],
		"Tasks":                   toolTypeCounts["TaskCreate"] + toolTypeCounts["TaskUpdate"] + toolTypeCounts["TaskList"] + toolTypeCounts["TaskGet"],
		"Agents (Task/Message)":   toolTypeCounts["Task"] + toolTypeCounts["SendMessage"],
		"Other":                   toolTypeCounts["AskUserQuestion"] + toolTypeCounts["Skill"] + toolTypeCounts["EnterPlanMode"] + toolTypeCounts["ExitPlanMode"] + toolTypeCounts["NotebookEdit"],
	}
	maxCatVal := 0
	for _, v := range categories {
		if v > maxCatVal {
			maxCatVal = v
		}
	}

	type catEntry struct {
		name  string
		count int
	}
	var catList []catEntry
	for name, count := range categories {
		catList = append(catList, catEntry{name, count})
	}
	sort.Slice(catList, func(i, j int) bool { return catList[i].count > catList[j].count })

	fmt.Println()
	for _, e := range catList {
		if e.count == 0 {
			continue
		}
		pct := 0.0
		if totalToolCalls > 0 {
			pct = float64(e.count) / float64(totalToolCalls) * 100
		}
		fmt.Printf("  %-28s %6d  (%4.0f%%)  %s\n",
			e.name, e.count, pct,
			format.Bar(float64(e.count), float64(maxCatVal), 15))
	}

	shellRatio := 0.0
	if totalToolCalls > 0 {
		shellRatio = float64(toolTypeCounts["Bash"]) / float64(totalToolCalls) * 100
	}

	// ── Recommendations
	format.Header("💡  RECOMMENDATIONS", "─")
	var recs []string

	if errRate > 8 {
		recs = append(recs, fmt.Sprintf("Error rate is %.1f%%. Top causes are usually Edit mismatches and Bash failures. Run `claude-stats tools` to see which tools fail most.", errRate))
	} else if errRate > 4 {
		recs = append(recs, fmt.Sprintf("Error rate %.1f%% is moderate. Some errors are unavoidable but there may be room to improve.", errRate))
	}

	if linesPerTurn < 1 && totalTurns > 20 {
		recs = append(recs, fmt.Sprintf("Producing %.1f lines/turn — heavy on research/exploration. Normal for new projects, but check if sessions are staying productive.", linesPerTurn))
	} else if linesPerTurn > 5 {
		recs = append(recs, fmt.Sprintf("High output at %.1f lines/turn — productive workflow.", linesPerTurn))
	}

	if shellRatio > 40 {
		recs = append(recs, fmt.Sprintf("Bash is %.0f%% of tool calls. Some of these might be replaceable with dedicated tools (Read instead of cat, Grep instead of grep).", shellRatio))
	}

	if researchTurns > productiveTurns*3 {
		recs = append(recs, fmt.Sprintf("Research turns outnumber productive turns %d:%d. Consider using Plan mode or Task agents for upfront research.", researchTurns, productiveTurns))
	}

	if classified > 0 && overheadTurns > classified/10 && overheadTurns > 5 {
		recs = append(recs, fmt.Sprintf("%d turns (%.0f%%) were overhead (errors). Allowlisting more commands via `claude-stats prompts` could help.",
			overheadTurns, float64(overheadTurns)/float64(classified)*100))
	}

	poorProj := []string{}
	for _, e := range projList {
		if e.errRate > 15 && e.turns > 10 {
			poorProj = append(poorProj, e.name)
		}
	}
	if len(poorProj) > 0 {
		names := ""
		for i, n := range poorProj {
			if i >= 2 {
				break
			}
			if names != "" {
				names += ", "
			}
			names += truncate(n, 20)
		}
		recs = append(recs, fmt.Sprintf("High error rate in: %s. Check project-specific CLAUDE.md or allowlist for those repos.", names))
	}

	if len(recs) == 0 {
		recs = append(recs, "Workflow looks efficient. No major optimization opportunities.")
	}

	for i, rec := range recs {
		fmt.Printf("  %d. %s\n", i+1, rec)
	}
	fmt.Println()
}
