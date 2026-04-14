package commands

import (
	"fmt"
	"sort"
	"time"

	"github.com/Andrevops/claude-stats/internal/config"
	"github.com/Andrevops/claude-stats/internal/dates"
	"github.com/Andrevops/claude-stats/internal/format"
	"github.com/Andrevops/claude-stats/internal/projects"
	"github.com/Andrevops/claude-stats/internal/session"
)

type heatSession struct {
	project  string
	start    time.Time
	duration float64
	messages int
	tokens   int
	cost     float64
}

// dowLabel returns a fixed-width (12-char) right-padded label for heatmap grid rows.
// Single date: "Mon 04-07  ", multiple: "Mon        ", none: "Mon        "
func dowLabel(d int, dates []string) string {
	if len(dates) == 1 {
		return fmt.Sprintf("%-3s %s  ", config.Days[d], dates[0][5:])
	}
	return fmt.Sprintf("%-3s        ", config.Days[d])
}

// dowLabelLeft returns a left-aligned label for daily summary rows.
func dowLabelLeft(d int, dates []string) string {
	if len(dates) == 1 {
		return fmt.Sprintf("%s %s", config.Days[d], dates[0][5:])
	}
	if len(dates) > 1 {
		return fmt.Sprintf("%s ×%d", config.Days[d], len(dates))
	}
	return config.Days[d]
}

func heatChar(value, maxVal float64) string {
	if value == 0 {
		return config.Heat[0]
	}
	if maxVal == 0 {
		return config.Heat[0]
	}
	ratio := value / maxVal
	switch {
	case ratio < 0.2:
		return config.Heat[1]
	case ratio < 0.45:
		return config.Heat[2]
	case ratio < 0.75:
		return config.Heat[3]
	default:
		return config.Heat[4]
	}
}

