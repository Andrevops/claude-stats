package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Andrevops/claude-stats/internal/format"
	"github.com/Andrevops/claude-stats/internal/projects"
	"github.com/Andrevops/claude-stats/internal/session"
)

type periodStats struct {
	label                                string
	dates                                []string
	sessions, subagents                  int
	messages, turns, toolCalls, errors   int
	input, output, cacheRead, cacheCreate int
	written, added, removed              int
	filesSet                             map[string]bool
	models                               map[string]*tokenCounts
	projLines                            map[string]int
}

func (p *periodStats) cost() float64 {
	total := 0.0
	for model, tc := range p.models {
		total += calcCost(*tc, model)
	}
	return total
}

func (p *periodStats) netLines() int {
	return p.written + p.added - p.removed
}

func (p *periodStats) files() int {
	return len(p.filesSet)
}

func collectPeriod(targetDates []string) *periodStats {
	ps := &periodStats{
		dates:    targetDates,
		filesSet: map[string]bool{},
		models:   map[string]*tokenCounts{},
		projLines: map[string]int{},
	}

	files := session.Find(targetDates, false)
	for _, f := range files {
		projName := projects.ExtractProject(f)
		isSub := strings.Contains(f, "subagents")
		if isSub {
			ps.subagents++
		} else {
			ps.sessions++
		}

		session.ScanLines(f, func(line session.LogLine) {
			if session.IsConversation(line) {
				ps.messages++
			}
			if session.IsUserTurn(line) {
				ps.turns++
			}
			if line.Type == "user" {
				for _, tr := range session.ParseToolResults(line.Message) {
					if tr.IsError {
						ps.errors++
					}
				}
			}
			if line.Type == "assistant" {
				msg, ok := session.ParseAssistantMsg(line.Message)
				if !ok {
					return
				}
				if msg.Usage != nil {
					model := msg.Model
					if _, ok := ps.models[model]; !ok {
						ps.models[model] = &tokenCounts{}
					}
					ps.models[model].add(msg.Usage)
					ps.input += msg.Usage.InputTokens
					ps.output += msg.Usage.OutputTokens
					ps.cacheRead += msg.Usage.CacheReadInputTokens
					ps.cacheCreate += msg.Usage.CacheCreationInputTokens
				}
				for _, b := range msg.Content {
					if b.Type != "tool_use" {
						continue
					}
					ps.toolCalls++
					switch b.Name {
					case "Write":
						wi := session.ParseWriteInput(b.Input)
						lines := session.CountLines(wi.Content)
						ps.written += lines
						if wi.FilePath != "" {
							ps.filesSet[wi.FilePath] = true
						}
						ps.projLines[projName] += lines
					case "Edit":
						ei := session.ParseEditInput(b.Input)
						added := session.CountLines(ei.NewString)
						removed := session.CountLines(ei.OldString)
						ps.added += added
						ps.removed += removed
						if ei.FilePath != "" {
							ps.filesSet[ei.FilePath] = true
						}
						ps.projLines[projName] += added - removed
					}
				}
			}
		})
	}
	return ps
}


func delta(cur, prev int) string {
	if prev == 0 {
		if cur == 0 {
			return "  —"
		}
		return " new"
	}
	pct := float64(cur-prev) / float64(prev) * 100
	if pct > 0 {
		return fmt.Sprintf(" +%.0f%%", pct)
	}
	return fmt.Sprintf(" %.0f%%", pct)
}

func deltaF(cur, prev float64) string {
	if prev == 0 {
		if cur == 0 {
			return "  —"
		}
		return " new"
	}
	pct := (cur - prev) / prev * 100
	if pct > 0 {
		return fmt.Sprintf(" +%.0f%%", pct)
	}
	return fmt.Sprintf(" %.0f%%", pct)
}

func arrow(cur, prev int) string {
	if cur > prev {
		return "▲"
	}
	if cur < prev {
		return "▼"
	}
	return "="
}

func arrowF(cur, prev float64) string {
	if cur > prev {
		return "▲"
	}
	if cur < prev {
		return "▼"
	}
	return "="
}

type TrendsJSON struct {
	Current  TrendsPeriodJSON   `json:"current"`
	Previous TrendsPeriodJSON   `json:"previous"`
	Deltas   TrendsDeltasJSON   `json:"deltas"`
	Projects []TrendsProjectJSON `json:"projects,omitempty"`
}

