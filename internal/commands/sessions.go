package commands

import (
	"fmt"
	"sort"
	"time"

	"github.com/Andrevops/claude-stats/internal/dates"
	"github.com/Andrevops/claude-stats/internal/format"
	"github.com/Andrevops/claude-stats/internal/projects"
	"github.com/Andrevops/claude-stats/internal/session"
)

type sessData struct {
	project                                 string
	start                                   time.Time
	duration                                float64
	messages, humanMessages, toolCalls      int
	errors                                  int
	totalOutput, totalCacheRead, totalCacheCreate int
	avgContext, peakContext, contextGrowth  float64
	contextSizes                            []float64
	linesWritten, linesAdded, linesRemoved  int
	filesTouched                            int
	netLines                                int
}

func Sessions(args []string) {
	targetDates, label := dates.ParseArgs(args)
	if label == "" {
		return
	}
	files := session.Find(targetDates, true) // skip subagents
	if len(files) == 0 {
		fmt.Printf("\n  No sessions found for %s\n", label)
		return
	}

	var sessionList []*sessData
	for _, f := range files {
		s := parseSessionHealth(f)
		if s != nil && s.messages >= 4 {
			sessionList = append(sessionList, s)
		}
	}

	if len(sessionList) == 0 {
		fmt.Printf("\n  No meaningful sessions found for %s\n", label)
		return
	}

	totalMsgs, totalTurns, totalTools, totalErrors := 0, 0, 0, 0
	totalDuration, totalOutput := 0.0, 0
	for _, s := range sessionList {
		totalMsgs += s.messages
		totalTurns += s.humanMessages
		totalTools += s.toolCalls
		totalErrors += s.errors
		totalDuration += s.duration
		totalOutput += s.totalOutput
	}
	avgDuration := totalDuration / float64(len(sessionList))
	avgCtx := 0.0
	for _, s := range sessionList {
		avgCtx += s.avgContext
	}
	avgCtx /= float64(len(sessionList))

	format.Header(fmt.Sprintf("🏥  SESSION HEALTH — %s", label), "═")
	fmt.Printf("\n  %-10s%-16d %-10s%s\n",
		"Sessions", len(sessionList), "Turns", format.Fmt(totalTurns))
	fmt.Printf("  %-10s%-16s %-10s%s (avg %s)\n",
		"Messages", format.Fmt(totalMsgs), "Active", format.FmtDuration(totalDuration), format.FmtDuration(avgDuration))
	fmt.Printf("  %-10s%-16s %-10s%s (%.1f%%)\n",
		"Tools", format.Fmt(totalTools), "Errors", format.Fmt(totalErrors), errorRate(totalErrors, totalTools))
	fmt.Printf("  %-10s%s/turn\n", "Context", format.FmtTokens(int(avgCtx)))

	// ── Session Table
	format.Header("📋  ALL SESSIONS", "─")
	sortedSessions := make([]*sessData, len(sessionList))
	copy(sortedSessions, sessionList)
	sort.Slice(sortedSessions, func(i, j int) bool {
		return sortedSessions[i].start.After(sortedSessions[j].start)
	})

	fmt.Printf("\n  %-12s %5s %5s %5s %4s %6s %7s %6s %5s  Project\n",
		"Date", "Dur", "Msgs", "Tools", "Err", "Ctx", "Growth", "Lines", "Files")
	fmt.Printf("  %s %s %s %s %s %s %s %s %s  %s\n",
		repeat("─", 12), repeat("─", 5), repeat("─", 5), repeat("─", 5),
		repeat("─", 4), repeat("─", 6), repeat("─", 7), repeat("─", 6),
		repeat("─", 5), repeat("─", 30))

	for _, s := range sortedSessions {
		dateStr := s.start.Format("2006-01-02")
		dur := format.FmtDuration(s.duration)
		ctx := format.FmtTokens(int(s.avgContext))
		sign := "+"
		if s.contextGrowth < 0 {
			sign = ""
		}
		growth := fmt.Sprintf("%s%.0f%%", sign, s.contextGrowth)
		errStr := "—"
		if s.errors > 0 {
			errStr = fmt.Sprintf("%d", s.errors)
		}
		proj := truncate(s.project, 28)
		warn := ""
		if s.contextGrowth > 200 {
			warn = " ⚠️"
		}
		fmt.Printf("  %-12s %5s %5d %5d %4s %6s %7s %6d %5d  %s%s\n",
			dateStr, dur, s.messages, s.toolCalls, errStr, ctx, growth,
			s.netLines, s.filesTouched, proj, warn)
	}

	// ── Context Growth
	format.Header("📈  CONTEXT GROWTH PATTERNS", "─")
	lowGrowth := []*sessData{}
	medGrowth := []*sessData{}
	highGrowth := []*sessData{}
	for _, s := range sessionList {
		switch {
		case s.contextGrowth < 50:
			lowGrowth = append(lowGrowth, s)
		case s.contextGrowth < 200:
			medGrowth = append(medGrowth, s)
		default:
			highGrowth = append(highGrowth, s)
		}
	}

	fmt.Printf(`
  Low growth  (<50%%):   %3d sessions  — context stays manageable
  Med growth  (50-200%%): %3d sessions  — normal for long sessions
  High growth (>200%%):  %3d sessions  — consider restarting ⚠️`+"\n",
		len(lowGrowth), len(medGrowth), len(highGrowth))

	if len(highGrowth) > 0 {
		fmt.Println("\n  High-growth sessions:")
		sort.Slice(highGrowth, func(i, j int) bool {
			return highGrowth[i].contextGrowth > highGrowth[j].contextGrowth
		})
		for i, s := range highGrowth {
			if i >= 5 {
				break
			}
			fmt.Printf("    %s %5s %+.0f%% ctx  %d msgs  %s\n",
				s.start.Format("2006-01-02"),
				format.FmtDuration(s.duration),
				s.contextGrowth, s.messages,
				truncate(s.project, 35))
		}
	}

	// ── Duration vs Productivity
	format.Header("⏱️  DURATION vs PRODUCTIVITY", "─")
	type bucket struct {
		label string
		pred  func(*sessData) bool
	}
	buckets := []bucket{
		{"< 30m", func(s *sessData) bool { return s.duration < 1800 }},
		{"30m-1h", func(s *sessData) bool { return s.duration >= 1800 && s.duration < 3600 }},
		{"1-2h", func(s *sessData) bool { return s.duration >= 3600 && s.duration < 7200 }},
		{"2-4h", func(s *sessData) bool { return s.duration >= 7200 && s.duration < 14400 }},
		{"4h+", func(s *sessData) bool { return s.duration >= 14400 }},
	}

	fmt.Printf("\n  %-10s %5s %9s %10s %10s %10s %6s\n",
		"Duration", "Count", "Avg msgs", "Avg lines", "Avg tools", "Lines/msg", "Err%")
	fmt.Printf("  %s %s %s %s %s %s %s\n",
		repeat("─", 10), repeat("─", 5), repeat("─", 9), repeat("─", 10),
		repeat("─", 10), repeat("─", 10), repeat("─", 6))

	for _, b := range buckets {
		var group []*sessData
		for _, s := range sessionList {
			if b.pred(s) {
				group = append(group, s)
			}
		}
		if len(group) == 0 {
			continue
		}
		totalM, totalL, totalT, totalMsgsG := 0, 0, 0, 0
		totalE, totalTC := 0, 0
		for _, s := range group {
			totalM += s.messages
			totalL += s.netLines
			totalT += s.toolCalls
			totalMsgsG += s.messages
			totalE += s.errors
			totalTC += s.toolCalls
		}
		n := len(group)
		lpm := 0.0
		if totalMsgsG > 0 {
			lpm = float64(totalL) / float64(totalMsgsG)
		}
		errPct := "—"
		if totalTC > 0 {
			errPct = fmt.Sprintf("%.1f%%", float64(totalE)/float64(totalTC)*100)
		}
		fmt.Printf("  %-10s %5d %9.0f %10.0f %10.0f %10.1f %6s\n",
			b.label, n,
			float64(totalM)/float64(n),
			float64(totalL)/float64(n),
			float64(totalT)/float64(n),
			lpm, errPct)
	}

	// ── Worst error-rate sessions (only show if there's signal)
	highErr := []*sessData{}
	for _, s := range sessionList {
		if s.toolCalls > 10 && float64(s.errors)/float64(s.toolCalls) > 0.1 {
			highErr = append(highErr, s)
		}
	}
	if len(highErr) > 0 {
		format.Header("🗑️  ERROR HOTSPOTS", "─")
		sort.Slice(highErr, func(i, j int) bool {
			ri := float64(highErr[i].errors) / float64(highErr[i].toolCalls)
			rj := float64(highErr[j].errors) / float64(highErr[j].toolCalls)
			return ri > rj
		})
		if len(highErr) > 5 {
			highErr = highErr[:5]
		}
		fmt.Println()
		for _, s := range highErr {
			rate := float64(s.errors) / float64(s.toolCalls) * 100
			fmt.Printf("  %s  %4.1f%% (%d/%d)  %s\n",
				s.start.Format("2006-01-02"), rate, s.errors, s.toolCalls,
				truncate(s.project, 35))
		}
	}

	// ── Recommendations
	format.Header("💡  RECOMMENDATIONS", "─")
	var recs []string

	if len(sessionList) >= 5 {
		var short, longS []*sessData
		for _, s := range sessionList {
			if s.duration < 3600 && s.messages > 5 {
				short = append(short, s)
			}
			if s.duration >= 7200 && s.messages > 5 {
				longS = append(longS, s)
			}
		}
		if len(short) > 0 && len(longS) > 0 {
			shortLPM := linesPerMsg(short)
			longLPM := linesPerMsg(longS)
			if shortLPM > longLPM*1.3 {
				recs = append(recs, fmt.Sprintf("Short sessions (<1h) produce %.1f lines/msg vs %.1f for long sessions (>2h). Consider shorter, focused sessions.", shortLPM, longLPM))
			} else if longLPM > shortLPM*1.3 {
				recs = append(recs, fmt.Sprintf("Long sessions (>2h) produce %.1f lines/msg vs %.1f for short. Your long sessions are productive — keep going.", longLPM, shortLPM))
			}
		}
	}

	if len(highGrowth) > 0 {
		avgGrowthDur := 0.0
		for _, s := range highGrowth {
			avgGrowthDur += s.duration
		}
		avgGrowthDur /= float64(len(highGrowth))
		recs = append(recs, fmt.Sprintf("%d sessions had >200%% context growth. Consider restarting after ~%s for these projects.",
			len(highGrowth), format.FmtDuration(avgGrowthDur/2)))
	}

	if totalTools > 0 {
		rate := float64(totalErrors) / float64(totalTools)
		if rate > 0.1 {
			recs = append(recs, fmt.Sprintf("Error rate is %.1f%% — above 10%%. Review common failures in claude-stats tools to reduce wasted turns.", rate*100))
		} else if totalErrors > 0 {
			recs = append(recs, fmt.Sprintf("Error rate %.1f%% is reasonable. Most errors are unavoidable (Edit mismatches, network issues).", rate*100))
		}
	}

	peakSessions := 0
	for _, s := range sessionList {
		if s.peakContext > 500_000 {
			peakSessions++
		}
	}
	if peakSessions > 0 {
		recs = append(recs, fmt.Sprintf("%d sessions hit >500K context tokens. Responses slow down significantly at high context — restart to reset.", peakSessions))
	}

	if len(recs) == 0 {
		recs = append(recs, "Sessions look healthy. No major optimization opportunities detected.")
	}

	for i, rec := range recs {
		fmt.Printf("  %d. %s\n", i+1, rec)
	}
	fmt.Println()
}

