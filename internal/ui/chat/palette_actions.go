package chat

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) openCommandPalette() tea.Cmd {
	m.commandPalette.SyncContext(m.peer.mailbox != "", m.pendingRequestsCount)
	m.input.Blur()
	return m.commandPalette.Open()
}

func (m *Model) handleCommandPaletteAction(action commandPaletteAction) tea.Cmd {
	switch action.command {
	case commandPaletteCommandAddContact:
		m.openAddContactModal()
		return nil
	case commandPaletteCommandSendContactRequest:
		return m.openContactRequestSendModal()
	case commandPaletteCommandContactRequests:
		m.openContactRequestsModal()
		return nil
	case commandPaletteCommandAttachFile:
		return m.handleAttachKey()
	case commandPaletteCommandPeerDetail:
		if m.peer.mailbox != "" {
			m.peerDetailOpen = true
		}
		return nil
	case commandPaletteCommandVerifyContact:
		return m.openContactVerifyModal()
	case commandPaletteCommandThemes:
		return m.applyPaletteTheme(action.themeName)
	case commandPaletteCommandSwitchRelay:
		return m.switchRelay(action.relayName)
	case commandPaletteCommandAddRelay:
		return m.openAddRelayModal()
	case commandPaletteCommandRemoveRelay:
		return m.removeRelayProfile(action.relayName)
	case commandPaletteCommandEditRelay:
		return m.openEditRelayModal(action.relayName)
	case commandPaletteCommandMessageTTL:
		return m.applyPaletteMessageTTL(action.messageTTL)
	default:
		return nil
	}
}

func (m *Model) applyPaletteTheme(name string) tea.Cmd {
	theme, ok := style.Themes[name]
	if !ok {
		m.pushToast(fmt.Sprintf("unknown theme %q", name), ToastBad)
		return nil
	}
	style.Apply(theme)
	m.pushToast(fmt.Sprintf("theme set to %s", name), ToastInfo)
	if m.commandPalette.deps.saveTheme == nil {
		return nil
	}
	if err := m.commandPalette.deps.saveTheme(name); err != nil {
		m.pushToast(fmt.Sprintf("theme applied but not saved: %v", err), ToastWarn)
	}
	return nil
}

func (m *Model) applyPaletteMessageTTL(ttl time.Duration) tea.Cmd {
	if ttl <= 0 {
		return nil
	}
	m.messaging.SetMessageTTL(ttl)
	m.pushToast(fmt.Sprintf("message TTL set to %s", formatMessageTTL(ttl)), ToastInfo)
	if m.commandPalette.deps.saveMessageTTL == nil {
		return nil
	}
	if err := m.commandPalette.deps.saveMessageTTL(ttl); err != nil {
		m.pushToast(fmt.Sprintf("TTL applied but not saved: %v", err), ToastWarn)
	}
	return nil
}
