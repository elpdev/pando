package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) renderPeerDetailModal(base string) string {
	if m.peer.isRoom {
		return base
	}
	modalWidth := min(max(56, m.ui.width*2/3), max(40, m.ui.width-6))
	if modalWidth <= 0 || m.ui.height <= 0 {
		return base
	}
	bodyWidth := max(24, modalWidth-6)
	title := style.Bright.Bold(true).Render("Peer detail")
	mailboxLine := style.PeerAccentStyle(m.peer.fingerprint).Bold(true).Render(m.peer.mailbox)
	verifyLabel := identity.TrustLabel(m.peer.trustSource, m.peer.verified)
	verifyLine := style.UnverifiedWarn.Render(verifyLabel)
	if m.peer.verified {
		verifyLine = style.VerifiedOk.Render(verifyLabel)
	}
	fullFp := m.peer.fingerprint
	shortFp := style.FormatFingerprintShort(fullFp)
	fpLong := style.FormatFingerprint(fullFp)
	deviceCount := 0
	if contact, err := m.messaging.Contact(m.peer.mailbox); err == nil {
		deviceCount = len(contact.ActiveDevices())
	}
	row := func(label, value string) string {
		padLabel := style.Muted.Render(label)
		return padLabel + "  " + value
	}
	parts := []string{title, mailboxLine + "  " + verifyLine, "", row("fingerprint", style.Bright.Render(fpLong)), row("short     ", style.Muted.Render(shortFp)), row("devices   ", style.Bright.Render(fmt.Sprintf("%d active", deviceCount))), row("relay     ", style.Muted.Render(m.relay.url)), "", style.Subtle.Render("ctrl+p or esc to close")}
	body := lipgloss.NewStyle().Width(bodyWidth).Render(strings.Join(parts, "\n"))
	modal := style.Modal.Width(modalWidth).Padding(1, 2).Render(body)
	background := style.Faint.Render(base)
	return strings.Join([]string{background, lipgloss.Place(m.ui.width, m.ui.height, lipgloss.Center, lipgloss.Center, modal)}, "\n")
}
