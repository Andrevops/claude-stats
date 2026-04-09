package config

import (
	"os"
	"path/filepath"
)

// Paths
var (
	ClaudeDir   = filepath.Join(homeDir(), ".claude")
	ProjectsDir = filepath.Join(homeDir(), ".claude", "projects")
	SettingsFile = filepath.Join(homeDir(), ".claude", "settings.json")
)

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// Pricing per 1M tokens
type ModelPricing struct {
	Input       float64
	Output      float64
	CacheRead   float64
	CacheCreate float64
}

var Pricing = map[string]ModelPricing{
	// Opus 4.6 / 4.5 — $5/$25 per MTok
	"claude-opus-4-6":          {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheCreate: 6.25},
	"claude-opus-4-5-20251101": {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheCreate: 6.25},
	// Opus 4.1 / 4 — $15/$75 per MTok
	"claude-opus-4-1-20250414": {Input: 15.00, Output: 75.00, CacheRead: 1.50, CacheCreate: 18.75},
	"claude-opus-4-20250414":   {Input: 15.00, Output: 75.00, CacheRead: 1.50, CacheCreate: 18.75},
	// Sonnet 4.6 / 4.5 / 4 — $3/$15 per MTok
	"claude-sonnet-4-6":          {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheCreate: 3.75},
	"claude-sonnet-4-5-20241022": {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheCreate: 3.75},
	"claude-sonnet-4-20250514":   {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheCreate: 3.75},
	// Haiku 4.5 — $1/$5 per MTok
	"claude-haiku-4-5-20251001": {Input: 1.00, Output: 5.00, CacheRead: 0.10, CacheCreate: 1.25},
	// Haiku 3.5 — $0.80/$4 per MTok
	"claude-haiku-3-5-20241022": {Input: 0.80, Output: 4.00, CacheRead: 0.08, CacheCreate: 1.00},
}

var DefaultPricing = ModelPricing{Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheCreate: 6.25}

func GetPricing(model string) ModelPricing {
	if p, ok := Pricing[model]; ok {
		return p
	}
	return DefaultPricing
}

// Tool classifications
var ReadTools = map[string]bool{
	"Read": true, "Glob": true, "Grep": true,
	"WebFetch": true, "WebSearch": true,
	"TaskList": true, "TaskGet": true,
}

var WriteTools = map[string]bool{
	"Edit": true, "Write": true, "NotebookEdit": true,
	"Bash": true, "TaskCreate": true, "TaskUpdate": true,
}

var AgentTools = map[string]bool{
	"Task": true, "SendMessage": true,
}

var AutoAllowedTools = map[string]bool{
	"Glob": true, "Grep": true, "WebSearch": true, "WebFetch": true,
	"Task": true, "TaskCreate": true, "TaskUpdate": true,
	"TaskList": true, "TaskGet": true, "TaskOutput": true,
	"SendMessage": true, "TeamCreate": true, "TeamDelete": true,
	"AskUserQuestion": true, "EnterPlanMode": true, "ExitPlanMode": true,
	"Skill": true, "NotebookEdit": true, "EnterWorktree": true, "TaskStop": true,
}

var DestructiveCmds = map[string]bool{
	"rm": true, "sudo": true, "kill": true, "pkill": true, "rmdir": true,
}

// Display
var Days = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
var Heat = []string{
	" \033[2m·\033[0m ",
	" \033[38;5;33m░\033[0m ",
	" \033[38;5;37m▒\033[0m ",
	" \033[38;5;214m▓\033[0m ",
	" \033[38;5;196m█\033[0m ",
}

// JiraPattern prefix list (checked via strings.HasPrefix in practice)
var JiraPrefixes = []string{
	"DX", "BACK", "FRNT", "ANG", "CACG", "CORE",
	"INF", "DATA", "RES", "CSD", "NJP", "SFR", "DPI",
}
