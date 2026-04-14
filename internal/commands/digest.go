package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Andrevops/claude-stats/internal/config"
	"github.com/Andrevops/claude-stats/internal/dates"
	"github.com/Andrevops/claude-stats/internal/format"
	"github.com/Andrevops/claude-stats/internal/projects"
	"github.com/Andrevops/claude-stats/internal/session"
)

var jiraPattern *regexp.Regexp
var branchPatterns []*regexp.Regexp
var commitPattern = regexp.MustCompile(`-m\s+["'](.+?)["']`)
var mrTitlePattern = regexp.MustCompile(`--title\s+["'](.+?)["']`)

func init() {
	// Build jira pattern from prefixes
	jiraPattern = regexp.MustCompile(
		`\b(` + strings.Join(config.JiraPrefixes, "|") + `)-(\d+)\b`,
	)
	branchPatterns = []*regexp.Regexp{
		regexp.MustCompile(`checkout\s+(?:-[bB]\s+)?([a-zA-Z0-9/_.-]+(?:DX|feat|fix|chore)[a-zA-Z0-9/_.-]*)`),
		regexp.MustCompile(`push\s+(?:-u\s+)?origin\s+([a-zA-Z0-9/_.-]+(?:DX|feat|fix|chore)[a-zA-Z0-9/_.-]*)`),
		regexp.MustCompile(`--source-branch[= ]([a-zA-Z0-9/_.-]+)`),
	}
}

type digestProjData struct {
	messages               int
	firstTS, lastTS        time.Time
	jiraIDs                map[string]bool
	branches               map[string]bool
	filesWritten           []fileOp
	filesEdited            []fileOp
	commits, mrCreates     []string
	mrUpdates              []string
	linesWritten, linesAdded, linesRemoved int
	deploys, awsCmds, acliCmds []string
}

type fileOp struct {
	path  string
	lines int
}

func Digest(args []string) {
	targetDates, label, useAI := dates.ParseDigestArgs(args)
	if label == "" {
		return
	}
	files := session.Find(targetDates, true)
	if len(files) == 0 {
		fmt.Printf("\n  No sessions found for %s\n", label)
		return
	}

	data := collectDigestData(files)
	if data.totalMessages == 0 {
		fmt.Printf("\n  No activity found for %s\n", label)
		return
	}

	printDigest(data, label, targetDates)
	if useAI {
		context := buildAIContext(data, label)
		runAISummary(context, label)
	}
}

type digestData struct {
	projData     map[string]*digestProjData
	allJira      map[string]bool
	allBranches  map[string]bool
	allCommits   []struct{ project, msg string }
	allMRs       []struct{ action, project, title string }
	totalMessages int
}

