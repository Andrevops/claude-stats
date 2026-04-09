package tui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type viewState int

const (
	viewMenu viewState = iota
	viewTimeframe
	viewAI
	viewCustomDate
	viewRunning
	viewOutput
)

// Command represents an analytics command exposed to the TUI.
type Command struct {
	Cmd, Name, Desc string
	Fn              func([]string)
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

// commandDoneMsg is sent when a command finishes executing.
type commandDoneMsg struct {
	output string
}

type model struct {
	state     viewState
	commands  []Command
	cursor    int
	tfCursor  int
	aiCursor  int
	selected  *Command
	args      []string
	dateInput textinput.Model
	dateErr   string
	spinner   spinner.Model
	viewport  viewport.Model
	output    string
	width     int
	height    int
}

func newModel(commands []Command) model {
	ti := textinput.New()
	ti.Placeholder = "YYYY-MM-DD"
	ti.CharLimit = 10
	ti.Width = 12

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorCyan)

	return model{
		commands:  commands,
		state:     viewMenu,
		dateInput: ti,
		spinner:   s,
		width:     80,
		height:    24,
	}
}

// Run starts the interactive TUI. Blocks until the user quits.
func Run(commands []Command) {
	m := newModel(commands)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

// ── Update ─────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.state == viewOutput {
			m.viewport.Width = m.width
			m.viewport.Height = m.height - 4
		}
		return m, nil

	case commandDoneMsg:
		m.output = msg.output
		h := m.height - 4
		if h < 5 {
			h = 5
		}
		m.viewport = viewport.New(m.width, h)
		m.viewport.SetContent(m.output)
		m.state = viewOutput
		return m, nil
	}

	switch m.state {
	case viewCustomDate:
		return m.updateCustomDate(msg)
	case viewRunning:
		return m.updateRunning(msg)
	case viewOutput:
		return m.updateOutput(msg)
	default:
		return m.updateNav(msg)
	}
}

func (m model) updateNav(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "q", "ctrl+c":
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
	return m, nil
}

func (m model) updateRunning(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) updateOutput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.state = viewMenu
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m model) updateCustomDate(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.state = viewTimeframe
			m.dateErr = ""
			return m, nil
		case "ctrl+c":
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
				return m, nil
			}
			return m.startCommand()
		}
	}
	var cmd tea.Cmd
	m.dateInput, cmd = m.dateInput.Update(msg)
	return m, cmd
}

// ── Navigation helpers ─────────────────────────────────────────────────────

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
			return m.startCommand()
		}

	case viewAI:
		if m.aiCursor == 1 {
			m.args = append(m.args, "--ai")
		}
		return m.startCommand()
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
		return m, tea.Quit
	}
	return m, nil
}

func (m model) startCommand() (tea.Model, tea.Cmd) {
	m.state = viewRunning
	return m, tea.Batch(
		m.spinner.Tick,
		executeCommand(m.selected.Fn, m.args),
	)
}

// ── Command execution ──────────────────────────────────────────────────────

func executeCommand(fn func([]string), args []string) tea.Cmd {
	return func() tea.Msg {
		output := captureStdout(func() { fn(args) })
		return commandDoneMsg{output: output}
	}
}

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ch := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		ch <- buf.String()
	}()

	fn()
	w.Close()
	os.Stdout = old
	return <-ch
}

// ── View ───────────────────────────────────────────────────────────────────

func (m model) View() string {
	switch m.state {
	case viewOutput:
		return m.viewOutputScreen()
	case viewRunning:
		return m.viewRunningScreen()
	default:
		return m.viewMenuScreen()
	}
}

func (m model) viewMenuScreen() string {
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
	tfLabel := "Today"
	if len(m.args) > 0 {
		tfLabel = m.args[0]
	}
	b.WriteString("  ")
	b.WriteString(sectionStyle.Render(m.selected.Name + " — " + tfLabel))
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

func (m model) viewRunningScreen() string {
	return fmt.Sprintf("\n\n  %s Running %s...\n", m.spinner.View(), m.selected.Name)
}

func (m model) viewOutputScreen() string {
	// Header bar
	title := fmt.Sprintf(" 📊 %s ", m.selected.Name)
	pct := fmt.Sprintf(" %3.0f%% ", m.viewport.ScrollPercent()*100)
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(pct)
	if gap < 0 {
		gap = 0
	}
	header := headerBarStyle.Render(title) +
		headerBarStyle.Render(strings.Repeat(" ", gap)) +
		scrollPctStyle.Render(pct)

	// Footer
	help := helpKeyStyle.Render("↑/↓") + helpStyle.Render(" scroll") + "  •  " +
		helpKeyStyle.Render("pgup/pgdn") + helpStyle.Render(" page") + "  •  " +
		helpKeyStyle.Render("esc") + helpStyle.Render(" menu") + "  •  " +
		helpKeyStyle.Render("q") + helpStyle.Render(" quit")

	return header + "\n" + m.viewport.View() + "\n" + help
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
