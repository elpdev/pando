package style

import "github.com/charmbracelet/lipgloss"

var (
	Muted    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	Accent   = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	Warning  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	Selected = lipgloss.NewStyle().Background(lipgloss.Color("238"))
	Bold     = lipgloss.NewStyle().Bold(true)
	Italic   = lipgloss.NewStyle().Italic(true)
	SidebarBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderRight(true).BorderLeft(false).BorderTop(false).BorderBottom(false).
			BorderForeground(lipgloss.Color("238"))
)
