package chat

import (
	"fmt"
	"strings"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) FooterSegments() []string {
	segments := []string{m.connectionFooterSegment()}
	if typing := m.typingFooterSegment(); typing != "" {
		segments = append(segments, typing)
		return segments
	}
	if peer := m.peerFooterSegment(); peer != "" {
		segments = append(segments, peer)
	}
	segments = append(segments, m.keyHintSegment())
	return segments
}

func verificationLabel(verified bool, trustSource string) string {
	return identity.TrustLabel(trustSource, verified)
}

func (m *Model) connectionFooterSegment() string {
	switch m.ConnectionState() {
	case ConnConnected:
		return style.StatusOk.Render(style.GlyphConnected) + " " + style.Muted.Render("connected")
	case ConnConnecting:
		return style.StatusWarn.Render(style.GlyphReconnecting) + " " + style.Muted.Render("connecting")
	case ConnReconnecting:
		txt := "reconnecting"
		if delay := m.ReconnectDelay(); delay > 0 {
			txt = fmt.Sprintf("reconnecting in %s", delay)
		}
		return style.StatusWarn.Render(style.GlyphReconnecting) + " " + style.Muted.Render(txt)
	case ConnDisconnected:
		return style.StatusBad.Render(style.GlyphOffline) + " " + style.Muted.Render("offline")
	case ConnAuthFailed:
		return style.StatusBad.Render(style.GlyphAuthFailed) + " " + style.Muted.Render("auth failed")
	default:
		return ""
	}
}

func (m *Model) peerFooterSegment() string {
	if m.peer.mailbox == "" {
		return style.Muted.Render("no active chat")
	}
	if m.peer.isRoom {
		joinState := "joined"
		if !m.peer.joined {
			joinState = "not joined"
		}
		return style.StatusInfo.Render(m.peer.label) + " " + style.Muted.Render(fmt.Sprintf("%s %d/%d", joinState, m.peer.memberCount, messaging.DefaultRoomCap))
	}
	verifyLabel := verificationLabel(m.peer.verified, m.peer.trustSource)
	verifyStyle := style.UnverifiedWarn
	if m.peer.verified {
		verifyStyle = style.VerifiedOk
	}
	return style.PeerAccentStyle(m.peer.fingerprint).Render(m.peer.mailbox) + " " + verifyStyle.Render(verifyLabel)
}

func (m *Model) keyHintSegment() string {
	if m.filePicker.open {
		return style.Muted.Render("type filter  up/down browse  enter select  backspace up  esc close")
	}
	if m.commandPalette.open {
		return style.Muted.Render(strings.ReplaceAll(m.commandPalette.footer(), " · ", "  "))
	}
	if m.ui.focus == focusSidebar {
		return style.Muted.Render("up/down browse  enter open  ctrl+p commands  tab chat  ? help")
	}
	hints := []string{"enter send", "shift+enter newline", "ctrl+p commands", "tab sidebar", "? help"}
	if m.pending != nil {
		hints = append([]string{"esc clear attachment"}, hints...)
	}
	return style.Muted.Render(strings.Join(hints, "  "))
}
