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
	if m.helpOpen {
		return m.handleHelpKey(msg)
	}
	if m.filePicker.open {
		return m, m.updateFilePicker(msg)
	}
	if m.addContact.open {
		return m.handleAddContactKey(msg)
	}
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
	if m.peerDetailOpen {
		if msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlP {
			m.peerDetailOpen = false
			return m, nil
		}
		return m, nil
	}
	if msg.Type == tea.KeyCtrlN {
		m.openAddContactModal()
		return m, nil
	}
	if msg.Type == tea.KeyCtrlP {
		if m.recipientMailbox != "" {
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

func (m *Model) handleAttachKey() tea.Cmd {
	if m.authFailed {
		m.pushToast("cannot attach: relay auth failed; restart with --relay-token", ToastBad)
		return nil
	}
	if m.recipientMailbox == "" {
		m.pushToast("select a contact from the sidebar first", ToastWarn)
		return nil
	}
	if !m.connected {
		m.pushToast("relay is not connected; waiting to reconnect", ToastWarn)
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
		previousRecipient := m.recipientMailbox
		if !m.activateSelectedContact() {
			return m, nil
		}
		return m, m.stopTypingCmd(previousRecipient)
	}
	if m.authFailed {
		m.pushToast("cannot send: relay auth failed; restart with --relay-token", ToastBad)
		return m, nil
	}
	if m.recipientMailbox == "" {
		m.pushToast("select a contact from the sidebar first", ToastWarn)
		return m, nil
	}
	if !m.connected {
		m.pushToast("relay is not connected; waiting to reconnect", ToastWarn)
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
	batch, err := m.messaging.EncryptOutgoing(m.recipientMailbox, body)
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
	return m, m.sendCmd(m.recipientMailbox, body, batch)
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
	if m.toast != nil && !now.Before(m.toast.expiresAt) {
		m.toast = nil
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
	if msg.recipient == m.recipientMailbox {
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

func (m *Model) handleAddContactResultMsg(msg addContactResultMsg) (*Model, tea.Cmd) {
	m.addContact.busy = false
	m.addContact.cancel = nil
	if msg.err != nil {
		m.addContact.error = msg.err.Error()
		return m, nil
	}
	m.finishAddContact(msg.contact, fmt.Sprintf("added verified contact %s", msg.contact.AccountID))
	return m, nil
}

func (m *Model) handleLookupContactResultMsg(msg lookupContactResultMsg) (*Model, tea.Cmd) {
	m.addContact.busy = false
	m.addContact.cancel = nil
	if msg.err != nil {
		m.addContact.error = msg.err.Error()
		return m, nil
	}
	m.finishAddContact(msg.contact, fmt.Sprintf("added relay-directory contact %s", msg.contact.AccountID))
	return m, nil
}

func (m *Model) handleInviteExchangeResultMsg(msg inviteExchangeResultMsg) (*Model, tea.Cmd) {
	m.addContact.busy = false
	m.addContact.cancel = nil
	if msg.cancelled {
		m.addContact.error = "cancelled"
		return m, nil
	}
	if msg.err != nil {
		m.addContact.error = msg.err.Error()
		return m, nil
	}
	m.finishAddContact(msg.contact, fmt.Sprintf("added invite-code contact %s", msg.contact.AccountID))
	return m, nil
}

func (m *Model) handleInviteStartedMsg(msg inviteStartedMsg) (*Model, tea.Cmd) {
	if msg.err != nil {
		m.addContact.busy = false
		m.addContact.cancel = nil
		m.addContact.error = msg.err.Error()
		return m, nil
	}
	m.addContact.code = msg.code
	return m, nil
}
