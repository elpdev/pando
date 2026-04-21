package chat

import (
	"strings"

	"github.com/elpdev/pando/internal/ui/style"
)

func (m contactVerifyModal) Overlay(width, height int) string {
	if !m.open {
		return ""
	}
	bodyWidth := max(1, paletteWidth(width)-6)
	rows := []string{
		style.PaletteMeta.Width(bodyWidth).Render("Mark this contact as manually verified after confirming the fingerprint out-of-band."),
		style.Muted.Render("Mailbox") + "\n" + style.PaletteInput.Width(bodyWidth).Padding(0, 1).Render(m.mailbox),
		style.Muted.Render("Fingerprint") + "\n" + style.PaletteInput.Width(bodyWidth).Padding(0, 1).Render(style.FormatFingerprint(m.fingerprint)),
	}
	return renderPaletteOverlay(width, height, "Verify Contact", "This confirms you trust this contact's identity.", []string{strings.Join(rows, "\n\n")}, "enter verify · y verify · esc cancel")
}
