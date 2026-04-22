package chat

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/style"
)

// peerDetailView renders read-only info about the active peer (or room).
// It's a thin adapter over Model so it can inspect live peer and relay state
// at render time; the pointer is captured on Open.
type peerDetailView struct {
	m *Model
}

func (v *peerDetailView) Open(viewOpenCtx) tea.Cmd { return nil }

func (v *peerDetailView) Close() {}

func (v *peerDetailView) Update(msg tea.Msg) (bool, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if v.m.canVerifyActiveContact() && (key.String() == "v" || key.String() == "y") {
		return true, paletteNavigateCmd(paletteNodeIDContacts, string(commandPaletteCommandVerifyContact))
	}
	return false, nil
}

func (v *peerDetailView) Body(int, int) string {
	row := func(label, value string) string {
		return style.Muted.Render(label) + "  " + value
	}
	peer := v.m.peer
	var rows []string
	if peer.isRoom {
		rows = append(rows,
			row("members", style.Bright.Render(fmt.Sprintf("%d/%d", peer.memberCount, messaging.DefaultRoomCap))),
			row("status ", style.Muted.Render(map[bool]string{true: "joined", false: "not joined"}[peer.joined])),
			row("relay  ", style.Muted.Render(v.m.relay.url)),
		)
	} else {
		fullFp := peer.fingerprint
		shortFp := style.FormatFingerprintShort(fullFp)
		fpLong := style.FormatFingerprint(fullFp)
		deviceCount := 0
		if contact, err := v.m.messaging.Contact(peer.mailbox); err == nil {
			deviceCount = len(contact.ActiveDevices())
		}
		rows = append(rows,
			row("fingerprint", style.Bright.Render(fpLong)),
			row("short     ", style.Muted.Render(shortFp)),
			row("devices   ", style.Bright.Render(fmt.Sprintf("%d active", deviceCount))),
			row("relay     ", style.Muted.Render(v.m.relay.url)),
		)
	}
	return strings.Join(rows, "\n")
}

func (v *peerDetailView) Subtitle() string {
	peer := v.m.peer
	if peer.isRoom {
		return style.StatusInfo.Bold(true).Render(peer.label)
	}
	verifyLabel := identity.TrustLabel(peer.trustSource, peer.verified)
	verifyLine := style.UnverifiedWarn.Render(verifyLabel)
	if peer.verified {
		verifyLine = style.VerifiedOk.Render(verifyLabel)
	}
	mailboxLine := style.PeerAccentStyle(peer.fingerprint).Bold(true).Render(peer.mailbox)
	return mailboxLine + "  " + verifyLine
}

func (v *peerDetailView) Footer() string {
	if v.m.canVerifyActiveContact() {
		return "v verify · esc close"
	}
	return "esc to close"
}