func parseSessionHealth(path string) *sessData {
	s := &sessData{
		project: projects.ExtractProject(path),
	}
	filesTouched := map[string]bool{}

	var prevTS time.Time
	activeSecs := 0.0
	session.ScanLines(path, func(line session.LogLine) {
		localDT, ok := dates.ParseTS(line.Timestamp)
		if ok {
			if s.start.IsZero() {
				s.start = localDT
			}
			if !prevTS.IsZero() {
				gap := localDT.Sub(prevTS).Seconds()
				if gap > 0 && gap < 900 { // 15 min idle threshold
					activeSecs += gap
				}
			}
			prevTS = localDT
		}
		if session.IsConversation(line) {
			s.messages++
		}
		if session.IsUserTurn(line) {
			s.humanMessages++
		}
		if line.Type == "assistant" {
			msg, parsed := session.ParseAssistantMsg(line.Message)
			if !parsed {
				return
			}
			if msg.Usage != nil {
				cr := msg.Usage.CacheReadInputTokens
				cc := msg.Usage.CacheCreationInputTokens
				out := msg.Usage.OutputTokens
				s.contextSizes = append(s.contextSizes, float64(cr+cc))
				s.totalOutput += out
				s.totalCacheRead += cr
				s.totalCacheCreate += cc
			}
			for _, b := range msg.Content {
				if b.Type == "tool_use" {
					s.toolCalls++
					switch b.Name {
					case "Write":
						wi := session.ParseWriteInput(b.Input)
						s.linesWritten += session.CountLines(wi.Content)
						if wi.FilePath != "" {
							filesTouched[wi.FilePath] = true
						}
					case "Edit":
						ei := session.ParseEditInput(b.Input)
						s.linesAdded += session.CountLines(ei.NewString)
						s.linesRemoved += session.CountLines(ei.OldString)
						if ei.FilePath != "" {
							filesTouched[ei.FilePath] = true
						}
					}
				}
			}
		} else if line.Type == "user" {
			for _, tr := range session.ParseToolResults(line.Message) {
				if tr.IsError {
					s.errors++
				}
			}
		}
	})

	if s.start.IsZero() || s.messages < 2 {
		return nil
	}

	// Duration = active work time (sum of inter-message gaps under the idle
	// threshold). Earlier versions used last-first span, which was misleading
	// for resumed sessions spanning many days.
	s.duration = activeSecs

	if len(s.contextSizes) >= 4 {
		q := len(s.contextSizes) / 4
		firstQ := avg(s.contextSizes[:q])
		lastQ := avg(s.contextSizes[len(s.contextSizes)-q:])
		if firstQ > 0 {
			s.contextGrowth = (lastQ/firstQ - 1) * 100
		}
	}
	if len(s.contextSizes) > 0 {
		s.avgContext = avg(s.contextSizes)
		s.peakContext = maxSlice(s.contextSizes)
	}

	s.filesTouched = len(filesTouched)
	s.netLines = s.linesWritten + s.linesAdded - s.linesRemoved
	return s
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func maxSlice(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func linesPerMsg(sessions []*sessData) float64 {
	totalLines, totalMsgs := 0, 0
	for _, s := range sessions {
		totalLines += s.netLines
		totalMsgs += s.messages
	}
	if totalMsgs == 0 {
		return 0
	}
	return float64(totalLines) / float64(totalMsgs)
}

func errorRate(errors, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(errors) / float64(total) * 100
}