func collectDigestData(files []string) *digestData {
	d := &digestData{
		projData:    map[string]*digestProjData{},
		allJira:     map[string]bool{},
		allBranches: map[string]bool{},
	}

	for _, f := range files {
		projName := projects.ExtractProject(f)
		if _, ok := d.projData[projName]; !ok {
			d.projData[projName] = &digestProjData{
				jiraIDs:  map[string]bool{},
				branches: map[string]bool{},
			}
		}
		p := d.projData[projName]

		session.ScanLines(f, func(line session.LogLine) {
			localDT, ok := dates.ParseTS(line.Timestamp)
			msgType := line.Type

			if msgType == "human" || msgType == "assistant" || msgType == "user" {
				p.messages++
				d.totalMessages++
				if ok {
					if p.firstTS.IsZero() {
						p.firstTS = localDT
					}
					p.lastTS = localDT
				}
			}

			if msgType != "assistant" {
				return
			}
			msg, parsed := session.ParseAssistantMsg(line.Message)
			if !parsed {
				return
			}

			for _, b := range msg.Content {
				if b.Type != "tool_use" {
					continue
				}
				switch b.Name {
				case "Write":
					wi := session.ParseWriteInput(b.Input)
					lines := session.CountLines(wi.Content)
					short := projects.ShortenPath(wi.FilePath)
					p.filesWritten = append(p.filesWritten, fileOp{short, lines})
					p.linesWritten += lines
					for _, m := range jiraPattern.FindAllStringSubmatch(wi.FilePath, -1) {
						jid := m[1] + "-" + m[2]
						p.jiraIDs[jid] = true
						d.allJira[jid] = true
					}

				case "Edit":
					ei := session.ParseEditInput(b.Input)
					short := projects.ShortenPath(ei.FilePath)
					delta := session.CountLines(ei.NewString) - session.CountLines(ei.OldString)
					p.filesEdited = append(p.filesEdited, fileOp{short, delta})
					p.linesAdded += session.CountLines(ei.NewString)
					p.linesRemoved += session.CountLines(ei.OldString)

				case "Bash":
					bi := session.ParseBashInput(b.Input)
					cmd := bi.Command
					for _, m := range jiraPattern.FindAllStringSubmatch(cmd, -1) {
						jid := m[1] + "-" + m[2]
						p.jiraIDs[jid] = true
						d.allJira[jid] = true
					}
					for _, bp := range branchPatterns {
						for _, m := range bp.FindAllStringSubmatch(cmd, -1) {
							p.branches[m[1]] = true
							d.allBranches[m[1]] = true
						}
					}
					if strings.Contains(cmd, "git commit") {
						if m := commitPattern.FindStringSubmatch(cmd); m != nil {
							msg := m[1]
							if len(msg) > 80 {
								msg = msg[:80]
							}
							p.commits = append(p.commits, msg)
							d.allCommits = append(d.allCommits, struct{ project, msg string }{projName, msg})
						}
					}
					if strings.Contains(cmd, "mr create") {
						if m := mrTitlePattern.FindStringSubmatch(cmd); m != nil {
							title := m[1]
							if len(title) > 80 {
								title = title[:80]
							}
							p.mrCreates = append(p.mrCreates, title)
							d.allMRs = append(d.allMRs, struct{ action, project, title string }{"created", projName, title})
						}
					}
					if strings.Contains(cmd, "mr update") {
						s := cmd
						if len(s) > 60 {
							s = s[:60]
						}
						p.mrUpdates = append(p.mrUpdates, s)
					}
					if regexp.MustCompile(`make\s+deploy|deploy\.sh|cloudformation\s+(?:create|update|deploy)`).MatchString(cmd) {
						parts := strings.Split(cmd, "\n")
						deploy := strings.Join(func() []string {
							var r []string
							for _, pt := range parts {
								pt = strings.TrimSpace(pt)
								if pt != "" {
									r = append(r, pt)
								}
							}
							return r
						}(), " ; ")
						if len(deploy) > 60 {
							deploy = deploy[:60]
						}
						p.deploys = append(p.deploys, deploy)
					}
					if regexp.MustCompile(`aws\s+\S+\s+(create|update|delete|put|start|run|deploy)`).MatchString(cmd) {
						line := strings.Split(strings.TrimSpace(cmd), "\n")[0]
						if len(line) > 60 {
							line = line[:60]
						}
						p.awsCmds = append(p.awsCmds, line)
					}
					if strings.Contains(cmd, "workitem transition") || strings.Contains(cmd, "workitem create") || strings.Contains(cmd, "workitem assign") {
						s := strings.TrimSpace(cmd)
						if len(s) > 60 {
							s = s[:60]
						}
						p.acliCmds = append(p.acliCmds, s)
					}
				}
			}
		})
	}
	return d
}