type TrendsPeriodJSON struct {
	Label     string  `json:"label"`
	Sessions  int     `json:"sessions"`
	Messages  int     `json:"messages"`
	ToolCalls int     `json:"tool_calls"`
	Errors    int     `json:"errors"`
	Cost      float64 `json:"cost"`
	NetLines  int     `json:"net_lines"`
	Files     int     `json:"files"`
}

type TrendsDeltasJSON struct {
	Sessions  string `json:"sessions"`
	Messages  string `json:"messages"`
	ToolCalls string `json:"tool_calls"`
	Errors    string `json:"errors"`
	Cost      string `json:"cost"`
	NetLines  string `json:"net_lines"`
	Files     string `json:"files"`
}

type TrendsProjectJSON struct {
	Name     string `json:"name"`
	Current  int    `json:"current_lines"`
	Previous int    `json:"previous_lines"`
	Delta    string `json:"delta"`
}

func Trends(args []string) {
	outputJSON := false
	filtered := []string{}
	for _, a := range args {
		if a == "--json" {
			outputJSON = true
		} else {
			filtered = append(filtered, a)
		}
	}

	today := time.Now()
	var curDates, prevDates []string
	var curLabel, prevLabel string
	periodDays := 7 // default

	if len(filtered) == 0 || filtered[0] == "--week" {
		periodDays = 7
		for i := 0; i < 7; i++ {
			curDates = append(curDates, today.AddDate(0, 0, -i).Format("2006-01-02"))
		}
		for i := 7; i < 14; i++ {
			prevDates = append(prevDates, today.AddDate(0, 0, -i).Format("2006-01-02"))
		}
		curLabel = fmt.Sprintf("This week (%s → %s)", curDates[6], curDates[0])
		prevLabel = fmt.Sprintf("Last week (%s → %s)", prevDates[6], prevDates[0])
	} else if filtered[0] == "--month" {
		periodDays = 30
		for i := 0; i < 30; i++ {
			curDates = append(curDates, today.AddDate(0, 0, -i).Format("2006-01-02"))
		}
		for i := 30; i < 60; i++ {
			prevDates = append(prevDates, today.AddDate(0, 0, -i).Format("2006-01-02"))
		}
		curLabel = fmt.Sprintf("This month (%s → %s)", curDates[29], curDates[0])
		prevLabel = fmt.Sprintf("Last month (%s → %s)", prevDates[29], prevDates[0])
	} else if filtered[0] == "--yesterday" {
		periodDays = 1
		curDates = []string{today.AddDate(0, 0, -1).Format("2006-01-02")}
		prevDates = []string{today.AddDate(0, 0, -2).Format("2006-01-02")}
		curLabel = fmt.Sprintf("Yesterday (%s)", curDates[0])
		prevLabel = fmt.Sprintf("Day before (%s)", prevDates[0])
	} else {
		fmt.Println("Usage: claude-stats trends [--yesterday|--week|--month]")
		fmt.Println("  Compares current period against the previous equivalent period.")
		return
	}

	_ = periodDays
	cur := collectPeriod(curDates)
	prev := collectPeriod(prevDates)
	cur.label = curLabel
	prev.label = prevLabel

	if outputJSON {
		trendsJSON(cur, prev)
		return
	}

	// ── Header (keep title short — dates are repeated in the comparison tables)
	format.Header(fmt.Sprintf("📈  TRENDS — %s", curLabel), "═")
	fmt.Printf("\n  Comparing against: %s\n", prevLabel)

	// ── Overview comparison
	format.Header("📋  OVERVIEW", "─")
	fmt.Printf("\n  %-24s %12s %12s %8s\n", "Metric", "Current", "Previous", "Change")
	fmt.Printf("  %-24s %12s %12s %8s\n", "────────────────────────", "────────────", "────────────", "────────")

	rows := []struct {
		label      string
		cur, prev  int
		isCost     bool
		curF, preF float64
	}{
		{"Sessions", cur.sessions, prev.sessions, false, 0, 0},
		{"Messages", cur.messages, prev.messages, false, 0, 0},
		{"Tool calls", cur.toolCalls, prev.toolCalls, false, 0, 0},
		{"Errors", cur.errors, prev.errors, false, 0, 0},
		{"Net lines", cur.netLines(), prev.netLines(), false, 0, 0},
		{"Files touched", cur.files(), prev.files(), false, 0, 0},
		{"Est. cost", 0, 0, true, cur.cost(), prev.cost()},
	}

	for _, r := range rows {
		if r.isCost {
			fmt.Printf("  %-24s %11s %11s %6s %s\n",
				r.label,
				fmt.Sprintf("$%.0f", r.curF),
				fmt.Sprintf("$%.0f", r.preF),
				deltaF(r.curF, r.preF),
				format.ArrowColor(arrowF(r.curF, r.preF)))
		} else {
			fmt.Printf("  %-24s %12s %12s %6s %s\n",
				r.label,
				format.Fmt(r.cur),
				format.Fmt(r.prev),
				delta(r.cur, r.prev),
				format.ArrowColor(arrow(r.cur, r.prev)))
		}
	}

	// ── Efficiency comparison
	format.Header("⚡  EFFICIENCY", "─")
	curLPT := 0.0
	prevLPT := 0.0
	if cur.turns > 0 {
		curLPT = float64(cur.netLines()) / float64(cur.turns)
	}
	if prev.turns > 0 {
		prevLPT = float64(prev.netLines()) / float64(prev.turns)
	}
	curErrRate := 0.0
	prevErrRate := 0.0
	if cur.toolCalls > 0 {
		curErrRate = float64(cur.errors) / float64(cur.toolCalls) * 100
	}
	if prev.toolCalls > 0 {
		prevErrRate = float64(prev.errors) / float64(prev.toolCalls) * 100
	}

	fmt.Printf("\n  %-24s %12s %12s %8s\n", "Metric", "Current", "Previous", "Change")
	fmt.Printf("  %-24s %12s %12s %8s\n", "────────────────────────", "────────────", "────────────", "────────")
	fmt.Printf("  %-24s %12.1f %12.1f %6s %s\n", "Lines/turn", curLPT, prevLPT, deltaF(curLPT, prevLPT), format.ArrowColor(arrowF(curLPT, prevLPT)))
	fmt.Printf("  %-24s %11.1f%% %11.1f%% %6s %s\n", "Error rate", curErrRate, prevErrRate, deltaF(curErrRate, prevErrRate), format.ArrowColor(arrowF(curErrRate, prevErrRate)))

	// ── Project comparison
	allProjects := map[string]bool{}
	for p := range cur.projLines {
		allProjects[p] = true
	}
	for p := range prev.projLines {
		allProjects[p] = true
	}

	if len(allProjects) > 0 {
		format.Header("📁  PROJECT COMPARISON (net lines)", "─")

		type projRow struct {
			name     string
			curL     int
			prevL    int
		}
		var projRows []projRow
		for p := range allProjects {
			projRows = append(projRows, projRow{p, cur.projLines[p], prev.projLines[p]})
		}
		sort.Slice(projRows, func(i, j int) bool {
			return projRows[i].curL > projRows[j].curL
		})

		fmt.Printf("\n  %-36s %8s %8s %8s\n", "Project", "Current", "Previous", "Change")
		fmt.Printf("  %-36s %8s %8s %8s\n", "────────────────────────────────────", "────────", "────────", "────────")

		for _, r := range projRows {
			name := r.name
			if len(name) > 34 {
				name = name[:34]
			}
			fmt.Printf("  %-36s %8s %8s %6s %s\n",
				name,
				format.Fmt(r.curL),
				format.Fmt(r.prevL),
				delta(r.curL, r.prevL),
				format.ArrowColor(arrow(r.curL, r.prevL)))
		}
	}

	// ── Token breakdown
	format.Header("🔢  TOKEN COMPARISON", "─")
	fmt.Printf("\n  %-24s %14s %14s %8s\n", "Category", "Current", "Previous", "Change")
	fmt.Printf("  %-24s %14s %14s %8s\n", "────────────────────────", "──────────────", "──────────────", "────────")
	fmt.Printf("  %-24s %14s %14s %6s %s\n", "Input", format.Fmt(cur.input), format.Fmt(prev.input), delta(cur.input, prev.input), format.ArrowColor(arrow(cur.input, prev.input)))
	fmt.Printf("  %-24s %14s %14s %6s %s\n", "Output", format.Fmt(cur.output), format.Fmt(prev.output), delta(cur.output, prev.output), format.ArrowColor(arrow(cur.output, prev.output)))
	fmt.Printf("  %-24s %14s %14s %6s %s\n", "Cache read", format.Fmt(cur.cacheRead), format.Fmt(prev.cacheRead), delta(cur.cacheRead, prev.cacheRead), format.ArrowColor(arrow(cur.cacheRead, prev.cacheRead)))
	fmt.Printf("  %-24s %14s %14s %6s %s\n", "Cache create", format.Fmt(cur.cacheCreate), format.Fmt(prev.cacheCreate), delta(cur.cacheCreate, prev.cacheCreate), format.ArrowColor(arrow(cur.cacheCreate, prev.cacheCreate)))

	// ── Verdict
	format.Header("💡  SUMMARY", "─")
	verdicts := []string{}

	costDiff := cur.cost() - prev.cost()
	if costDiff > 0 {
		verdicts = append(verdicts, fmt.Sprintf("Spending up $%.0f (%s)", costDiff, deltaF(cur.cost(), prev.cost())))
	} else if costDiff < 0 {
		verdicts = append(verdicts, fmt.Sprintf("Spending down $%.0f (%s)", -costDiff, deltaF(cur.cost(), prev.cost())))
	}

	if curLPT > prevLPT*1.1 {
		verdicts = append(verdicts, fmt.Sprintf("Productivity improved: %.1f → %.1f lines/turn", prevLPT, curLPT))
	} else if curLPT < prevLPT*0.9 {
		verdicts = append(verdicts, fmt.Sprintf("Productivity dropped: %.1f → %.1f lines/turn", prevLPT, curLPT))
	}

	if curErrRate < prevErrRate*0.8 && prev.errors > 5 {
		verdicts = append(verdicts, fmt.Sprintf("Error rate improved: %.1f%% → %.1f%%", prevErrRate, curErrRate))
	} else if curErrRate > prevErrRate*1.2 && cur.errors > 5 {
		verdicts = append(verdicts, fmt.Sprintf("Error rate worsened: %.1f%% → %.1f%%", prevErrRate, curErrRate))
	}

	if len(verdicts) == 0 {
		verdicts = append(verdicts, "Stable period — no significant changes.")
	}

	for i, v := range verdicts {
		fmt.Printf("  %d. %s\n", i+1, v)
	}
	fmt.Println()
}

