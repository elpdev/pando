package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/transport"
)

func (m *Model) connectCmd() tea.Cmd {
	m.conn.connecting = true
	m.conn.disconnected = false
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Connect(ctx); err != nil {
			return connectResultMsg{client: client, err: err}
		}
		return connectResultMsg{client: client}
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
	m.conn.idleDisconnected = false
	m.conn.reconnectDelay = delay
	m.conn.status = fmt.Sprintf("reconnecting in %s", delay)
	client := m.client
	return func() tea.Msg {
		time.Sleep(delay)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Connect(ctx); err != nil {
			return reconnectResultMsg{client: client, err: err}
		}
		return reconnectResultMsg{client: client}
	}
}

func (m *Model) waitForEvent() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		event, ok := <-client.Events()
		if !ok {
			return clientEventMsg{client: client, event: transport.Event{Err: fmt.Errorf("connection closed")}}
		}
		return clientEventMsg{client: client, event: event}
	}
}

func (m *Model) handleAuthFailure(err error) {
	m.conn.connecting = false
	m.conn.connected = false
	m.conn.disconnected = true
	m.conn.idleDisconnected = false
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
	m.conn.idleDisconnected = false
	m.conn.reconnectAttempt = 0
	m.conn.reconnectDelay = 0
	m.noteActivity(time.Now().UTC())
	m.syncInputPlaceholder()
	m.conn.status = status
}

func (m *Model) idleDisconnectCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		return idleDisconnectResultMsg{err: client.Disconnect()}
	}
}

func (m *Model) markIdleDisconnected(now time.Time) {
	m.conn.connecting = false
	m.conn.connected = false
	m.conn.disconnected = true
	m.conn.idleDisconnected = true
	m.conn.authFailed = false
	m.conn.reconnectAttempt = 0
	m.conn.reconnectDelay = 0
	m.conn.lastActivityAt = now
	m.conn.status = "idle; reconnect on send"
	m.clearPeerTyping()
	m.resetLocalTypingState()
	m.syncInputPlaceholder()
}

func (m *Model) noteActivity(now time.Time) {
	m.conn.lastActivityAt = now
}

func (m *Model) handleConnectionError(err error) tea.Cmd {
	if transport.IsUnauthorized(err) {
		m.handleAuthFailure(err)
		return nil
	}
	if isPermanentConnectError(err) {
		m.conn.connecting = false
		m.conn.connected = false
		m.conn.disconnected = true
		m.conn.idleDisconnected = false
		m.conn.authFailed = false
		m.conn.reconnectDelay = 0
		m.conn.status = err.Error()
		m.clearPeerTyping()
		m.resetLocalTypingState()
		m.syncInputPlaceholder()
		return nil
	}
	m.conn.status = fmt.Sprintf("disconnected: %v", err)
	m.conn.disconnected = true
	m.conn.connected = false
	m.conn.idleDisconnected = false
	m.resetLocalTypingState()
	return m.reconnectCmd()
}

func isPermanentConnectError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "publish your signed relay directory entry before connecting") || strings.Contains(message, "device is not authorized for this mailbox")
}
