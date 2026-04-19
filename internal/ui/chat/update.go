package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/transport"
)

func (m *Model) handleKeyMsg(msg tea.KeyMsg) (*Model, tea.Cmd) {
	if msg.Type == tea.KeyRunes && string(msg.Runes) == "?" && m.input.Value() == "" {
		m.helpOpen = true
		return m, nil
	}
	if msg.Type == tea.KeyTab {
		m.toggleFocus()
		return m, nil
	}
	if msg.Type == tea.KeyEnd || (msg.Type == tea.KeyRunes && string(msg.Runes) == "G" && m.input.Value() == "") {
		m.jumpToLatest()
		return m, nil
	}
	if msg.Type == tea.KeyCtrlN {
		m.openAddContactModal()
		return m, nil
	}
	if msg.Type == tea.KeyCtrlP {
		if m.peer.mailbox != "" {
			m.peerDetailOpen = true
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp:
		m.moveSelection(-1)
		return m, nil
	case tea.KeyDown:
		m.moveSelection(1)
		return m, nil
	case tea.KeyCtrlO:
		return m, m.handleAttachKey()
	case tea.KeyEnter:
		return m.handleEnterKey()
	default:
		return nil, nil
	}
}

func (m *Model) guardCanSend() error {
	switch {
	case m.conn.authFailed:
		return fmt.Errorf("cannot send: relay auth failed; restart with --relay-token")
	case m.peer.mailbox == "":
		return fmt.Errorf("select a contact from the sidebar first")
	case !m.conn.connected:
		return fmt.Errorf("relay is not connected; waiting to reconnect")
	default:
		return nil
	}
}

func (m *Model) handleAttachKey() tea.Cmd {
	if err := m.guardCanSend(); err != nil {
		level := ToastWarn
		if m.conn.authFailed {
			level = ToastBad
		}
		m.pushToast(err.Error(), level)
		return nil
	}
	if err := m.openFilePicker(); err != nil {
		m.pushToast(fmt.Sprintf("open file picker failed: %v", err), ToastBad)
	}
	return nil
}

func (m *Model) handleEnterKey() (*Model, tea.Cmd) {
	body := strings.TrimSpace(m.input.Value())
	if body == "" {
		previousRecipient := m.peer.mailbox
		if !m.activateSelectedContact() {
			return m, nil
		}
		return m, m.stopTypingCmd(previousRecipient)
	}
	if err := m.guardCanSend(); err != nil {
		level := ToastWarn
		if m.conn.authFailed {
			level = ToastBad
		}
		m.pushToast(err.Error(), level)
		return m, nil
	}
	if strings.HasPrefix(body, "/send-photo") {
		return m.handleAttachmentCommand("/send-photo", body, m.messaging.PreparePhotoOutgoing)
	}
	if strings.HasPrefix(body, "/send-voice") {
		return m.handleAttachmentCommand("/send-voice", body, m.messaging.PrepareVoiceOutgoing)
	}
	if strings.HasPrefix(body, "/send-file") {
		return m.handleAttachmentCommand("/send-file", body, m.messaging.PrepareFileOutgoing)
	}
	batch, err := m.messaging.EncryptOutgoing(m.peer.mailbox, body)
	if err != nil {
		m.pushToast(err.Error(), ToastBad)
		return m, nil
	}
	m.appendMessageItem(messageItem{
		direction: "outbound",
		sender:    m.mailbox,
		body:      body,
		timestamp: time.Now().UTC(),
		messageID: batchMessageID(batch),
		status:    statusPending,
	})
	m.input.SetValue("")
	m.resetLocalTypingState()
	m.syncViewportToBottom()
	return m, m.sendCmd(m.peer.mailbox, body, batch)
}

func (m *Model) handleClientEventMsg(msg clientEventMsg) (*Model, tea.Cmd) {
	event := transport.Event(msg)
	if event.Err != nil {
		return m, m.handleConnectionError(event.Err)
	}
	if event.Message != nil {
		m.handleProtocolMessage(*event.Message)
	}
	return m, m.waitForEvent()
}

func (m *Model) handleConnectResultMsg(err error) (*Model, tea.Cmd) {
	if err != nil {
		return m, m.handleConnectionError(err)
	}
	m.markConnected(fmt.Sprintf("connected as %s", m.mailbox))
	return m, m.waitForEvent()
}

func (m *Model) handleTypingTickMsg(msg typingTickMsg) (*Model, tea.Cmd) {
	now := time.Time(msg)
	if m.typing.peerVisible && !m.typing.peerExpiresAt.IsZero() && !now.Before(m.typing.peerExpiresAt) {
		m.clearPeerTyping()
	}
	if m.ui.toast != nil && !now.Before(m.ui.toast.expiresAt) {
		m.ui.toast = nil
	}
	var spCmd tea.Cmd
	if m.typing.peerVisible {
		m.typing.spinner, spCmd = m.typing.spinner.Update(spinner.TickMsg{Time: now})
	}
	var cmd tea.Cmd
	if m.typing.localSent && !m.typing.localAt.IsZero() && now.Sub(m.typing.localAt) >= typingIdleTimeout {
		cmd = m.sendTypingCmd(m.typing.localPeer, messaging.TypingStateIdle)
		m.resetLocalTypingState()
	}
	return m, tea.Batch(m.typingTickCmd(), spCmd, cmd)
}

func (m *Model) handleSendResultMsg(msg sendResultMsg) (*Model, tea.Cmd) {
	if msg.err != nil {
		m.updateMessageStatus(msg.messageID, statusFailed)
		m.syncViewport()
		m.pushToast(fmt.Sprintf("send failed: %v", msg.err), ToastBad)
		return m, nil
	}
	if err := m.messaging.SaveSent(msg.recipient, msg.messageID, msg.body); err != nil {
		m.pushToast(fmt.Sprintf("save history failed: %v", err), ToastBad)
		return m, nil
	}
	if msg.recipient == m.peer.mailbox {
		if !m.updateMessageStatus(msg.messageID, statusSent) {
			m.loadHistory()
		}
		m.syncViewport()
	}
	return m, nil
}

func (m *Model) handleTypingSendResultMsg(msg typingSendResultMsg) (*Model, tea.Cmd) {
	if msg.err != nil {
		m.pushToast(fmt.Sprintf("typing indicator failed: %v", msg.err), ToastBad)
	}
	return m, nil
}
