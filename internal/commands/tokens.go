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

type tokenCounts struct {
	input, output, cacheRead, cacheCreate int
}

func (t *tokenCounts) add(u *session.Usage) {
	if u == nil {
		return
	}
	t.input += u.InputTokens
	t.output += u.OutputTokens
	t.cacheRead += u.CacheReadInputTokens
	t.cacheCreate += u.CacheCreationInputTokens
}

func calcCost(tc tokenCounts, model string) float64 {
	p := config.GetPricing(model)
	return float64(tc.input)/1e6*p.Input +
		float64(tc.output)/1e6*p.Output +
		float64(tc.cacheRead)/1e6*p.CacheRead +
		float64(tc.cacheCreate)/1e6*p.CacheCreate
}

type projTokens struct {
	tokenCounts
	messages, tools, sessions int
	models                    map[string]*tokenCounts
	isSubagent                bool
}

func Tokens(args []string) {
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

	targetDates, label := dates.ParseArgs(args)
	if label == "" {
		return
	}
	files := session.Find(targetDates, false)
	if len(files) == 0 {
		fmt.Printf("\n  No sessions found for %s\n", label)
		return
	}

	proj := map[string]*projTokens{}
	modelsGlobal := map[string]*tokenCounts{}
	var grand tokenCounts
	grandMessages, grandTools := 0, 0
	mainSessions, subagentCount := 0, 0

	for _, f := range files {
		isSub := containsSubagentsPath(f)
		if isSub {
			subagentCount++
		} else {
			mainSessions++
		}

		projName := projects.ExtractProject(f)
		if _, ok := proj[projName]; !ok {
			proj[projName] = &projTokens{models: map[string]*tokenCounts{}}
		}
		p := proj[projName]
		if !isSub {
			p.sessions++
		}

		session.ScanLines(f, func(line session.LogLine) {
			if session.IsConversation(line) {
				grandMessages++
				p.messages++
			}
			if line.Type != "assistant" {
				return
			}
			msg, ok := session.ParseAssistantMsg(line.Message)
			if !ok {
				return
			}
			for _, b := range msg.Content {
				if b.Type == "tool_use" {
					grandTools++
					p.tools++
				}
			}
			if msg.Usage != nil {
				grand.add(msg.Usage)
				p.add(msg.Usage)
				if _, ok := modelsGlobal[msg.Model]; !ok {
					modelsGlobal[msg.Model] = &tokenCounts{}
				}
				modelsGlobal[msg.Model].add(msg.Usage)
				if _, ok := p.models[msg.Model]; !ok {
					p.models[msg.Model] = &tokenCounts{}
				}
				p.models[msg.Model].add(msg.Usage)
			}
		})
	}

	// Calc total cost
	totalCost := 0.0
	for m, tc := range modelsGlobal {
		totalCost += calcCost(*tc, m)
	}
	totalTokens := grand.input + grand.output + grand.cacheRead + grand.cacheCreate

	if outputJSON {
		type projJSON struct {
			Name     string  `json:"name"`
			Sessions int     `json:"sessions"`
			Messages int     `json:"messages"`
			Tools    int     `json:"tools"`
			Output   int     `json:"output_tokens"`
			Cost     float64 `json:"cost"`
		}
		type modelJSON struct {
			Name   string  `json:"name"`
			Input  int     `json:"input_tokens"`
			Output int     `json:"output_tokens"`
			Cost   float64 `json:"cost"`
		}
		var pjs []projJSON
		for name, p := range proj {
			c := 0.0
			for m, tc := range p.models {
				c += calcCost(*tc, m)
			}
			pjs = append(pjs, projJSON{name, p.sessions, p.messages, p.tools, p.output, c})
		}
		sort.Slice(pjs, func(i, j int) bool { return pjs[i].Cost > pjs[j].Cost })

		var mjs []modelJSON
		for name, tc := range modelsGlobal {
			mjs = append(mjs, modelJSON{name, tc.input, tc.output, calcCost(*tc, name)})
		}
		sort.Slice(mjs, func(i, j int) bool { return mjs[i].Cost > mjs[j].Cost })

		out := map[string]interface{}{
			"label":         label,
			"sessions":      mainSessions,
			"subagents":     subagentCount,
			"messages":      grandMessages,
			"tool_calls":    grandTools,
			"input_tokens":  grand.input,
			"output_tokens": grand.output,
			"cache_read":    grand.cacheRead,
			"cache_create":  grand.cacheCreate,
			"total_tokens":  totalTokens,
			"total_cost":    totalCost,
			"projects":      pjs,
			"models":        mjs,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	format.Header(fmt.Sprintf("🔢  TOKEN USAGE — %s", label), "═")
	sessStr := fmt.Sprintf("%d", mainSessions)
	if subagentCount > 0 {
		sessStr = fmt.Sprintf("%d (+%d sub)", mainSessions, subagentCount)
	}
	fmt.Printf("\n  %-12s%-18s %-12s%s\n",
		"Sessions", sessStr, "Messages", format.Fmt(grandMessages))
	fmt.Printf("  %-12s%-18d %-12s%s\n",
		"Projects", len(proj), "Tool calls", format.Fmt(grandTools))
	fmt.Printf("\n  Input %s · Output %s · Cache r/w %s / %s\n",
		format.FmtTokens(grand.input),
		format.FmtTokens(grand.output),
		format.FmtTokens(grand.cacheRead),
		format.FmtTokens(grand.cacheCreate))
	fmt.Printf("  Total %s tokens · Est. cost $%.2f\n",
		format.FmtTokens(totalTokens), totalCost)

	// ── By Project
	format.Header("📁  BY PROJECT", "─")
	type projEntry struct {
		name string
		p    *projTokens
		cost float64
	}
	var projList []projEntry
	for name, p := range proj {
		c := 0.0
		for m, tc := range p.models {
			c += calcCost(*tc, m)
		}
		projList = append(projList, projEntry{name, p, c})
	}
	sort.Slice(projList, func(i, j int) bool { return projList[i].p.output > projList[j].p.output })

	maxCost := 0.0
	for _, e := range projList {
		if e.cost > maxCost {
			maxCost = e.cost
		}
	}

	fmt.Printf("\n  %-42s %4s %5s %9s %8s  %s\n",
		"Project", "Sess", "Msgs", "Output", "Cost", "")
	fmt.Printf("  %s %s %s %s %s  %s\n",
		repeat("─", 42), repeat("─", 4), repeat("─", 5), repeat("─", 9), repeat("─", 8), repeat("─", 15))

	for _, e := range projList {
		name := truncate(e.name, 40)
		fmt.Printf("  %-42s %4d %5d %9s $%7.1f  %s\n",
			name, e.p.sessions, e.p.messages,
			format.Fmt(e.p.output), e.cost,
			format.Bar(e.cost, maxCost, 15))
	}
	fmt.Printf("  %s %s %s %s %s\n",
		repeat("─", 42), repeat("─", 4), repeat("─", 5), repeat("─", 9), repeat("─", 8))
	fmt.Printf("  %-42s %4d %5d %9s $%7.1f\n",
		"TOTAL", mainSessions, grandMessages, format.Fmt(grand.output), totalCost)

	// ── By Model
	format.Header("🤖  BY MODEL", "─")
	type modelEntry struct {
		name string
		tc   *tokenCounts
		cost float64
	}
	var modelList []modelEntry
	maxModelCost := 0.0
	for m, tc := range modelsGlobal {
		if tc.output == 0 {
			continue
		}
		c := calcCost(*tc, m)
		modelList = append(modelList, modelEntry{m, tc, c})
		if c > maxModelCost {
			maxModelCost = c
		}
	}
	sort.Slice(modelList, func(i, j int) bool { return modelList[i].tc.output > modelList[j].tc.output })

	fmt.Printf("\n  %-25s %10s %5s %9s  %s\n", "Model", "Output", "%", "Cost", "")
	fmt.Printf("  %s %s %s %s  %s\n",
		repeat("─", 25), repeat("─", 10), repeat("─", 5), repeat("─", 9), repeat("─", 15))

	for _, e := range modelList {
		pct := 0.0
		if grand.output > 0 {
			pct = float64(e.tc.output) / float64(grand.output) * 100
		}
		name := format.FriendlyModel(e.name)
		fmt.Printf("  %-25s %10s %4.0f%% $%8.1f  %s\n",
			name, format.Fmt(e.tc.output), pct, e.cost,
			format.Bar(e.cost, maxModelCost, 15))
	}

	// ── Cost Breakdown
	format.Header("💰  COST BREAKDOWN", "─")
	type costPart struct {
		cat, model string
		cost       float64
		tokens     int
	}
	var parts []costPart
	for m, tc := range modelsGlobal {
		p := config.GetPricing(m)
		cats := []struct {
			key    string
			tokens int
			price  float64
		}{
			{"cache_read", tc.cacheRead, p.CacheRead},
			{"cache_create", tc.cacheCreate, p.CacheCreate},
			{"output", tc.output, p.Output},
			{"input", tc.input, p.Input},
		}
		for _, c := range cats {
			cost := float64(c.tokens) / 1e6 * c.price
			if cost >= 0.50 {
				parts = append(parts, costPart{c.key, m, cost, c.tokens})
			}
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].cost > parts[j].cost })

	maxPart := 0.0
	if len(parts) > 0 {
		maxPart = parts[0].cost
	}
	shownCost := 0.0

	fmt.Printf("\n  %-35s %9s %14s  %s\n", "Category", "Cost", "Tokens", "")
	fmt.Printf("  %s %s %s  %s\n",
		repeat("─", 35), repeat("─", 9), repeat("─", 14), repeat("─", 12))

	for _, pt := range parts {
		name := format.FriendlyModel(pt.model)
		catLabel := titleCase(strings.ReplaceAll(pt.cat, "_", " "))
		label := fmt.Sprintf("%s (%s)", catLabel, name)
		padding := 35 - len(label)
		if padding < 0 {
			padding = 0
		}
		fmt.Printf("  %s%s $%8.1f %14s  %s\n",
			label, repeat(" ", padding), pt.cost,
			format.Fmt(pt.tokens),
			format.Bar(pt.cost, maxPart, 12))
		shownCost += pt.cost
	}

	otherCost := totalCost - shownCost
	if otherCost >= 0.01 {
		fmt.Printf("  %-35s $%8.2f\n", "Other (<$0.50 each)", otherCost)
	}
	fmt.Printf("  %s %s\n", repeat("─", 35), repeat("─", 9))
	fmt.Printf("  %-35s $%8.2f\n\n", "TOTAL", totalCost)
}

func containsSubagentsPath(path string) bool {
	clean := slashPath(path)
	for i := 0; i+9 <= len(clean); i++ {
		if clean[i:i+9] == "subagents" {
			return true
		}
	}
	return false
}
