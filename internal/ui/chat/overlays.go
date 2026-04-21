package chat

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) handleOverlays(msg tea.Msg) (bool, tea.Cmd) {
	if m.contactRequests.open {
		if handled, cmd := m.contactRequests.Update(msg); handled {
			return true, cmd
		}
	}
	if m.addContact.open {
		if handled, cmd := m.addContact.Update(msg); handled {
			return true, cmd
		}
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if m.helpOpen {
		return true, m.handleHelpKey(keyMsg)
	}
	if m.commandPalette.open {
		action, cmd := m.commandPalette.Update(msg)
		if action != nil {
			return true, m.handleCommandPaletteAction(*action)
		}
		if !m.commandPalette.open && m.ui.focus == focusChat {
			m.input.Focus()
		}
		return true, cmd
	}
	if m.filePicker.open {
		var cmd tea.Cmd
		m.filePicker, cmd = m.filePicker.Update(msg)
		if cmd == nil {
			return true, nil
		}
		switch next := cmd().(type) {
		case filePickerClosedMsg:
			m.closeFilePicker()
			return true, nil
		case filePickerErrorMsg:
			m.pushToast(fmt.Sprintf("file picker failed: %v", next.err), ToastBad)
			return true, nil
		case filePickerSelectedMsg:
			m.closeFilePicker()
			if err := m.setPendingAttachment(next.path, messaging.AttachmentTypeFile); err != nil {
				m.pushToast(fmt.Sprintf("attach failed: %v", err), ToastBad)
			}
			return true, nil
		default:
			return true, func() tea.Msg { return next }
		}
	}
	if m.peerDetailOpen {
		return true, m.handlePeerDetailKey(keyMsg)
	}
	return false, nil
}

// handleHelpKey closes the help overlay on ?, esc, q, or ctrl+c. Every other
// key is absorbed so the chat input doesn't receive keystrokes meant to
// dismiss the overlay.
func (m *Model) handleHelpKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case msg.Type == tea.KeyEsc:
		m.helpOpen = false
	case msg.Type == tea.KeyCtrlC:
		m.helpOpen = false
		return tea.Quit
	case msg.Type == tea.KeyRunes && (string(msg.Runes) == "?" || string(msg.Runes) == "q"):
		m.helpOpen = false
	}
	return nil
}

func (m *Model) handlePeerDetailKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyEsc {
		m.peerDetailOpen = false
		if m.ui.focus == focusChat {
			m.input.Focus()
		}
	}
	return nil
}

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
	case commandPaletteCommandThemes:
		return m.applyPaletteTheme(action.themeName)
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