func Heatmap(args []string) {
	targetDates, label := dates.ParseArgs(args)
	if label == "" {
		return
	}
	files := session.Find(targetDates, false)
	dateSet := dates.DateSet(targetDates)
	tzLabel := dates.TZLabel()

	if len(files) == 0 {
		fmt.Printf("\n  No sessions found for %s\n", label)
		return
	}

	// grid[dow][hour]
	gridMessages := [7][24]int{}
	gridTools := [7][24]int{}
	gridTokens := [7][24]int{}
	gridCost := [7][24]float64{}
	dailyMessages := map[string]int{}
	dailyTokens := map[string]int{}
	dailyCost := map[string]float64{}
	dailySessions := map[string]int{}
	hourlyMessages := [24]int{}
	hourlyTools := [24]int{}
	totalMessages, totalTools, totalOutputTokens := 0, 0, 0
	var sessionData []heatSession

	for _, f := range files {
		proj := projects.ExtractProject(f)
		var firstTS, lastTS time.Time
		sessMessages, sessTokens := 0, 0
		sessCost := 0.0

		session.ScanLines(f, func(line session.LogLine) {
			localDT, ok := dates.ParseTS(line.Timestamp)
			if ok {
				if firstTS.IsZero() {
					firstTS = localDT
				}
				lastTS = localDT
			}

			dow := int(localDT.Weekday()+6) % 7 // Mon=0
			hour := localDT.Hour()
			dateStr := localDT.Format("2006-01-02")

			if targetDates != nil && !dateSet[dateStr] {
				return
			}

			if (line.Type == "human" || line.Type == "assistant") && ok {
				gridMessages[dow][hour]++
				hourlyMessages[hour]++
				dailyMessages[dateStr]++
				totalMessages++
				sessMessages++
			}

			if line.Type != "assistant" {
				return
			}
			msg, parsed := session.ParseAssistantMsg(line.Message)
			if !parsed {
				return
			}
			for _, b := range msg.Content {
				if b.Type == "tool_use" && ok {
					gridTools[dow][hour]++
					hourlyTools[hour]++
					totalTools++
				}
			}
			if msg.Usage != nil && ok {
				out := msg.Usage.OutputTokens
				gridTokens[dow][hour] += out
				dailyTokens[dateStr] += out
				totalOutputTokens += out
				sessTokens += out

				pricing := config.GetPricing(msg.Model)
				cost := float64(msg.Usage.InputTokens)/1e6*pricing.Input +
					float64(msg.Usage.OutputTokens)/1e6*pricing.Output +
					float64(msg.Usage.CacheReadInputTokens)/1e6*pricing.CacheRead +
					float64(msg.Usage.CacheCreationInputTokens)/1e6*pricing.CacheCreate
				gridCost[dow][hour] += cost
				dailyCost[dateStr] += cost
				sessCost += cost
			}
		})

		if !firstTS.IsZero() && !lastTS.IsZero() && sessMessages > 0 {
			duration := lastTS.Sub(firstTS).Seconds()
			dateStr := firstTS.Format("2006-01-02")
			dailySessions[dateStr]++
			sessionData = append(sessionData, heatSession{
				project: proj, start: firstTS, duration: duration,
				messages: sessMessages, tokens: sessTokens, cost: sessCost,
			})
		}
	}

	if totalMessages == 0 {
		fmt.Printf("\n  No activity found for %s\n", label)
		return
	}

	maxMessages := 0.0
	maxCost := 0.0
	for d := 0; d < 7; d++ {
		for h := 0; h < 24; h++ {
			if float64(gridMessages[d][h]) > maxMessages {
				maxMessages = float64(gridMessages[d][h])
			}
			if gridCost[d][h] > maxCost {
				maxCost = gridCost[d][h]
			}
		}
	}

	// Build DOW → date mapping for labels (from target dates, not session data)
	dowDates := [7][]string{}
	if targetDates != nil {
		for _, dateStr := range targetDates {
			dt, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue
			}
			dow := int(dt.Weekday()+6) % 7
			dowDates[dow] = append(dowDates[dow], dateStr)
		}
	} else {
		for dateStr := range dailyMessages {
			dt, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue
			}
			dow := int(dt.Weekday()+6) % 7
			dowDates[dow] = append(dowDates[dow], dateStr)
		}
	}
	for d := 0; d < 7; d++ {
		sort.Strings(dowDates[d])
	}

	// ── Output
	format.Header(fmt.Sprintf("📊  CLAUDE CODE ACTIVITY HEATMAP — %s", label), "═")
	fmt.Printf("\n  Timezone: %s\n", tzLabel)
	fmt.Printf("  Sessions: %d | Messages: %s | Tools: %s\n",
		len(sessionData), format.Fmt(totalMessages), format.Fmt(totalTools))

	// ── Messages Heatmap
	format.Header("💬  MESSAGES BY HOUR & DAY", "─")
	fmt.Println()
	fmt.Print("              ")
	for h := 0; h < 24; h++ {
		if h%3 == 0 {
			fmt.Printf("%2d ", h)
		} else {
			fmt.Print("   ")
		}
	}
	fmt.Println()
	for d := 0; d < 7; d++ {
		fmt.Printf("  %s", dowLabel(d, dowDates[d]))
		rowTotal := 0
		for h := 0; h < 24; h++ {
			fmt.Print(heatChar(float64(gridMessages[d][h]), maxMessages))
			rowTotal += gridMessages[d][h]
		}
		fmt.Printf("  %5d\n", rowTotal)
	}
	fmt.Println()
	fmt.Printf("              %snone %slow  %smed  %shigh %speak\n",
		config.Heat[0], config.Heat[1], config.Heat[2], config.Heat[3], config.Heat[4])

	// ── Cost Heatmap
	format.Header("💰  COST ($) BY HOUR & DAY", "─")
	fmt.Println()
	fmt.Print("              ")
	for h := 0; h < 24; h++ {
		if h%3 == 0 {
			fmt.Printf("%2d ", h)
		} else {
			fmt.Print("   ")
		}
	}
	fmt.Println()
	for d := 0; d < 7; d++ {
		fmt.Printf("  %s", dowLabel(d, dowDates[d]))
		rowCost := 0.0
		for h := 0; h < 24; h++ {
			fmt.Print(heatChar(gridCost[d][h], maxCost))
			rowCost += gridCost[d][h]
		}
		fmt.Printf(" $%6.1f\n", rowCost)
	}
	fmt.Println()
	fmt.Printf("              %s$0   %s<20%% %s<45%% %s<75%% %speak\n",
		config.Heat[0], config.Heat[1], config.Heat[2], config.Heat[3], config.Heat[4])

	// ── Hourly Summary
	format.Header(fmt.Sprintf("⏰  HOURLY ACTIVITY (%s)", tzLabel), "─")
	fmt.Println()
	maxHourly := 0
	peakHour := 0
	for h := 0; h < 24; h++ {
		if hourlyMessages[h] > maxHourly {
			maxHourly = hourlyMessages[h]
			peakHour = h
		}
	}
	for h := 0; h < 24; h++ {
		msgs := hourlyMessages[h]
		tools := hourlyTools[h]
		if msgs == 0 && tools == 0 {
			continue
		}
		barStr := format.Bar(float64(msgs), float64(maxHourly), 35)
		peak := ""
		if h == peakHour {
			peak = " ← PEAK"
		}
		fmt.Printf("  %02d:00  %s  %5d msgs  %4d tools%s\n",
			h, barStr, msgs, tools, peak)
	}

	// ── Daily Summary
	// Always compute DOW totals — used here and in the insights section below.
	dowMessages := [7]int{}
	dowCost := [7]float64{}
	for d := 0; d < 7; d++ {
		for h := 0; h < 24; h++ {
			dowMessages[d] += gridMessages[d][h]
			dowCost[d] += gridCost[d][h]
		}
	}

	format.Header("📅  DAILY SUMMARY", "─")
	fmt.Println()

	maxDOW := 0
	for d := 0; d < 7; d++ {
		if dowMessages[d] > maxDOW {
			maxDOW = dowMessages[d]
		}
	}

	// Sort rows chronologically by earliest date in each DOW slot so that
	// filtered views (e.g. --week spanning two calendar weeks) display days
	// in actual date order rather than Mon→Sun, preventing past days from
	// appearing to be future ones.
	type dowEntry struct {
		dow      int
		sortDate string
	}
	var dowOrder []dowEntry
	for d := 0; d < 7; d++ {
		sd := ""
		if len(dowDates[d]) > 0 {
			sd = dowDates[d][0]
		} else {
			sd = fmt.Sprintf("9999-%02d", d) // no data: keep Mon→Sun fallback order
		}
		dowOrder = append(dowOrder, dowEntry{d, sd})
	}
	sort.Slice(dowOrder, func(i, j int) bool { return dowOrder[i].sortDate < dowOrder[j].sortDate })

	fmt.Printf("  %-12s %9s %9s %s\n", "Day", "Messages", "Cost", "")
	fmt.Printf("  %s %s %s %s\n", repeat("─", 12), repeat("─", 9), repeat("─", 9), repeat("─", 35))
	for _, e := range dowOrder {
		d := e.dow
		msgs := dowMessages[d]
		cost := dowCost[d]
		barStr := format.Bar(float64(msgs), float64(maxDOW), 35)
		fmt.Printf("  %-12s %9d $%8.1f %s\n", dowLabelLeft(d, dowDates[d]), msgs, cost, barStr)
	}

	// ── Calendar View (all-time only — filtered views already show per-date data above)
	if targetDates == nil && len(dailyMessages) > 0 {
		format.Header("📆  DAILY ACTIVITY (last 30 days)", "─")
		fmt.Println()
		var sortedDates []string
		for d := range dailyMessages {
			sortedDates = append(sortedDates, d)
		}
		sort.Strings(sortedDates)

		maxDaily := 0
		for _, msgs := range dailyMessages {
			if msgs > maxDaily {
				maxDaily = msgs
			}
		}

		fmt.Printf("  %-12s %5s %6s %9s %8s  %s\n",
			"Date", "Sess", "Msgs", "Tokens", "Cost", "")
		fmt.Printf("  %s %s %s %s %s  %s\n",
			repeat("─", 12), repeat("─", 5), repeat("─", 6), repeat("─", 9), repeat("─", 8), repeat("─", 25))

		start := 0
		if len(sortedDates) > 30 {
			start = len(sortedDates) - 30
		}
		for _, date := range sortedDates[start:] {
			msgs := dailyMessages[date]
			tokens := dailyTokens[date]
			cost := dailyCost[date]
			sess := dailySessions[date]
			barStr := format.Bar(float64(msgs), float64(maxDaily), 25)
			dt, _ := time.Parse("2006-01-02", date)
			marker := ""
			if dt.Weekday() == time.Saturday || dt.Weekday() == time.Sunday {
				marker = " 🏖"
			}
			fmt.Printf("  %-12s %5d %6d %9s $%7.1f  %s%s\n",
				date, sess, msgs, format.Fmt(tokens), cost, barStr, marker)
		}
	}

	// ── Top Sessions
	if len(sessionData) > 0 {
		format.Header("🏆  TOP SESSIONS BY COST", "─")
		fmt.Println()
		sort.Slice(sessionData, func(i, j int) bool { return sessionData[i].cost > sessionData[j].cost })
		top := sessionData
		if len(top) > 10 {
			top = top[:10]
		}
		for _, s := range top {
			dur := format.FmtDuration(s.duration)
			fmt.Printf("  $%6.1f  %6s  %4d msgs  %s\n",
				s.cost, dur, s.messages, s.project)
		}
	}

	// ── Insights
	format.Header("💡  INSIGHTS", "─")
	var insights []string
	if peakHour >= 0 && hourlyMessages[peakHour] > 0 {
		insights = append(insights, fmt.Sprintf("Peak hour: %02d:00 %s (%d messages)",
			peakHour, tzLabel, hourlyMessages[peakHour]))
	}
	busiestDow := 0
	quietestDow := 0
	for d := 1; d < 7; d++ {
		if dowMessages[d] > dowMessages[busiestDow] {
			busiestDow = d
		}
		if dowMessages[d] < dowMessages[quietestDow] {
			quietestDow = d
		}
	}
	insights = append(insights, fmt.Sprintf("Busiest day: %s (%d msgs, $%.0f)",
		config.Days[busiestDow], dowMessages[busiestDow], dowCost[busiestDow]))
	if dowMessages[quietestDow] > 0 {
		insights = append(insights, fmt.Sprintf("Quietest day: %s (%d msgs)",
			config.Days[quietestDow], dowMessages[quietestDow]))
	}

	weekendMsgs := dowMessages[5] + dowMessages[6]
	weekdayMsgs := 0
	for d := 0; d < 5; d++ {
		weekdayMsgs += dowMessages[d]
	}
	if weekendMsgs > 0 {
		pct := float64(weekendMsgs) / float64(weekendMsgs+weekdayMsgs) * 100
		note := "good balance"
		if pct > 20 {
			note = "take a break!"
		}
		insights = append(insights, fmt.Sprintf("Weekend activity: %.1f%% of messages — %s", pct, note))
	} else {
		insights = append(insights, "No weekend activity — healthy work-life balance!")
	}

	if len(dailyCost) > 0 {
		maxDate := ""
		maxDateCost := 0.0
		for d, c := range dailyCost {
			if c > maxDateCost {
				maxDateCost = c
				maxDate = d
			}
		}
		insights = append(insights, fmt.Sprintf("Most expensive day: %s ($%.1f)", maxDate, maxDateCost))
	}

	if len(sessionData) > 0 {
		totalDur := 0.0
		for _, s := range sessionData {
			totalDur += s.duration
		}
		avgDur := totalDur / float64(len(sessionData))
		insights = append(insights, fmt.Sprintf("Avg session duration: %s", format.FmtDuration(avgDur)))
	}

	for i, ins := range insights {
		fmt.Printf("  %d. %s\n", i+1, ins)
	}
	fmt.Println()
}
