package chat

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/transport"
)

type Model struct {
	client    transport.Client
	messaging *messaging.Service
	mailbox   string

	relay    relayState
	peer     peerState
	conn     connectionState
	msgs     messageState
	typing   typingState
	roomSync roomSyncState
	ui       uiState

	input    textinput.Model
	viewport viewport.Model

	contacts      []contactItem
	selectedIndex int

	filePicker     filePickerModel
	addContact     addContactModal
	helpOpen       bool
	peerDetailOpen bool
	unread         map[string]int
}

func New(deps Deps) *Model {
	input := textinput.New()
	input.Focus()
	input.CharLimit = 4096
	input.Prompt = "> "

	vp := viewport.New(0, 0)
	vp.SetContent("")

	factory := deps.RelayClientFactory
	if factory == nil {
		factory = defaultRelayClientFactory
	}
	m := &Model{
		client:    deps.Client,
		messaging: deps.Messaging,
		mailbox:   deps.Mailbox,
		relay: relayState{
			url:           deps.RelayURL,
			token:         deps.RelayToken,
			clientFactory: factory,
		},
		peer: peerState{mailbox: deps.RecipientMailbox},
		conn: connectionState{
			status:     fmt.Sprintf("connecting as %s", deps.Mailbox),
			connecting: true,
		},
		typing:        typingState{spinner: newTypingSpinner()},
		input:         input,
		viewport:      vp,
		selectedIndex: -1,
		filePicker:    newFilePickerModel(),
		unread:        map[string]int{},
	}
	m.addContact = newAddContactModal(addContactDeps{
		messaging:         deps.Messaging,
		ensureRelayClient: m.ensureRelayClient,
		relayConfigured:   m.relayConfigured,
	})
	m.loadContacts(deps.RecipientMailbox)
	m.syncRecipientDetails()
	m.syncInputPlaceholder()
	m.filePicker.SetSize(m.conversationWidth(), m.ui.height)
	return m
}

func defaultRelayClientFactory(url, token string) (RelayClient, error) {
	return relayapi.NewClient(url, token)
}

// ensureRelayClient builds the relay client on demand. Returns an error if no
// relay URL is configured — callers should gate relay-dependent flows before
// reaching this point.
func (m *Model) ensureRelayClient() (RelayClient, error) {
	if m.relay.client != nil {
		return m.relay.client, nil
	}
	if strings.TrimSpace(m.relay.url) == "" {
		return nil, fmt.Errorf("no relay configured")
	}
	client, err := m.relay.clientFactory(m.relay.url, m.relay.token)
	if err != nil {
		return nil, err
	}
	m.relay.client = client
	return client, nil
}

func (m *Model) Init() tea.Cmd {
	m.loadHistory()
	return tea.Batch(m.connectCmd(), m.waitForEvent(), m.typingTickCmd())
}

func (m *Model) SetSize(width, height int) {
	m.ui.width = width
	m.ui.height = height
	m.updateLayout()
	m.syncViewport()
}

func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	if handled, cmd := m.handleOverlays(msg); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if next, cmd := m.handleKeyMsg(msg); next != nil {
			return next, cmd
		}
	case addContactCompletedMsg:
		return m.handleAddContactCompletedMsg(msg)
	case addContactClosedMsg:
		return m.handleAddContactClosedMsg(msg)
	case filePickerClosedMsg:
		m.closeFilePicker()
		return m, nil
	case filePickerErrorMsg:
		m.pushToast(fmt.Sprintf("file picker failed: %v", msg.err), ToastBad)
		return m, nil
	case filePickerSelectedMsg:
		return m, m.sendAttachment(msg.path, messaging.AttachmentTypeFile)
	case clientEventMsg:
		return m.handleClientEventMsg(msg)
	case connectResultMsg:
		return m.handleConnectResultMsg(msg.err)
	case reconnectResultMsg:
		return m.handleConnectResultMsg(msg.err)
	case typingTickMsg:
		return m.handleTypingTickMsg(msg)
	case sendResultMsg:
		return m.handleSendResultMsg(msg)
	case typingSendResultMsg:
		return m.handleTypingSendResultMsg(msg)
	case roomHistorySyncResultMsg:
		return m.handleRoomHistorySyncResultMsg(msg)
	}

	previousValue := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	typingCmd := m.handleInputActivity(previousValue, m.input.Value())
	return m, tea.Batch(cmd, typingCmd)
}

