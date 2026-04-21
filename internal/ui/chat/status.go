package chat

import "time"

// Status returns the persistent connection status line: connecting,
// connected, reconnecting with a countdown, disconnected, or auth-failed.
// Ephemeral feedback (send failures, contact imports, etc.) goes through the
// toast slot instead, see Toast().
func (m *Model) Status() string {
	return m.conn.status
}

// ConnectionState returns the coarse connection state. The App header uses
// this to pick a pill color and glyph; Status() supplies the accompanying
// detail text when one is useful.
func (m *Model) ConnectionState() ConnState {
	switch {
	case m.conn.authFailed:
		return ConnAuthFailed
	case m.conn.disconnected:
		return ConnDisconnected
	case m.conn.connecting && m.conn.reconnectAttempt > 0:
		return ConnReconnecting
	case m.conn.connecting:
		return ConnConnecting
	case m.conn.connected:
		return ConnConnected
	default:
		return ConnConnecting
	}
}

// ReconnectDelay reports the most recently scheduled reconnect delay, or
// zero if not currently waiting to reconnect. Useful for rendering
// "reconnecting in 8s" in the header.
func (m *Model) ReconnectDelay() time.Duration {
	if m.ConnectionState() != ConnReconnecting {
		return 0
	}
	return m.conn.reconnectDelay
}

func (m *Model) Mailbox() string {
	return m.mailbox
}

func (m *Model) RecipientMailbox() string {
	return m.peer.mailbox
}

func (m *Model) Toast() (string, ToastLevel) {
	if m.ui.toast == nil {
		return "", ToastInfo
	}
	return m.ui.toast.text, m.ui.toast.level
}

func (m *Model) Focus() focusState {
	return m.ui.focus
}

func (m *Model) HasPendingAttachment() bool {
	return m.pending != nil
}
