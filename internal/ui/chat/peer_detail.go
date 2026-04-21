package chat

import (
	"fmt"
	"strings"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) renderPeerDetailModal(base string) string {
	if m.ui.width <= 0 || m.ui.height <= 0 {
		return base
	}

	title := "Peer detail"
	if m.peer.isRoom {
		title = "Room detail"
	}

	var subtitle string
	if m.peer.isRoom {
		subtitle = style.StatusInfo.Bold(true).Render(m.peer.label)
	} else {
		verifyLabel := identity.TrustLabel(m.peer.trustSource, m.peer.verified)
		verifyLine := style.UnverifiedWarn.Render(verifyLabel)
		if m.peer.verified {
			verifyLine = style.VerifiedOk.Render(verifyLabel)
		}
		mailboxLine := style.PeerAccentStyle(m.peer.fingerprint).Bold(true).Render(m.peer.mailbox)
		subtitle = mailboxLine + "  " + verifyLine
	}

	row := func(label, value string) string {
		return style.Muted.Render(label) + "  " + value
	}
	var rows []string
	if m.peer.isRoom {
		rows = append(rows,
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
		rows = append(rows,
			row("fingerprint", style.Bright.Render(fpLong)),
			row("short     ", style.Muted.Render(shortFp)),
			row("devices   ", style.Bright.Render(fmt.Sprintf("%d active", deviceCount))),
			row("relay     ", style.Muted.Render(m.relay.url)),
		)
	}

	return renderPaletteOverlay(
		m.ui.width, m.ui.height,
		title, subtitle,
		[]string{strings.Join(rows, "\n")},
		m.peerDetailFooter(),
	)
}

func (m *Model) peerDetailFooter() string {
	if m.canVerifyActiveContact() {
		return "v verify · esc close"
	}
	return "esc to close"
}
