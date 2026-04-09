package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorPrimary = lipgloss.Color("75")  // Sky blue
	colorAccent  = lipgloss.Color("35")  // Green
	colorSubtle  = lipgloss.Color("241") // Gray
	colorCyan    = lipgloss.Color("39")  // Bright cyan
	colorDim     = lipgloss.Color("238") // Dark gray
	colorError   = lipgloss.Color("197") // Red

	// Title box
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("255")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 2).
			Width(53).
			Align(lipgloss.Center)

	// Menu items
	cursorStyle = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	selectedNameStyle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	nameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	cmdStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	descStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	selectedDescStyle = lipgloss.NewStyle().
				Foreground(colorSubtle)

	// Section / sublabels
	sectionStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	subtleStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorError)

	// Footer help
	helpStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorPrimary)
)
