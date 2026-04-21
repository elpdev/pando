package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) renderPeerDetailModal(base string) string {
	modalWidth := min(max(56, m.ui.width*2/3), max(40, m.ui.width-6))
	verticalAlign := lipgloss.Center
	if m.ui.width < narrowThreshold {
		modalWidth = max(1, m.ui.width)
		verticalAlign = lipgloss.Bottom
	}
	if modalWidth <= 0 || m.ui.height <= 0 {
		return base
	}
	bodyWidth := max(24, modalWidth-6)
	title := style.ModalTitle.Render("Peer detail")
	mailboxLine := style.PeerAccentStyle(m.peer.fingerprint).Bold(true).Render(m.peer.mailbox)
	verifyLabel := identity.TrustLabel(m.peer.trustSource, m.peer.verified)
	verifyLine := style.UnverifiedWarn.Render(verifyLabel)
	if m.peer.verified {
		verifyLine = style.VerifiedOk.Render(verifyLabel)
	}
	row := func(label, value string) string {
		padLabel := style.Muted.Render(label)
		return padLabel + "  " + value
	}
	parts := []string{title}
	if m.peer.isRoom {
		parts = append(parts,
			style.StatusInfo.Bold(true).Render(m.peer.label),
			"",
			row("members", style.Bright.Render(fmt.Sprintf("%d/%d", m.peer.memberCount, messaging.DefaultRoomCap))),
			row("status ", style.Muted.Render(map[bool]string{true: "joined", false: "not joined"}[m.peer.joined])),
			row("relay  ", style.Muted.Render(m.relay.url)),
		)
	} else {
		fullFp := m.peer.fingerprint
		shortFp := style.FormatFingerprintShort(fullFp)
		fpLong := style.FormatFingerprint(fullFp)
		deviceCount := 0
		if contact, err := m.messaging.Contact(m.peer.mailbox); err == nil {
			deviceCount = len(contact.ActiveDevices())
		}
		parts = append(parts,
			mailboxLine+"  "+verifyLine,
			"",
			row("fingerprint", style.Bright.Render(fpLong)),
			row("short     ", style.Muted.Render(shortFp)),
			row("devices   ", style.Bright.Render(fmt.Sprintf("%d active", deviceCount))),
			row("relay     ", style.Muted.Render(m.relay.url)),
		)
	}
	parts = append(parts, "", style.Subtle.Render("ctrl+p or esc to close"))
	body := lipgloss.NewStyle().Width(bodyWidth).Render(strings.Join(parts, "\n"))
	modal := style.Modal.Width(modalWidth).Padding(1, 2).Render(body)
	return lipgloss.Place(m.ui.width, m.ui.height, lipgloss.Center, verticalAlign, modal,
		lipgloss.WithWhitespaceBackground(style.BackdropTint))
}
