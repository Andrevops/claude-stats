package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type viewState int

const (
	viewMenu viewState = iota
	viewTimeframe
	viewAI
	viewCustomDate
)

// Command represents an analytics command exposed to the TUI.
type Command struct {
	Cmd, Name, Desc string
	Fn              func([]string)
}

// Selection is the result returned after the TUI exits.
type Selection struct {
	Command *Command
	Args    []string
	Quit    bool
}

type timeframeItem struct {
	label    string
	args     []string
	isCustom bool
}

var timeframes = []timeframeItem{
	{"Today", nil, false},
	{"Yesterday", []string{"--yesterday"}, false},
	{"Last 7 days", []string{"--week"}, false},
	{"Last 30 days", []string{"--month"}, false},
	{"All time", []string{"--all"}, false},
	{"Custom date", nil, true},
}

type model struct {
	state     viewState
	commands  []Command
	cursor    int
	tfCursor  int
	aiCursor  int
	selected  *Command
	args      []string
	selection Selection
	dateInput textinput.Model
	dateErr   string
	width     int
	height    int
}

func newModel(commands []Command) model {
	ti := textinput.New()
	ti.Placeholder = "YYYY-MM-DD"
	ti.CharLimit = 10
	ti.Width = 12

	return model{
		commands: commands,
		state:    viewMenu,
		dateInput: ti,
		width:    80,
		height:   24,
	}
}

// Run starts the TUI and returns the user's selection.
func Run(commands []Command) Selection {
	m := newModel(commands)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return Selection{Quit: true}
	}
	if fm, ok := finalModel.(model); ok {
		return fm.selection
	}
	return Selection{Quit: true}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.state == viewCustomDate {
		return m.updateCustomDate(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.selection = Selection{Quit: true}
			return m, tea.Quit
		case "up", "k":
			m.moveUp()
		case "down", "j":
			m.moveDown()
		case "enter":
			return m.handleEnter()
		case "esc":
			return m.handleEsc()
		}
	}
	return m, nil
}

func (m *model) moveUp() {
	switch m.state {
	case viewMenu:
		m.cursor--
		if m.cursor < 0 {
			m.cursor = len(m.commands) - 1
		}
	case viewTimeframe:
		m.tfCursor--
		if m.tfCursor < 0 {
			m.tfCursor = len(timeframes) - 1
		}
	case viewAI:
		m.aiCursor = 1 - m.aiCursor
	}
}

func (m *model) moveDown() {
	switch m.state {
	case viewMenu:
		m.cursor++
		if m.cursor >= len(m.commands) {
			m.cursor = 0
		}
	case viewTimeframe:
		m.tfCursor++
		if m.tfCursor >= len(timeframes) {
			m.tfCursor = 0
		}
	case viewAI:
		m.aiCursor = 1 - m.aiCursor
	}
}

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.state {
	case viewMenu:
		m.selected = &m.commands[m.cursor]
		m.tfCursor = 0
		m.state = viewTimeframe

	case viewTimeframe:
		tf := timeframes[m.tfCursor]
		if tf.isCustom {
			m.dateInput.Reset()
			m.dateInput.Focus()
			m.state = viewCustomDate
			m.dateErr = ""
			return m, textinput.Blink
		}
		m.args = copyArgs(tf.args)
		if m.selected.Cmd == "digest" {
			m.aiCursor = 0
			m.state = viewAI
		} else {
			m.selection = Selection{Command: m.selected, Args: m.args}
			return m, tea.Quit
		}

	case viewAI:
		if m.aiCursor == 1 {
			m.args = append(m.args, "--ai")
		}
		m.selection = Selection{Command: m.selected, Args: m.args}
		return m, tea.Quit
	}
	return m, nil
}

func (m model) handleEsc() (tea.Model, tea.Cmd) {
	switch m.state {
	case viewTimeframe:
		m.state = viewMenu
	case viewAI:
		m.state = viewTimeframe
	case viewMenu:
		m.selection = Selection{Quit: true}
		return m, tea.Quit
	}
	return m, nil
}

func (m model) updateCustomDate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.state = viewTimeframe
			m.dateErr = ""
			return m, nil
		case "ctrl+c":
			m.selection = Selection{Quit: true}
			return m, tea.Quit
		case "enter":
			date := m.dateInput.Value()
			if _, err := time.Parse("2006-01-02", date); err != nil {
				m.dateErr = "Invalid format. Use YYYY-MM-DD"
				return m, nil
			}
			m.args = []string{date}
			if m.selected.Cmd == "digest" {
				m.aiCursor = 0
				m.state = viewAI
			} else {
				m.selection = Selection{Command: m.selected, Args: m.args}
				return m, tea.Quit
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.dateInput, cmd = m.dateInput.Update(msg)
	return m, cmd
}

