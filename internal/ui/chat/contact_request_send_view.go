package chat

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m contactRequestSendModal) Overlay(width, height int) string {
	if !m.open {
		return ""
	}
	bodyWidth := max(1, paletteWidth(width)-6)
	lines := []string{
		style.PaletteMeta.Width(bodyWidth).Render("Send an outgoing contact request to a discoverable mailbox on the active relay."),
		renderContactRequestSendInput(bodyWidth, "Mailbox", m.inputs[0], m.focused == 0),
		renderContactRequestSendInput(bodyWidth, "Note", m.inputs[1], m.focused == 1),
	}
	if m.busy {
		lines = append(lines, style.PaletteMeta.Width(bodyWidth).Render("sending request..."))
	}
	if m.error != "" {
		lines = append(lines, style.StatusBad.Width(bodyWidth).Render(m.error))
	}
	footer := "tab move · enter send · esc cancel"
	if m.busy {
		footer = "sending..."
	}
	return renderPaletteOverlay(width, height, "Send Contact Request", "Create a pending introduction request without importing the contact yet.", []string{strings.Join(lines, "\n\n")}, footer)
}

func renderContactRequestSendInput(width int, label string, input textinput.Model, focused bool) string {
	input.Width = max(1, width-2)
	heading := style.Muted.Render(label)
	if focused {
		heading = style.Bright.Render(label)
	}
	return heading + "\n" + style.PaletteInput.Width(width).Padding(0, 1).Render(input.View())
}
