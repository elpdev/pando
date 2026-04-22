package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/style"
)

func newTypingSpinner() spinner.Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Muted
	return sp
}

func (m *Model) typingTickCmd() tea.Cmd {
	return tea.Tick(typingAnimationInterval, func(t time.Time) tea.Msg {
		return typingTickMsg(t)
	})
}

func (m *Model) sendTypingCmd(recipient, state string) tea.Cmd {
	if recipient == "" || m.peer.isRoom || m.conn.idleDisconnected || m.guardCanSend() != nil {
		return nil
	}
	return func() tea.Msg {
		envelopes, err := m.messaging.TypingEnvelopes(recipient, state)
		if err != nil {
			return typingSendResultMsg{err: err}
		}
		for _, envelope := range envelopes {
			if err := m.client.Send(envelope); err != nil {
				return typingSendResultMsg{err: err}
			}
		}
		return typingSendResultMsg{}
	}
}

func (m *Model) handleInputActivity(previousValue, currentValue string) tea.Cmd {
	if previousValue == currentValue {
		return nil
	}
	if m.guardCanSend() != nil {
		return nil
	}
	now := time.Now().UTC()
	m.noteActivity(now)
	if strings.TrimSpace(currentValue) == "" {
		if !m.typing.localSent || m.typing.localPeer != m.peer.mailbox {
			m.resetLocalTypingState()
			return nil
		}
		cmd := m.sendTypingCmd(m.peer.mailbox, messaging.TypingStateIdle)
		m.resetLocalTypingState()
		return cmd
	}
	m.typing.localAt = now
	if m.typing.localSent && m.typing.localPeer == m.peer.mailbox {
		return nil
	}
	m.typing.localSent = true
	m.typing.localPeer = m.peer.mailbox
	return m.sendTypingCmd(m.peer.mailbox, messaging.TypingStateActive)
}

func (m *Model) stopTypingCmd(recipient string) tea.Cmd {
	if recipient == "" || !m.typing.localSent || m.typing.localPeer != recipient {
		m.resetLocalTypingState()
		return nil
	}
	cmd := m.sendTypingCmd(recipient, messaging.TypingStateIdle)
	m.resetLocalTypingState()
	return cmd
}

func (m *Model) resetLocalTypingState() {
	m.typing.localSent = false
	m.typing.localPeer = ""
	m.typing.localAt = time.Time{}
}

func (m *Model) clearPeerTyping() {
	m.typing.peerVisible = false
	m.typing.peerExpiresAt = time.Time{}
	m.typing.spinner = newTypingSpinner()
}

func (m *Model) renderTypingIndicator() string {
	if !m.typing.peerVisible || m.peer.mailbox == "" {
		return ""
	}
	return style.Italic.Render(fmt.Sprintf("%s is typing %s", m.peer.mailbox, m.typing.spinner.View()))
}

func (m *Model) typingFooterSegment() string {
	return m.renderTypingIndicator()
}