// ── View ───────────────────────────────────────────────────────────────────

func (m model) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(indent(titleStyle.Render("Claude Code Analytics Suite\nby Andrevops"), 2))
	b.WriteString("\n\n")

	switch m.state {
	case viewMenu:
		b.WriteString(m.viewMenu())
	case viewTimeframe:
		b.WriteString(m.viewTimeframe())
	case viewAI:
		b.WriteString(m.viewAI())
	case viewCustomDate:
		b.WriteString(m.viewCustomDate())
	}

	b.WriteString("\n")
	b.WriteString(m.viewHelp())
	b.WriteString("\n")

	return b.String()
}

func (m model) viewMenu() string {
	var b strings.Builder
	for i, cmd := range m.commands {
		cur := "  "
		nStyle := nameStyle
		dStyle := descStyle

		if i == m.cursor {
			cur = cursorStyle.Render("❯ ")
			nStyle = selectedNameStyle
			dStyle = selectedDescStyle
		}

		b.WriteString(fmt.Sprintf("  %s%-24s %s\n", cur, nStyle.Render(cmd.Name), cmdStyle.Render("("+cmd.Cmd+")")))
		b.WriteString(fmt.Sprintf("      %s\n", dStyle.Render(cmd.Desc)))

		if i < len(m.commands)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m model) viewTimeframe() string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(sectionStyle.Render(m.selected.Name))
	b.WriteString("\n\n")
	b.WriteString("  ")
	b.WriteString(subtleStyle.Render("Select timeframe:"))
	b.WriteString("\n\n")

	for i, tf := range timeframes {
		cur := "  "
		style := nameStyle
		if i == m.tfCursor {
			cur = cursorStyle.Render("❯ ")
			style = selectedNameStyle
		}
		b.WriteString(fmt.Sprintf("  %s%s\n", cur, style.Render(tf.label)))
	}
	return b.String()
}

func (m model) viewAI() string {
	var b strings.Builder
	tfLabel := ""
	if len(m.args) > 0 {
		tfLabel = " — " + m.args[0]
	} else {
		tfLabel = " — Today"
	}
	b.WriteString("  ")
	b.WriteString(sectionStyle.Render(m.selected.Name + tfLabel))
	b.WriteString("\n\n")
	b.WriteString("  ")
	b.WriteString(subtleStyle.Render("AI Analysis:"))
	b.WriteString("\n\n")

	options := []string{"Data only", "Data + AI summary"}
	for i, opt := range options {
		cur := "  "
		style := nameStyle
		if i == m.aiCursor {
			cur = cursorStyle.Render("❯ ")
			style = selectedNameStyle
		}
		b.WriteString(fmt.Sprintf("  %s%s\n", cur, style.Render(opt)))
	}
	return b.String()
}

func (m model) viewCustomDate() string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(sectionStyle.Render(m.selected.Name))
	b.WriteString("\n\n")
	b.WriteString("  ")
	b.WriteString(subtleStyle.Render("Enter date:"))
	b.WriteString("\n\n")
	b.WriteString("    ")
	b.WriteString(m.dateInput.View())
	b.WriteString("\n")

	if m.dateErr != "" {
		b.WriteString("\n    ")
		b.WriteString(errorStyle.Render(m.dateErr))
		b.WriteString("\n")
	}
	return b.String()
}

func (m model) viewHelp() string {
	var parts []string
	switch m.state {
	case viewMenu:
		parts = []string{
			helpKeyStyle.Render("↑/↓") + helpStyle.Render(" navigate"),
			helpKeyStyle.Render("enter") + helpStyle.Render(" select"),
			helpKeyStyle.Render("q") + helpStyle.Render(" quit"),
		}
	default:
		parts = []string{
			helpKeyStyle.Render("↑/↓") + helpStyle.Render(" navigate"),
			helpKeyStyle.Render("enter") + helpStyle.Render(" select"),
			helpKeyStyle.Render("esc") + helpStyle.Render(" back"),
			helpKeyStyle.Render("q") + helpStyle.Render(" quit"),
		}
	}
	return "  " + strings.Join(parts, "  •  ")
}

// ── Helpers ────────────────────────────────────────────────────────────────

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}

func copyArgs(src []string) []string {
	if src == nil {
		return []string{}
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
