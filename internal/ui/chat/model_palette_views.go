package chat

import tea "github.com/charmbracelet/bubbletea"

// resolvePaletteView returns the backing view struct for id, or nil if no view
// is registered. New views are added here once they implement paletteView.
func (m *Model) resolvePaletteView(id paletteViewID) paletteView {
	switch id {
	case paletteViewHelp:
		return helpView{}
	}
	return nil
}

// enterPaletteView is invoked by the palette when a view node is activated.
// It resolves the view, captures the Model's relevant open-time context, and
// calls the view's Open hook.
func (m *Model) enterPaletteView(id paletteViewID) tea.Cmd {
	view := m.resolvePaletteView(id)
	if view == nil {
		return nil
	}
	return view.Open(viewOpenCtx{
		peerMailbox:     m.peer.mailbox,
		peerFingerprint: m.peer.fingerprint,
	})
}

// exitPaletteView is invoked by the palette when leaving a view path (via back
// or Close). It closes the backing view so any resources are released.
func (m *Model) exitPaletteView(id paletteViewID) {
	view := m.resolvePaletteView(id)
	if view == nil {
		return
	}
	view.Close()
}

// openPaletteAtHelp opens the command palette and lands directly on the Help
// view, preserving the Settings › Help breadcrumb so Esc returns to Settings.
func (m *Model) openPaletteAtHelp() tea.Cmd {
	m.commandPalette.SyncContext(m.peer.mailbox != "", m.pendingRequestsCount, m.recentVoiceNotes(), m.voicePlayer != nil && m.voicePlayer.IsPlaying())
	m.input.Blur()
	return m.commandPalette.OpenAtPath([]string{paletteNodeIDSettings, paletteNodeIDHelp})
}

// handlePaletteCloseMsg dismisses the palette (closing any active view via its
// Close hook) and surfaces the optional toast. Used by views that complete or
// dismiss themselves programmatically.
func (m *Model) handlePaletteCloseMsg(msg paletteCloseMsg) (*Model, tea.Cmd) {
	m.commandPalette.Close()
	if m.ui.focus == focusChat {
		m.input.Focus()
	}
	if msg.toast != "" {
		m.pushToast(msg.toast, msg.toastKind)
	}
	return m, nil
}
