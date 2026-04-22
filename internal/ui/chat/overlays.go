package chat

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/messaging"
)

func (m *Model) handleOverlays(msg tea.Msg) (bool, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
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
	_ = keyMsg
	return false, nil
}