func parseAttachmentPath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) >= 2 {
		switch {
		case path[0] == '"' && path[len(path)-1] == '"':
			if unquoted, err := strconv.Unquote(path); err == nil {
				path = unquoted
			} else {
				path = path[1 : len(path)-1]
			}
		case path[0] == '\'' && path[len(path)-1] == '\'':
			path = path[1 : len(path)-1]
		}
	}
	return strings.ReplaceAll(path, `\ `, " ")
}

// Status returns the persistent connection status line — connecting,
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

func (m *Model) PeerFingerprint() string {
	return m.peer.fingerprint
}

func (m *Model) PeerVerified() bool {
	return m.peer.verified
}

// Toast returns the current ephemeral message and its level, or empty string
// if no toast is active.
func (m *Model) Toast() (string, ToastLevel) {
	if m.ui.toast == nil {
		return "", ToastInfo
	}
	return m.ui.toast.text, m.ui.toast.level
}

// pushToast posts an ephemeral message to the toast slot. The message
// persists for toastLifetime; after that the next typing tick clears it.
func (m *Model) pushToast(text string, level ToastLevel) {
	if text == "" {
		m.ui.toast = nil
		return
	}
	m.ui.toast = &toastState{
		text:      text,
		level:     level,
		expiresAt: time.Now().Add(toastLifetime),
	}
}

func (m *Model) Close() error {
	return m.client.Close()
}

func (m *Model) handleOverlays(msg tea.Msg) (bool, tea.Cmd) {
	if m.addContact.open {
		if handled, cmd := m.addContact.Update(msg); handled {
			return true, cmd
		}
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if m.helpOpen {
		return true, m.handleHelpKey(keyMsg)
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
			return true, m.sendAttachment(next.path, messaging.AttachmentTypeFile)
		default:
			return true, func() tea.Msg { return next }
		}
	}
	if m.peerDetailOpen {
		return true, m.handlePeerDetailKey(keyMsg)
	}
	return false, nil
}

func (m *Model) sendCmd(recipient, body string, batch *messaging.OutgoingBatch) tea.Cmd {
	return func() tea.Msg {
		if batch == nil {
			return sendResultMsg{recipient: recipient, body: body}
		}
		for _, envelope := range batch.Envelopes {
			if err := m.client.Send(envelope); err != nil {
				return sendResultMsg{recipient: recipient, messageID: batch.MessageID, body: body, err: err}
			}
		}
		return sendResultMsg{recipient: recipient, messageID: batch.MessageID, body: body}
	}
}

func (m *Model) sendRoomCmd(roomID, body string, batch *messaging.OutgoingBatch) tea.Cmd {
	return func() tea.Msg {
		if batch == nil {
			return sendResultMsg{roomID: roomID, body: body}
		}
		for _, envelope := range batch.Envelopes {
			if err := m.client.Send(envelope); err != nil {
				return sendResultMsg{roomID: roomID, messageID: batch.MessageID, body: body, err: err}
			}
		}
		return sendResultMsg{roomID: roomID, messageID: batch.MessageID, body: body}
	}
}

func (m *Model) sendRoomHistorySyncCmd() tea.Cmd {
	if !m.peer.isRoom || !m.peer.joined || m.roomSync.active {
		return nil
	}
	batch, requestID, err := m.messaging.RequestDefaultRoomHistory()
	if err != nil {
		return func() tea.Msg { return roomHistorySyncResultMsg{err: err} }
	}
	if batch == nil || len(batch.Envelopes) == 0 {
		return func() tea.Msg { return roomHistorySyncResultMsg{skipped: "no room members available for history sync"} }
	}
	m.roomSync.active = true
	m.roomSync.requestID = requestID
	m.roomSync.startedAt = time.Now().UTC()
	m.roomSync.lastRequestedAt = m.roomSync.startedAt
	m.roomSync.syncedCount = 0
	return func() tea.Msg {
		for _, envelope := range batch.Envelopes {
			if err := m.client.Send(envelope); err != nil {
				return roomHistorySyncResultMsg{requestID: requestID, err: err}
			}
		}
		return roomHistorySyncResultMsg{requestID: requestID}
	}
}

