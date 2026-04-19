package chat

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/transport"
)

func (m *Model) connectCmd() tea.Cmd {
	m.connecting = true
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
	attempt := m.reconnectAttempt + 1
	m.reconnectAttempt = attempt
	shift := attempt - 1
	if shift > 4 {
		shift = 4
	}
	delay := time.Second * time.Duration(1<<shift)
	m.connecting = true
	m.reconnectDelay = delay
	m.status = fmt.Sprintf("reconnecting in %s", delay)
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
	m.connecting = false
	m.connected = false
	m.disconnected = true
	m.authFailed = true
	m.status = fmt.Sprintf("relay auth failed: %v", err)
	m.clearPeerTyping()
	m.resetLocalTypingState()
	m.syncInputPlaceholder()
}

func (m *Model) markConnected(status string) {
	m.connecting = false
	m.connected = true
	m.authFailed = false
	m.disconnected = false
	m.reconnectAttempt = 0
	m.reconnectDelay = 0
	m.syncInputPlaceholder()
	m.status = status
}

func (m *Model) handleConnectionError(err error) tea.Cmd {
	if transport.IsUnauthorized(err) {
		m.handleAuthFailure(err)
		return nil
	}
	m.status = fmt.Sprintf("disconnected: %v", err)
	m.disconnected = true
	m.connected = false
	m.resetLocalTypingState()
	return m.reconnectCmd()
}