func trendsJSON(cur, prev *periodStats) {
	// Collect projects
	allProjects := map[string]bool{}
	for p := range cur.projLines {
		allProjects[p] = true
	}
	for p := range prev.projLines {
		allProjects[p] = true
	}
	var projs []TrendsProjectJSON
	for p := range allProjects {
		projs = append(projs, TrendsProjectJSON{
			Name:     p,
			Current:  cur.projLines[p],
			Previous: prev.projLines[p],
			Delta:    delta(cur.projLines[p], prev.projLines[p]),
		})
	}
	sort.Slice(projs, func(i, j int) bool {
		return projs[i].Current > projs[j].Current
	})

	out := TrendsJSON{
		Current: TrendsPeriodJSON{
			Label: cur.label, Sessions: cur.sessions, Messages: cur.messages,
			ToolCalls: cur.toolCalls, Errors: cur.errors,
			Cost: cur.cost(), NetLines: cur.netLines(), Files: cur.files(),
		},
		Previous: TrendsPeriodJSON{
			Label: prev.label, Sessions: prev.sessions, Messages: prev.messages,
			ToolCalls: prev.toolCalls, Errors: prev.errors,
			Cost: prev.cost(), NetLines: prev.netLines(), Files: prev.files(),
		},
		Deltas: TrendsDeltasJSON{
			Sessions:  delta(cur.sessions, prev.sessions),
			Messages:  delta(cur.messages, prev.messages),
			ToolCalls: delta(cur.toolCalls, prev.toolCalls),
			Errors:    delta(cur.errors, prev.errors),
			Cost:      deltaF(cur.cost(), prev.cost()),
			NetLines:  delta(cur.netLines(), prev.netLines()),
			Files:     delta(cur.files(), prev.files()),
		},
		Projects: projs,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}
