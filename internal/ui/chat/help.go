package chat

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/ui/style"
)

type helpShortcut struct {
	keys string
	desc string
}

var helpSectionNavigation = []helpShortcut{{"↑ ↓", "browse contacts or draft history"}, {"pgup / pgdn", "scroll messages"}, {"home / end", "jump transcript top/bottom"}, {"tab", "switch pane"}, {"ctrl+c", "quit"}}

var helpSectionMessaging = []helpShortcut{{"enter", "send / open selected chat"}, {"shift+enter", "insert newline"}, {"ctrl+p", "open command palette"}, {"/send-photo <path>", "queue photo via path"}, {"/send-voice <path>", "queue voice via path"}, {"/send-file <path>", "queue file via path"}, {"esc", "back / close overlay"}, {"?", "toggle this help"}}

type helpView struct{}

func (helpView) Open(viewOpenCtx) tea.Cmd { return nil }

func (helpView) Close() {}

func (helpView) Update(msg tea.Msg) (bool, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch {
	case key.Type == tea.KeyCtrlC:
		return true, tea.Quit
	case key.Type == tea.KeyRunes && (string(key.Runes) == "?" || string(key.Runes) == "q"):
		return true, dismissPaletteCmd()
	}
	return true, nil
}

func (helpView) Body(width int) string {
	colWidth := max(20, (width-2)/2)
	navTitle := style.Bold.Render("Navigation")
	msgTitle := style.Bold.Render("Messaging")
	nav := renderHelpColumn(helpSectionNavigation, colWidth)
	msg := renderHelpColumn(helpSectionMessaging, colWidth)
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{navTitle, nav}, "\n")),
		"  ",
		lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{msgTitle, msg}, "\n")),
	)
}

func (helpView) Subtitle() string { return "Keyboard shortcuts for navigation and messaging." }

func (helpView) Footer() string { return "? or esc to close" }

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
