package chat

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/ui/style"
)

type helpShortcut struct {
	keys string
	desc string
}

var helpSectionNavigation = []helpShortcut{{"↑ ↓", "browse contacts"}, {"⏎", "open selected chat / send"}, {"tab", "switch pane"}, {"end / G", "jump to latest message"}, {"ctrl+c", "quit"}}

var helpSectionMessaging = []helpShortcut{{"ctrl+n", "add contact"}, {"ctrl+o", "attach file"}, {"ctrl+p", "peer detail"}, {"/send-photo <path>", "attach photo via path"}, {"/send-voice <path>", "attach voice via path"}, {"/send-file <path>", "attach file via path"}, {"ctrl+u", "clear input"}, {"?", "toggle this help"}, {"esc", "close overlay"}}

func (m *Model) renderHelpModal(base string) string {
	modalWidth := min(max(64, m.ui.width*2/3), max(40, m.ui.width-6))
	modalHeight := min(max(18, m.ui.height*2/3), max(14, m.ui.height-4))
	if modalWidth <= 0 || modalHeight <= 0 {
		return base
	}
	colWidth := max(20, (modalWidth-6)/2)
	title := style.Bright.Bold(true).Render("Help")
	navTitle := style.Bold.Render("Navigation")
	msgTitle := style.Bold.Render("Messaging")
	nav := renderHelpColumn(helpSectionNavigation, colWidth)
	msg := renderHelpColumn(helpSectionMessaging, colWidth)
	columns := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{navTitle, nav}, "\n")), "  ", lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{msgTitle, msg}, "\n")))
	footer := style.Subtle.Render("? or esc to close")
	body := strings.Join([]string{title, columns, footer}, "\n\n")
	modal := style.Modal.Width(modalWidth).Padding(1, 2).Render(body)
	background := style.Faint.Render(base)
	return strings.Join([]string{background, lipgloss.Place(m.ui.width, m.ui.height, lipgloss.Center, lipgloss.Center, modal)}, "\n")
}

func renderHelpColumn(entries []helpShortcut, width int) string {
	keyWidth := 0
	for _, e := range entries {
		if w := lipgloss.Width(e.keys); w > keyWidth {
			keyWidth = w
		}
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		pad := keyWidth - lipgloss.Width(e.keys)
		if pad < 0 {
			pad = 0
		}
		keys := style.StatusInfo.Render(e.keys)
		lines = append(lines, keys+strings.Repeat(" ", pad+2)+style.Muted.Render(e.desc))
	}
	return strings.Join(lines, "\n")
}
