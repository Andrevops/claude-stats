package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/Andrevops/claude-stats/internal/commands"
	"github.com/Andrevops/claude-stats/internal/tui"
)

// version is set at build time via -ldflags.
var version = "dev"

type command struct {
	cmd, name, desc string
	fn              func([]string)
}

var allCommands = []command{
	{"tokens", "Token Usage", "Tokens, cost breakdown, per-project and per-model spending", commands.Tokens},
	{"tools", "Tool Analytics", "Tool call frequency, error rates, Bash subcommands, chains", commands.Tools},
	{"prompts", "Permission Prompts", "Commands requiring approval, allowlist suggestions", commands.Prompts},
	{"heatmap", "Activity Heatmap", "Activity by hour/day, calendar view, top sessions", commands.Heatmap},
	{"lines", "Lines of Code", "Lines written, edited, removed by extension, project, file", commands.Lines},
	{"sessions", "Session Health", "Context growth, duration, bloat detection, restart advice", commands.Sessions},
	{"efficiency", "Efficiency", "Lines/turn, wasted turns, productivity ratios per project", commands.Efficiency},
	{"report", "Weekly Report", "Executive summary combining all analytics into one view", commands.Report},
	{"digest", "Work Digest", "What you worked on: tickets, branches, MRs, commits, files", commands.Digest},
	{"trends", "Trends", "Week-over-week or month-over-month comparison with deltas", commands.Trends},
}

func interactiveMenu() {
	cmds := make([]tui.Command, len(allCommands))
	for i, c := range allCommands {
		cmds[i] = tui.Command{Cmd: c.cmd, Name: c.name, Desc: c.desc, Fn: c.fn}
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		sel := tui.Run(cmds)
		if sel.Quit {
			break
		}
		sel.Command.Fn(sel.Args)
		fmt.Print("\n  Press Enter to continue...")
		reader.ReadString('\n')
	}
}

func printHelp() {
	fmt.Println("Usage: claude-stats [command] [options]")
	fmt.Println()
	fmt.Println("Commands:")
	for _, c := range allCommands {
		fmt.Printf("  %-14s %s\n", c.cmd, c.desc)
	}
	fmt.Println()
	fmt.Println("Meta:")
	fmt.Println("  version        Show version")
	fmt.Println("  self-update    Update to latest version")
	fmt.Println("  help           Show this help")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --yesterday    Yesterday's data")
	fmt.Println("  --week         Last 7 days")
	fmt.Println("  --month        Last 30 days")
	fmt.Println("  --all          All time")
	fmt.Println("  YYYY-MM-DD     Specific date")
	fmt.Println()
	fmt.Println("Run without arguments for interactive menu.")
}

func selfUpdate() {
	fmt.Printf("Updating claude-stats (current: %s)...\n", version)

	const repo = "Andrevops/claude-stats"
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	// Get latest release tag
	resp, err := http.Get(apiURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to check for updates: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse release info: %v\n", err)
		os.Exit(1)
	}

	if release.TagName == version {
		fmt.Printf("already up to date (%s)\n", version)
		return
	}

	fmt.Printf("updating %s → %s\n\n", version, release.TagName)

	// Determine binary name
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	binaryName := fmt.Sprintf("claude-stats-%s-%s%s", goos, goarch, ext)
	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, release.TagName, binaryName)

	// Download to temp file
	fmt.Printf("downloading %s...\n", binaryName)
	dlResp, err := http.Get(downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "download failed: %v\n", err)
		os.Exit(1)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "download failed: HTTP %d\n", dlResp.StatusCode)
		os.Exit(1)
	}

	// Find where the current binary lives
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine binary path: %v\n", err)
		os.Exit(1)
	}

	// Create temp file in same directory as binary so Rename is same-filesystem
	exeDir := exe[:strings.LastIndex(exe, string(os.PathSeparator))+1]
	tmpFile, err := os.CreateTemp(exeDir, ".claude-stats-update-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot create temp file: %v\n", err)
		os.Exit(1)
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, dlResp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "download failed: %v\n", err)
		os.Exit(1)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "chmod failed: %v\n", err)
		os.Exit(1)
	}

	// Atomic replace
	if err := os.Rename(tmpPath, exe); err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "replace failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nupdated to %s\n", release.TagName)
}

func main() {
	// Disable ANSI on Windows if not in a capable terminal
	if runtime.GOOS == "windows" {
		// Colors work fine in Windows Terminal / Git Bash; keep them
	}

	if len(os.Args) < 2 {
		interactiveMenu()
		return
	}

	subcmd := os.Args[1]
	remaining := os.Args[2:]

	switch subcmd {
	case "help", "--help", "-h":
		printHelp()
	case "version", "--version", "-v":
		fmt.Printf("claude-stats %s\n", version)
	case "self-update", "update":
		selfUpdate()
	default:
		for _, c := range allCommands {
			if c.cmd == subcmd {
				c.fn(remaining)
				return
			}
		}
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", subcmd)
		var names []string
		for _, c := range allCommands {
			names = append(names, c.cmd)
		}
		fmt.Fprintf(os.Stderr, "Available: %s\n", strings.Join(names, ", "))
		os.Exit(1)
	}
}
