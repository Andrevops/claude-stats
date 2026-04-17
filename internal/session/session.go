package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/Andrevops/claude-stats/internal/config"
	"github.com/Andrevops/claude-stats/internal/dates"
)

// Find returns JSONL session file paths matching targetDates (nil = all).
func Find(targetDates []string, skipSubagents bool) []string {
	dateSet := dates.DateSet(targetDates)
	var files []string

	_ = filepath.WalkDir(config.ProjectsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if skipSubagents {
			if containsSubagents(path) {
				return nil
			}
		}
		if targetDates != nil {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			mdate := info.ModTime().Format("2006-01-02")
			if !dateSet[mdate] {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})

	sort.Slice(files, func(i, j int) bool {
		si, _ := os.Stat(files[i])
		sj, _ := os.Stat(files[j])
		if si == nil || sj == nil {
			return files[i] < files[j]
		}
		return si.ModTime().Before(sj.ModTime())
	})
	return files
}

func containsSubagents(path string) bool {
	for _, part := range filepath.SplitList(path) {
		if part == "subagents" {
			return true
		}
	}
	// filepath.SplitList splits PATH env var, not path components — use manual check
	clean := filepath.ToSlash(path)
	for i := 0; i < len(clean)-9; i++ {
		if clean[i:i+9] == "subagents" {
			return true
		}
	}
	return false
}

// ── Shared JSON structs ────────────────────────────────────────────────────

// LogLine is a single line from a JSONL session file.
type LogLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type AssistantMsg struct {
	Model   string         `json:"model"`
	Usage   *Usage         `json:"usage"`
	Content []ContentBlock `json:"content"`
}

type UserMsg struct {
	Content json.RawMessage `json:"content"`
}

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type ContentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

type ToolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
}

// ToolInput helpers
type BashInput struct {
	Command string `json:"command"`
}

type WriteInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type EditInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

type ReadInput struct {
	FilePath string `json:"file_path"`
}

// ScanLines opens a file and calls fn for each parsed LogLine.
func ScanLines(path string, fn func(LogLine)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(b) == 0 {
			continue
		}
		var line LogLine
		if err := json.Unmarshal(b, &line); err != nil {
			continue
		}
		fn(line)
	}
	return scanner.Err()
}

// ParseAssistantMsg decodes an assistant message from raw JSON.
func ParseAssistantMsg(raw json.RawMessage) (AssistantMsg, bool) {
	var msg AssistantMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return msg, false
	}
	return msg, true
}

// ParseToolResults decodes tool_result blocks from a user message.
// The raw JSON is the full message object: {"content": [...]}.
func ParseToolResults(raw json.RawMessage) []ToolResult {
	// First try as {"content": [...]} (user message envelope)
	var envelope struct {
		Content []ContentBlock `json:"content"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil
	}
	blocks := envelope.Content
	var results []ToolResult
	for _, b := range blocks {
		if b.Type == "tool_result" {
			results = append(results, ToolResult{
				Type: b.Type, ToolUseID: b.ToolUseID, IsError: b.IsError,
			})
		}
	}
	return results
}

// IsToolResultOnly returns true when a user message's content consists
// solely of tool_result blocks (i.e., it is not a real user turn).
// Works on the raw message envelope — `{"content": "..."}` or `{"content": [...]}`.
func IsToolResultOnly(raw json.RawMessage) bool {
	var env struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return false
	}
	if len(env.Content) == 0 {
		return false
	}
	// String content is always a real user turn (plain text).
	if env.Content[0] == '"' {
		return false
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(env.Content, &blocks); err != nil {
		return false
	}
	if len(blocks) == 0 {
		return false
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			return false
		}
	}
	return true
}

// IsUserTurn returns true for a user line that represents an actual user
// input (not a tool_result response).
func IsUserTurn(line LogLine) bool {
	if line.Type != "user" {
		return false
	}
	return !IsToolResultOnly(line.Message)
}

// IsAssistant returns true for assistant-authored lines.
func IsAssistant(line LogLine) bool {
	return line.Type == "assistant"
}

// IsConversation returns true for lines that are part of the visible
// conversation stream — user turns or assistant messages. Tool-result
// envelopes and bookkeeping lines (system, attachment, ...) are excluded.
func IsConversation(line LogLine) bool {
	return IsAssistant(line) || IsUserTurn(line)
}

func ParseBashInput(raw json.RawMessage) BashInput {
	var v BashInput
	_ = json.Unmarshal(raw, &v)
	return v
}

func ParseWriteInput(raw json.RawMessage) WriteInput {
	var v WriteInput
	_ = json.Unmarshal(raw, &v)
	return v
}

func ParseEditInput(raw json.RawMessage) EditInput {
	var v EditInput
	_ = json.Unmarshal(raw, &v)
	return v
}

func ParseReadInput(raw json.RawMessage) ReadInput {
	var v ReadInput
	_ = json.Unmarshal(raw, &v)
	return v
}

func CountLines(s string) int {
	if s == "" {
		return 0
	}
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
