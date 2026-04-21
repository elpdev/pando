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

var helpSectionNavigation = []helpShortcut{{"↑ ↓", "browse contacts or draft history"}, {"pgup / pgdn", "scroll messages"}, {"home / end", "jump transcript top/bottom"}, {"tab", "switch pane"}, {"ctrl+c", "quit"}}

var helpSectionMessaging = []helpShortcut{{"enter", "send / open selected chat"}, {"shift+enter", "insert newline"}, {"ctrl+n", "add contact"}, {"ctrl+o", "queue file attachment"}, {"ctrl+p", "peer detail"}, {"/send-photo <path>", "queue photo via path"}, {"/send-voice <path>", "queue voice via path"}, {"/send-file <path>", "queue file via path"}, {"esc", "clear attachment or close overlay"}, {"?", "toggle this help"}}

func (m *Model) renderHelpModal(_ string) string {
	modalWidth := min(max(64, m.ui.width*2/3), max(40, m.ui.width-6))
	modalHeight := min(max(18, m.ui.height*2/3), max(14, m.ui.height-4))
	if modalWidth <= 0 || modalHeight <= 0 {
		return ""
	}
	colWidth := max(20, (modalWidth-6)/2)
	title := style.ModalTitle.Render("Help")
	navTitle := style.Bold.Render("Navigation")
	msgTitle := style.Bold.Render("Messaging")
	nav := renderHelpColumn(helpSectionNavigation, colWidth)
	msg := renderHelpColumn(helpSectionMessaging, colWidth)
	columns := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{navTitle, nav}, "\n")), "  ", lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{msgTitle, msg}, "\n")))
	footer := style.Subtle.Render("? or esc to close")
	body := strings.Join([]string{title, columns, footer}, "\n\n")
	modal := style.Modal.Width(modalWidth).Padding(1, 2).Render(body)
	return lipgloss.Place(m.ui.width, m.ui.height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceBackground(style.BackdropTint))
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
