package chat

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/transport"
)

func (m *Model) connectCmd() tea.Cmd {
	m.conn.connecting = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.client.Connect(ctx); err != nil {
			return connectResultMsg{err: err}
		}
		return connectResultMsg{}
	}
}

func (m *Model) reconnectCmd() tea.Cmd {
	attempt := m.conn.reconnectAttempt + 1
	m.conn.reconnectAttempt = attempt
	shift := attempt - 1
	if shift > 4 {
		shift = 4
	}
	delay := time.Second * time.Duration(1<<shift)
	m.conn.connecting = true
	m.conn.reconnectDelay = delay
	m.conn.status = fmt.Sprintf("reconnecting in %s", delay)
	return func() tea.Msg {
		time.Sleep(delay)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.client.Connect(ctx); err != nil {
			return reconnectResultMsg{err: err}
		}
		return reconnectResultMsg{}
	}
}

func (m *Model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.client.Events()
		if !ok {
			return clientEventMsg(transport.Event{Err: fmt.Errorf("connection closed")})
		}
		return clientEventMsg(event)
	}
}

func (m *Model) handleAuthFailure(err error) {
	m.conn.connecting = false
	m.conn.connected = false
	m.conn.disconnected = true
	m.conn.authFailed = true
	m.conn.status = fmt.Sprintf("relay auth failed: %v", err)
	m.clearPeerTyping()
	m.resetLocalTypingState()
	m.syncInputPlaceholder()
}

func (m *Model) markConnected(status string) {
	m.conn.connecting = false
	m.conn.connected = true
	m.conn.authFailed = false
	m.conn.disconnected = false
	m.conn.reconnectAttempt = 0
	m.conn.reconnectDelay = 0
	m.syncInputPlaceholder()
	m.conn.status = status
}

func (m *Model) handleConnectionError(err error) tea.Cmd {
	if transport.IsUnauthorized(err) {
		m.handleAuthFailure(err)
		return nil
	}
	m.conn.status = fmt.Sprintf("disconnected: %v", err)
	m.conn.disconnected = true
	m.conn.connected = false
	m.resetLocalTypingState()
	return m.reconnectCmd()
}