func (m *Model) clearRoomSync() {
	m.roomSync.active = false
	m.roomSync.requestID = ""
	m.roomSync.startedAt = time.Time{}
	m.roomSync.syncedCount = 0
}

// handleHelpKey closes the help overlay on ?, esc, q, or ctrl+c. Every other
// key is absorbed so the chat input doesn't receive keystrokes meant to
// dismiss the overlay.
func (m *Model) handleHelpKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case msg.Type == tea.KeyEsc:
		m.helpOpen = false
	case msg.Type == tea.KeyCtrlC:
		m.helpOpen = false
		return tea.Quit
	case msg.Type == tea.KeyRunes && (string(msg.Runes) == "?" || string(msg.Runes) == "q"):
		m.helpOpen = false
	}
	return nil
}

func (m *Model) handlePeerDetailKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlP {
		m.peerDetailOpen = false
	}
	return nil
}

// toggleFocus flips which pane owns keyboard input. In wide mode this mostly
// affects the border color; in narrow mode it switches which pane is rendered.
func (m *Model) toggleFocus() {
	if m.ui.focus == focusChat {
		m.ui.focus = focusSidebar
		m.input.Blur()
	} else {
		m.ui.focus = focusChat
		m.input.Focus()
	}
}

// jumpToLatest scrolls the viewport all the way down and clears the pending
// incoming-message counter that feeds the "↓ N new" pill.
func (m *Model) jumpToLatest() {
	m.viewport.GotoBottom()
	m.msgs.pendingIncoming = 0
}

func (m *Model) upsertContact(contact *identity.Contact) {
	if contact == nil {
		return
	}
	for idx := range m.contacts {
		if m.contacts[idx].Mailbox != contact.AccountID {
			continue
		}
		m.contacts[idx].Fingerprint = contact.Fingerprint()
		m.contacts[idx].Verified = contact.Verified
		m.contacts[idx].TrustSource = contact.TrustSource
		m.contacts[idx].Label = contact.AccountID
		return
	}
	m.contacts = append(m.contacts, contactItem{Mailbox: contact.AccountID, Label: contact.AccountID, Fingerprint: contact.Fingerprint(), Verified: contact.Verified, TrustSource: contact.TrustSource})
	if m.selectedIndex == -1 {
		m.selectedIndex = len(m.contacts) - 1
	}
}

func (m *Model) syncRoomContact(state *store.RoomState) {
	if state == nil {
		return
	}
	for idx := range m.contacts {
		if !m.contacts[idx].IsRoom {
			continue
		}
		m.contacts[idx].Joined = state.Joined
		m.contacts[idx].MemberCount = len(state.Members)
		m.contacts[idx].Label = messaging.DefaultRoomLabel()
		if m.peer.isRoom && m.peer.mailbox == state.ID {
			m.peer.label = messaging.DefaultRoomLabel()
			m.peer.joined = state.Joined
			m.peer.memberCount = len(state.Members)
			m.syncInputPlaceholder()
		}
		return
	}
}

func (m *Model) findContact(mailbox string) *contactItem {
	for idx := range m.contacts {
		if m.contacts[idx].Mailbox == mailbox {
			return &m.contacts[idx]
		}
	}
	return nil
}

func verificationLabel(verified bool, trustSource string) string {
	return identity.TrustLabel(trustSource, verified)
}

func (m *Model) handleAttachmentCommand(prefix, body string, prepare func(string, string) (*messaging.OutgoingBatch, string, error)) (*Model, tea.Cmd) {
	path := parseAttachmentPath(strings.TrimSpace(strings.TrimPrefix(body, prefix)))
	if path == "" {
		m.pushToast(fmt.Sprintf("usage: %s <path>", prefix), ToastWarn)
		return m, nil
	}
	batch, displayBody, err := prepare(m.peer.mailbox, path)
	if err != nil {
		m.pushToast(err.Error(), ToastBad)
		return m, nil
	}
	m.appendMessageItem(messageItem{
		direction:    "outbound",
		sender:       m.mailbox,
		body:         displayBody,
		timestamp:    time.Now().UTC(),
		messageID:    batchMessageID(batch),
		status:       statusPending,
		isAttachment: true,
	})
	m.input.SetValue("")
	m.resetLocalTypingState()
	m.syncViewportToBottom()
	return m, m.sendCmd(m.peer.mailbox, displayBody, batch)
}
