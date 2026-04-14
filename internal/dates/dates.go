package dates

import (
	"fmt"
	"strings"
	"time"
)

// ParseArgs converts CLI date args into (targetDates, label).
// Returns nil targetDates for --all.
func ParseArgs(args []string) ([]string, string) {
	today := time.Now()
	if len(args) == 0 {
		d := today.Format("2006-01-02")
		return []string{d}, fmt.Sprintf("Today (%s)", d)
	}
	arg := args[0]
	switch arg {
	case "--yesterday":
		d := today.AddDate(0, 0, -1).Format("2006-01-02")
		return []string{d}, fmt.Sprintf("Yesterday (%s)", d)
	case "--week":
		dates := make([]string, 7)
		for i := range dates {
			dates[i] = today.AddDate(0, 0, -i).Format("2006-01-02")
		}
		return dates, fmt.Sprintf("Last 7 days (%s → %s)", dates[6], dates[0])
	case "--month":
		dates := make([]string, 30)
		for i := range dates {
			dates[i] = today.AddDate(0, 0, -i).Format("2006-01-02")
		}
		return dates, fmt.Sprintf("Last 30 days (%s → %s)", dates[29], dates[0])
	case "--all":
		return nil, "All time"
	case "--help", "-h":
		fmt.Println("Usage: claude-stats <command> [--yesterday|--week|--month|--all|YYYY-MM-DD]")
		return nil, ""
	default:
		if _, err := time.Parse("2006-01-02", arg); err == nil {
			return []string{arg}, arg
		}
		fmt.Printf("Invalid date: %s. Use --yesterday, --week, --month, --all, or YYYY-MM-DD\n", arg)
		return nil, ""
	}
}

// ParseReportArgs is like ParseArgs but defaults to today (report-specific).
func ParseReportArgs(args []string) ([]string, string) {
	today := time.Now()
	if len(args) == 0 {
		d := today.Format("2006-01-02")
		return []string{d}, fmt.Sprintf("Today's Report (%s)", d)
	}
	arg := args[0]
	switch arg {
	case "--week":
		dates := make([]string, 7)
		for i := range dates {
			dates[i] = today.AddDate(0, 0, -i).Format("2006-01-02")
		}
		return dates, fmt.Sprintf("Weekly Report (%s → %s)", dates[6], dates[0])
	case "--yesterday":
		d := today.AddDate(0, 0, -1).Format("2006-01-02")
		return []string{d}, fmt.Sprintf("Yesterday Report (%s)", d)
	case "--month":
		dates := make([]string, 30)
		for i := range dates {
			dates[i] = today.AddDate(0, 0, -i).Format("2006-01-02")
		}
		return dates, fmt.Sprintf("Monthly Report (%s → %s)", dates[29], dates[0])
	case "--all":
		return nil, "All-Time Report"
	default:
		if target, err := time.Parse("2006-01-02", arg); err == nil {
			days := int(today.Sub(target).Hours()/24) + 1
			dates := make([]string, days)
			for i := range dates {
				dates[i] = today.AddDate(0, 0, -i).Format("2006-01-02")
			}
			return dates, fmt.Sprintf("Report (%s → %s)", arg, today.Format("2006-01-02"))
		}
		fmt.Printf("Invalid: %s\n", arg)
		return nil, ""
	}
}

// ParseDigestArgs parses date args for the digest command, also handling --ai flag.
func ParseDigestArgs(args []string) ([]string, string, bool) {
	useAI := false
	filtered := args[:0]
	for _, a := range args {
		if a == "--ai" {
			useAI = true
		} else {
			filtered = append(filtered, a)
		}
	}
	dates, label := ParseArgs(filtered)
	// Relabel for digest
	if len(filtered) == 0 {
		d := time.Now().Format("2006-01-02")
		label = fmt.Sprintf("Daily Digest — %s", d)
	} else {
		switch filtered[0] {
		case "--yesterday":
			d := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
			label = fmt.Sprintf("Daily Digest — %s", d)
		case "--week":
			label = strings.Replace(label, "Last 7 days", "Weekly Digest", 1)
		case "--month":
			label = strings.Replace(label, "Last 30 days", "Monthly Digest", 1)
		case "--all":
			label = "Full History Digest"
		}
	}
	return dates, label, useAI
}

// ParseTS parses an ISO timestamp string (UTC) and returns local time.
func ParseTS(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	ts = strings.ReplaceAll(ts, "Z", "+00:00")
	formats := []string{
		"2006-01-02T15:04:05.999999-07:00",
		"2006-01-02T15:04:05.999999+07:00",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05+07:00",
		"2006-01-02T15:04:05.999999Z07:00",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, ts); err == nil {
			return t.Local(), true
		}
	}
	return time.Time{}, false
}

// TZLabel returns the local timezone label as a UTC offset string.
// Zone() abbreviations are unreliable on Windows (returns "CST" even during CDT),
// so we derive the label from the actual numeric offset instead.
func TZLabel() string {
	_, offset := time.Now().Zone()
	h := offset / 3600
	m := (offset % 3600) / 60
	if m < 0 {
		m = -m
	}
	if offset == 0 {
		return "UTC"
	}
	if m == 0 {
		return fmt.Sprintf("UTC%+d", h)
	}
	return fmt.Sprintf("UTC%+d:%02d", h, m)
}

// DateSet converts a date slice to a lookup map.
func DateSet(dates []string) map[string]bool {
	m := make(map[string]bool, len(dates))
	for _, d := range dates {
		m[d] = true
	}
	return m
}
