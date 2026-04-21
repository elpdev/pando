package chat

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/messaging"
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
	if m.addRelay.open {
		if handled, cmd := m.addRelay.Update(msg); handled {
			return true, cmd
		}
	}
	if m.contactRequestSend.open {
		if handled, cmd := m.contactRequestSend.Update(msg); handled {
			return true, cmd
		}
	}
	if m.contactVerify.open {
		if handled, cmd := m.contactVerify.Update(msg); handled {
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
	if m.canVerifyActiveContact() && (msg.String() == "v" || msg.String() == "y") {
		return m.openContactVerifyModal()
	}
	if msg.Type == tea.KeyEsc {
		m.peerDetailOpen = false
		if m.ui.focus == focusChat {
			m.input.Focus()
		}
	}
	return nil
}
