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
		return m, m.openPaletteAtHelp()
	}
	if msg.Type == tea.KeyEsc && m.recording.active && m.ui.focus == focusChat {
		return m, m.cancelVoiceRecordingCmd()
	}
	if msg.Type == tea.KeyEsc && m.pending != nil && m.ui.focus == focusChat {
		m.clearPendingAttachment()
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
	if msg.Type == tea.KeyHome && m.ui.focus == focusChat {
		m.msgs.followLatest = false
		m.viewport.GotoTop()
		return m, nil
	}
	if msg.Type == tea.KeyPgUp && m.ui.focus == focusChat {
		m.msgs.followLatest = false
		m.viewport.PageUp()
		return m, nil
	}
	if msg.Type == tea.KeyPgDown && m.ui.focus == focusChat {
		m.viewport.PageDown()
		m.msgs.followLatest = m.viewport.AtBottom()
		if m.msgs.followLatest {
			m.msgs.pendingIncoming = 0
		}
		return m, nil
	}
	if msg.Type == tea.KeyCtrlP {
		return m, m.openCommandPalette()
	}
	switch msg.Type {
	case tea.KeyUp:
		if m.ui.focus == focusChat && m.input.Value() == "" && m.browseDraftHistory(-1) {
			return m, nil
		}
		if m.ui.focus == focusChat {
			m.msgs.followLatest = false
			m.viewport.LineUp(1)
			return m, nil
		}
		m.moveSelection(-1)
		return m, nil
	case tea.KeyDown:
		if m.ui.focus == focusChat && m.input.Value() == "" && m.browseDraftHistory(1) {
			return m, nil
		}
		if m.ui.focus == focusChat {
			m.viewport.LineDown(1)
			m.msgs.followLatest = m.viewport.AtBottom()
			if m.msgs.followLatest {
				m.msgs.pendingIncoming = 0
			}
			return m, nil
		}
		m.moveSelection(1)
		return m, nil
	case tea.KeyEnter:
		if msg.String() == "shift+enter" || msg.Alt {
			return nil, nil
		}
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
	case m.peer.isRoom && !m.peer.joined:
		return fmt.Errorf("join %s first", m.peer.label)
	case m.conn.idleDisconnected:
		return nil
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
	if body == "" && m.recording.active {
		return m, m.stopVoiceRecordingCmd()
	}
	if body == "" && m.pending != nil {
		if err := m.guardCanSend(); err != nil {
			level := ToastWarn
			if m.conn.authFailed {
				level = ToastBad
			}
			m.pushToast(err.Error(), level)
			return m, nil
		}
		cmd := m.consumePendingAttachment()
		m.resetLocalTypingState()
		return m, cmd
	}
	if body == "" {
		previousRecipient := m.peer.mailbox
		if !m.activateSelectedContact() {
			return m, nil
		}
		if m.peer.isRoom && !m.peer.joined {
			state, batch, err := m.messaging.JoinDefaultRoom()
			if err != nil {
				m.pushToast(err.Error(), ToastBad)
				return m, nil
			}
			m.syncRoomContact(state)
			m.loadHistory()
			m.appendEventItem(time.Now().UTC(), fmt.Sprintf("joined %s", messaging.DefaultRoomLabel()), "room")
			m.pushToast(fmt.Sprintf("joined %s", messaging.DefaultRoomLabel()), ToastInfo)
			return m, tea.Batch(m.sendRoomCmd(m.peer.mailbox, "", batch), m.sendRoomHistorySyncCmd())
		}
		return m, tea.Batch(m.stopTypingCmd(previousRecipient), m.sendRoomHistorySyncCmd())
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
	m.rememberDraft(body)
	if m.peer.isRoom {
		batch, err := m.messaging.EncryptDefaultRoomOutgoing(body)
		if err != nil {
			m.pushToast(err.Error(), ToastBad)
			return m, nil
		}
		now := time.Now().UTC()
		m.appendMessageItem(messageItem{kind: transcriptMessage, direction: "outbound", sender: m.messaging.Identity().AccountID, body: body, timestamp: now, messageID: batchMessageID(batch), status: statusPending, expiresAt: m.outgoingItemExpiresAt(now)})
		m.input.SetValue("")
		m.syncComposer()
		m.resetLocalTypingState()
		m.syncViewportToBottom()
		return m, m.sendRoomCmd(m.peer.mailbox, body, batch)
	}
	batch, err := m.messaging.EncryptOutgoing(m.peer.mailbox, body)
	if err != nil {
		m.pushToast(err.Error(), ToastBad)
		return m, nil
	}
	now := time.Now().UTC()
	m.appendMessageItem(messageItem{
		kind:      transcriptMessage,
		direction: "outbound",
		sender:    m.mailbox,
		body:      body,
		timestamp: now,
		messageID: batchMessageID(batch),
		status:    statusPending,
		expiresAt: m.outgoingItemExpiresAt(now),
	})
	m.input.SetValue("")
	m.syncComposer()
	m.resetLocalTypingState()
	m.syncViewportToBottom()
	return m, m.sendCmd(m.peer.mailbox, body, batch)
}

func (m *Model) handleClientEventMsg(msg clientEventMsg) (*Model, tea.Cmd) {
	if msg.client != m.client {
		return m, nil
	}
	event := msg.event
	if event.Err != nil {
		return m, m.handleConnectionError(event.Err)
	}
	if event.Message != nil {
		m.noteActivity(time.Now().UTC())
		m.handleProtocolMessage(*event.Message)
	}
	return m, m.waitForEvent()
}

func (m *Model) handleConnectResultMsg(client transport.Client, err error) (*Model, tea.Cmd) {
	if client != m.client {
		return m, nil
	}
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
	if m.purgeExpiredTranscript(now) {
		m.renderMessages()
		m.syncViewport()
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
	if m.roomSync.active && !m.roomSync.startedAt.IsZero() && now.Sub(m.roomSync.startedAt) >= 10*time.Second {
		m.clearRoomSync()
		m.pushToast("room history sync timed out", ToastWarn)
	}
	var idleCmd tea.Cmd
	if m.conn.connected && m.conn.idleTimeout > 0 && !m.conn.lastActivityAt.IsZero() && now.Sub(m.conn.lastActivityAt) >= m.conn.idleTimeout {
		m.markIdleDisconnected(now)
		idleCmd = m.idleDisconnectCmd()
	}
	return m, tea.Batch(m.typingTickCmd(), spCmd, cmd, idleCmd)
}

func (m *Model) handleSendResultMsg(msg sendResultMsg) (*Model, tea.Cmd) {
	if msg.err != nil {
		m.updateMessageStatus(msg.messageID, statusFailed)
		m.syncViewport()
		m.appendEventItem(time.Now().UTC(), fmt.Sprintf("send failed: %v", msg.err), "error")
		m.pushToast(fmt.Sprintf("send failed: %v", msg.err), ToastBad)
		return m, nil
	}
	if msg.reconnected {
		m.markConnected(fmt.Sprintf("connected as %s", m.mailbox))
	}
	m.noteActivity(time.Now().UTC())
	if msg.roomID != "" {
		if msg.body != "" {
			if err := m.messaging.SaveDefaultRoomSent(msg.messageID, msg.body); err != nil {
				m.pushToast(fmt.Sprintf("save room history failed: %v", err), ToastBad)
				return m, nil
			}
		}
		if msg.roomID == m.peer.mailbox {
			if msg.messageID != "" && !m.updateMessageStatus(msg.messageID, statusSent) {
				m.loadHistory()
			}
			m.syncViewport()
		}
		return m, nil
	}
	if err := m.messaging.SaveSent(msg.recipient, msg.messageID, msg.body, msg.attachment); err != nil {
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
		return m, nil
	}
	m.noteActivity(time.Now().UTC())
	return m, nil
}

func (m *Model) handleIdleDisconnectResultMsg(msg idleDisconnectResultMsg) (*Model, tea.Cmd) {
	if msg.err != nil {
		m.pushToast(fmt.Sprintf("idle disconnect failed: %v", msg.err), ToastBad)
	}
	return m, nil
}

func (m *Model) handleRoomHistorySyncResultMsg(msg roomHistorySyncResultMsg) (*Model, tea.Cmd) {
	if msg.err != nil {
		m.clearRoomSync()
		m.pushToast(fmt.Sprintf("room history sync failed: %v", msg.err), ToastBad)
		return m, nil
	}
	if msg.skipped != "" {
		m.clearRoomSync()
		m.pushToast(msg.skipped, ToastInfo)
		return m, nil
	}
	if msg.requestID != "" {
		m.appendEventItem(time.Now().UTC(), "syncing recent room history", "sync")
		m.pushToast("syncing recent room history...", ToastInfo)
	}
	return m, nil
}