func printDigest(data *digestData, label string, targetDates []string) {
	allFiles := map[string]bool{}
	totalWritten, totalAdded, totalRemoved := 0, 0, 0
	for _, p := range data.projData {
		for _, f := range p.filesWritten {
			allFiles[f.path] = true
		}
		for _, f := range p.filesEdited {
			allFiles[f.path] = true
		}
		totalWritten += p.linesWritten
		totalAdded += p.linesAdded
		totalRemoved += p.linesRemoved
	}
	net := totalWritten + totalAdded - totalRemoved
	tzLabel := dates.TZLabel()

	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════════════════════════════╗")
	fmt.Printf("  ║  📓  CLAUDE CODE — %-47s ║\n", truncate(label, 47))
	fmt.Println("  ╚══════════════════════════════════════════════════════════════════╝")
	fmt.Printf(`
  Projects: %-10d Jira tickets: %-10d Messages: %d
  Files:    %-10d Branches:     %-10d Net lines: %s`+"\n",
		len(data.projData), len(data.allJira), data.totalMessages,
		len(allFiles), len(data.allBranches), format.Fmt(net))

	if len(data.allJira) > 0 {
		format.Header("🎫  JIRA TICKETS", "─")
		fmt.Println()
		var jiraList []string
		for jid := range data.allJira {
			jiraList = append(jiraList, jid)
		}
		sort.Strings(jiraList)
		for _, jid := range jiraList {
			var projsForTicket []string
			for name, p := range data.projData {
				if p.jiraIDs[jid] {
					projsForTicket = append(projsForTicket, truncate(name, 30))
				}
			}
			fmt.Printf("  %-12s %s\n", jid, strings.Join(projsForTicket, ", "))
		}
	}

	format.Header("📁  ACTIVITY BY PROJECT", "─")
	type projEntry struct {
		name string
		p    *digestProjData
	}
	var projList []projEntry
	for name, p := range data.projData {
		projList = append(projList, projEntry{name, p})
	}
	sort.Slice(projList, func(i, j int) bool {
		return projList[i].p.messages > projList[j].p.messages
	})

	for _, e := range projList {
		p := e.p
		pNet := p.linesWritten + p.linesAdded - p.linesRemoved
		timeRange := ""
		if !p.firstTS.IsZero() && !p.lastTS.IsZero() {
			var s, end string
			if len(targetDates) == 1 {
				s = p.firstTS.Format("15:04")
				end = p.lastTS.Format("15:04")
				if p.firstTS.Day() != p.lastTS.Day() {
					s = p.firstTS.Format("01/02 15:04")
					end = p.lastTS.Format("01/02 15:04")
				}
			} else {
				s = p.firstTS.Format("01/02 15:04")
				end = p.lastTS.Format("01/02 15:04")
			}
			timeRange = fmt.Sprintf(" (%s → %s)", s, end)
		}

		fmt.Printf("\n  📂 %s%s\n", e.name, timeRange)
		fileCount := map[string]bool{}
		for _, f := range p.filesWritten {
			fileCount[f.path] = true
		}
		for _, f := range p.filesEdited {
			fileCount[f.path] = true
		}
		fmt.Printf("     %d messages | %+d lines | %d files\n",
			p.messages, pNet, len(fileCount))

		if len(p.jiraIDs) > 0 {
			var ids []string
			for id := range p.jiraIDs {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			fmt.Printf("     🎫 Tickets: %s\n", strings.Join(ids, ", "))
		}
		for b := range p.branches {
			fmt.Printf("     🌿 %s\n", b)
		}
		seen := map[string]bool{}
		for _, c := range p.commits {
			if !seen[c] {
				seen[c] = true
				fmt.Printf("     💾 %s\n", c)
			}
		}
		for _, mr := range p.mrCreates {
			fmt.Printf("     🔀 MR: %s\n", mr)
		}
		seenDeploy := map[string]bool{}
		count := 0
		for _, d := range p.deploys {
			if !seenDeploy[d] && count < 3 {
				seenDeploy[d] = true
				fmt.Printf("     🚀 %s\n", d)
				count++
			}
		}
		seenAWS := map[string]bool{}
		count = 0
		for _, a := range p.awsCmds {
			if !seenAWS[a] && count < 3 {
				seenAWS[a] = true
				fmt.Printf("     ☁️  %s\n", a)
				count++
			}
		}

		// Top files
		fileOps := map[string]struct{ written, edits int }{}
		for _, f := range p.filesWritten {
			v := fileOps[f.path]
			v.written += f.lines
			fileOps[f.path] = v
		}
		for _, f := range p.filesEdited {
			v := fileOps[f.path]
			v.edits++
			fileOps[f.path] = v
		}
		type foEntry struct {
			path    string
			written int
			edits   int
		}
		var foList []foEntry
		for path, ops := range fileOps {
			foList = append(foList, foEntry{path, ops.written, ops.edits})
		}
		sort.Slice(foList, func(i, j int) bool {
			ti := foList[i].written + foList[i].edits
			tj := foList[j].written + foList[j].edits
			return ti > tj
		})
		if len(foList) > 5 {
			foList = foList[:5]
		}
		if len(foList) > 0 {
			fmt.Println("     📄 Key files:")
			for _, fe := range foList {
				var parts []string
				if fe.written > 0 {
					parts = append(parts, fmt.Sprintf("wrote %dL", fe.written))
				}
				if fe.edits > 0 {
					parts = append(parts, fmt.Sprintf("%d edits", fe.edits))
				}
				display := fe.path
				if len(display) > 45 {
					display = "..." + display[len(display)-42:]
				}
				fmt.Printf("        %s  (%s)\n", display, strings.Join(parts, ", "))
			}
		}
	}

	if len(data.allMRs) > 0 {
		format.Header("🔀  MERGE REQUESTS", "─")
		fmt.Println()
		for _, mr := range data.allMRs {
			fmt.Printf("  %-10s [%s]\n", strings.ToUpper(mr.action), truncate(mr.project, 25))
			fmt.Printf("             %s\n", mr.title)
		}
	}

	if len(data.allCommits) > 0 {
		format.Header("💾  COMMITS", "─")
		fmt.Println()
		seen := map[string]bool{}
		for _, c := range data.allCommits {
			if !seen[c.msg] {
				seen[c.msg] = true
				fmt.Printf("  [%s] %s\n", truncate(c.project, 20), c.msg)
			}
		}
	}

	format.Header("📄  FILES CHANGED", "─")
	extCounts := map[string][2]int{} // [created, edited]
	for _, p := range data.projData {
		for _, f := range p.filesWritten {
			v := extCounts[projects.GetExt(f.path)]
			v[0]++
			extCounts[projects.GetExt(f.path)] = v
		}
		for _, f := range p.filesEdited {
			v := extCounts[projects.GetExt(f.path)]
			v[1]++
			extCounts[projects.GetExt(f.path)] = v
		}
	}
	type extEntry struct {
		ext     string
		created int
		edited  int
	}
	var extList []extEntry
	for ext, counts := range extCounts {
		extList = append(extList, extEntry{ext, counts[0], counts[1]})
	}
	sort.Slice(extList, func(i, j int) bool {
		ti := extList[i].created + extList[i].edited
		tj := extList[j].created + extList[j].edited
		return ti > tj
	})
	if len(extList) > 10 {
		extList = extList[:10]
	}

	fmt.Printf("\n  %-12s %8s %8s\n", "Extension", "Created", "Edited")
	fmt.Printf("  %s %s %s\n", repeat("─", 12), repeat("─", 8), repeat("─", 8))
	for _, e := range extList {
		fmt.Printf("  %-12s %8d %8d\n", e.ext, e.created, e.edited)
	}

	// ── Timeline (single-day only)
	if len(targetDates) == 1 {
		format.Header(fmt.Sprintf("⏰  TIMELINE (%s)", tzLabel), "─")
		fmt.Println()
		type event struct {
			ts   time.Time
			desc string
		}
		var events []event
		for projName, p := range data.projData {
			if !p.firstTS.IsZero() {
				events = append(events, event{p.firstTS, fmt.Sprintf("Started working on %s", projName)})
			}
			for _, mr := range p.mrCreates {
				ts := p.lastTS
				if ts.IsZero() {
					ts = p.firstTS
				}
				events = append(events, event{ts, fmt.Sprintf("Created MR: %s", truncate(mr, 50))})
			}
			seen := map[string]bool{}
			count := 0
			for _, c := range p.commits {
				if !seen[c] && count < 3 {
					seen[c] = true
					ts := p.lastTS
					if ts.IsZero() {
						ts = p.firstTS
					}
					events = append(events, event{ts, fmt.Sprintf("Committed: %s", truncate(c, 50))})
					count++
				}
			}
		}
		sort.Slice(events, func(i, j int) bool { return events[i].ts.Before(events[j].ts) })
		for _, e := range events {
			fmt.Printf("  %s  %s\n", e.ts.Format("15:04"), e.desc)
		}
	}

	fmt.Println()
}

func buildAIContext(data *digestData, label string) string {
	var lines []string
	allFiles := map[string]bool{}
	totalWritten, totalAdded, totalRemoved := 0, 0, 0
	for _, p := range data.projData {
		for _, f := range p.filesWritten {
			allFiles[f.path] = true
		}
		for _, f := range p.filesEdited {
			allFiles[f.path] = true
		}
		totalWritten += p.linesWritten
		totalAdded += p.linesAdded
		totalRemoved += p.linesRemoved
	}
	net := totalWritten + totalAdded - totalRemoved

	lines = append(lines,
		fmt.Sprintf("Period: %s", label),
		fmt.Sprintf("Projects: %d, Jira tickets: %d, Messages: %d",
			len(data.projData), len(data.allJira), data.totalMessages),
		fmt.Sprintf("Files: %d, Net lines: %d (wrote %d, added %d, removed %d)",
			len(allFiles), net, totalWritten, totalAdded, totalRemoved),
		"",
		"JIRA TICKETS:",
	)
	var jiraList []string
	for jid := range data.allJira {
		jiraList = append(jiraList, jid)
	}
	sort.Strings(jiraList)
	for _, jid := range jiraList {
		var projs []string
		for name, p := range data.projData {
			if p.jiraIDs[jid] {
				projs = append(projs, name)
			}
		}
		lines = append(lines, fmt.Sprintf("  %s: %s", jid, strings.Join(projs, ", ")))
	}
	lines = append(lines, "", "PROJECTS:")

	type projE struct {
		name string
		p    *digestProjData
	}
	var projList []projE
	for name, p := range data.projData {
		projList = append(projList, projE{name, p})
	}
	sort.Slice(projList, func(i, j int) bool {
		return projList[i].p.messages > projList[j].p.messages
	})

	for _, e := range projList {
		p := e.p
		pNet := p.linesWritten + p.linesAdded - p.linesRemoved
		timeInfo := ""
		if !p.firstTS.IsZero() {
			timeInfo = fmt.Sprintf(" (%s → %s)",
				p.firstTS.Format("01/02 15:04"), p.lastTS.Format("01/02 15:04"))
		}
		lines = append(lines,
			fmt.Sprintf("\n  %s%s", e.name, timeInfo),
			fmt.Sprintf("    %d messages, %+d lines", p.messages, pNet),
		)
		var ids []string
		for id := range p.jiraIDs {
			ids = append(ids, id)
		}
		if len(ids) > 0 {
			sort.Strings(ids)
			lines = append(lines, fmt.Sprintf("    Tickets: %s", strings.Join(ids, ", ")))
		}
		for b := range p.branches {
			lines = append(lines, fmt.Sprintf("    Branch: %s", b))
		}
		seen := map[string]bool{}
		count := 0
		for _, c := range p.commits {
			if !seen[c] && count < 5 {
				seen[c] = true
				lines = append(lines, fmt.Sprintf("    Commit: %s", c))
				count++
			}
		}
		for _, mr := range p.mrCreates {
			lines = append(lines, fmt.Sprintf("    MR created: %s", mr))
		}
	}

	for _, mr := range data.allMRs {
		lines = append(lines, fmt.Sprintf("\nMERGE REQUESTS:\n  [%s] %s: %s",
			truncate(mr.project, 30), mr.action, mr.title))
	}

	return strings.Join(lines, "\n")
}

func runAISummary(data, label string) {
	format.Header("🤖  AI ANALYSIS", "─")
	fmt.Println()
	fmt.Print("  Generating AI summary")
	stop := make(chan struct{})
	go func() {
		frames := []string{".", "..", "..."}
		i := 0
		for {
			select {
			case <-stop:
				return
			case <-time.After(500 * time.Millisecond):
				fmt.Printf("\r  Generating AI summary%-3s", frames[i%len(frames)])
				i++
			}
		}
	}()

	period := "today"
	switch {
	case strings.Contains(label, "Weekly"):
		period = "this week"
	case strings.Contains(label, "Monthly"):
		period = "this month"
	case strings.Contains(label, "Yesterday"):
		period = "yesterday"
	}

	prompt := fmt.Sprintf(`You are reviewing a developer's work log. Write a tight summary.

Rules:
- Use actual ticket IDs, project names, branch names from the data. No placeholders.
- Second person ("you"). Direct. No filler.
- No markdown headers. Use plain text with bullet dashes.

Format (exactly this, nothing more):

SUMMARY: 1-2 sentences on main focus %s.

DONE:
- (bullet per concrete deliverable: MR created, repo initialized, policy added, etc.)

STANDUP: 2-3 sentence ready-to-paste standup for Slack.

Keep under 150 words total.

DATA:
%s`, period, data)

	env := os.Environ()
	filtered := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			filtered = append(filtered, e)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", "opus")
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = filtered
	out, err := cmd.Output()
	close(stop)
	fmt.Print("\r\033[K") // clear the spinner line
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Println("  AI summary timed out after 60s. Try again or skip with option 1.")
		fmt.Println()
		return
	}
	if err != nil {
		fmt.Printf("  Could not generate AI summary: %v\n", err)
		fmt.Println()
		return
	}

	text := strings.TrimSpace(string(out))
	if text == "" {
		fmt.Println("  No output from AI.")
		fmt.Println()
		return
	}

	// Clear "Generating..." line
	fmt.Print("\033[A\033[2K")

	// Parse sections
	sections := parseAISections(text)
	if summary, ok := sections["summary"]; ok {
		fmt.Printf("  📋 %s\n\n", summary)
	}
	if done, ok := sections["done"]; ok {
		fmt.Println("  ✅ Accomplishments:")
		for _, line := range strings.Split(done, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "-") {
				fmt.Printf("     %s\n", line)
			} else {
				fmt.Printf("     - %s\n", line)
			}
		}
		fmt.Println()
	}
	if standup, ok := sections["standup"]; ok {
		fmt.Println("  💬 Standup:")
		fmt.Printf("  ┌%s┐\n", repeat("─", 66))
		words := strings.Fields(standup)
		line := ""
		for _, word := range words {
			if len(line)+len(word)+1 > 64 {
				fmt.Printf("  │ %-64s │\n", line)
				line = word
			} else {
				if line == "" {
					line = word
				} else {
					line += " " + word
				}
			}
		}
		if line != "" {
			fmt.Printf("  │ %-64s │\n", line)
		}
		fmt.Printf("  └%s┘\n", repeat("─", 66))
	}
	fmt.Println()
}

func parseAISections(text string) map[string]string {
	sections := map[string]string{}
	currentKey := ""
	var currentLines []string

	flush := func() {
		if currentKey != "" {
			sections[currentKey] = strings.TrimSpace(strings.Join(currentLines, "\n"))
		}
	}

	for _, line := range strings.Split(text, "\n") {
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(upper, "SUMMARY:"):
			flush()
			currentKey = "summary"
			currentLines = []string{strings.TrimSpace(line[strings.Index(line, ":")+1:])}
		case upper == "DONE:" || strings.HasPrefix(upper, "DONE:"):
			flush()
			currentKey = "done"
			rest := strings.TrimSpace(line[strings.Index(line, ":")+1:])
			if rest != "" {
				currentLines = []string{rest}
			} else {
				currentLines = nil
			}
		case strings.HasPrefix(upper, "STANDUP:"):
			flush()
			currentKey = "standup"
			currentLines = []string{strings.TrimSpace(line[strings.Index(line, ":")+1:])}
		default:
			currentLines = append(currentLines, line)
		}
	}
	flush()
	return sections
}
