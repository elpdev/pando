package chat

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/transport"
)

type Model struct {
	client             transport.Client
	messaging          *messaging.Service
	mailbox            string
	recipientMailbox   string
	relayURL           string
	relayToken         string
	relayClient        RelayClient
	relayClientFactory func(url, token string) (RelayClient, error)
	input              textinput.Model
	viewport           viewport.Model
	contacts           []contactItem
	selectedIndex      int
	messageItems       []messageItem
	messages           []string
	status             string
	connecting         bool
	disconnected       bool
	connected          bool
	authFailed         bool
	reconnectAttempt   int
	reconnectDelay     time.Duration
	peerFingerprint    string
	peerVerified       bool
	peerTrustSource    string
	typing             typingState
	filePicker         filePickerState
	addContact         addContactState
	helpOpen           bool
	peerDetailOpen     bool
	focus              focusState
	pendingIncoming    int
	unread             map[string]int
	toast              *toastState
	width              int
	height             int
	sidebarWidth       int
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
		client:             deps.Client,
		messaging:          deps.Messaging,
		mailbox:            deps.Mailbox,
		recipientMailbox:   deps.RecipientMailbox,
		relayURL:           deps.RelayURL,
		relayToken:         deps.RelayToken,
		relayClientFactory: factory,
		input:              input,
		viewport:           vp,
		typing:             typingState{spinner: newTypingSpinner()},
		status:             fmt.Sprintf("connecting as %s", deps.Mailbox),
		connecting:         true,
		selectedIndex:      -1,
		filePicker:         filePickerState{dir: defaultFilePickerDir()},
		unread:             map[string]int{},
	}
	m.loadContacts(deps.RecipientMailbox)
	m.syncRecipientDetails()
	m.syncInputPlaceholder()
	return m
}

func defaultRelayClientFactory(url, token string) (RelayClient, error) {
	return relayapi.NewClient(url, token)
}

// ensureRelayClient builds the relay client on demand. Returns an error if no
// relay URL is configured — callers should gate relay-dependent flows before
// reaching this point.
func (m *Model) ensureRelayClient() (RelayClient, error) {
	if m.relayClient != nil {
		return m.relayClient, nil
	}
	if strings.TrimSpace(m.relayURL) == "" {
		return nil, fmt.Errorf("no relay configured")
	}
	client, err := m.relayClientFactory(m.relayURL, m.relayToken)
	if err != nil {
		return nil, err
	}
	m.relayClient = client
	return client, nil
}

func (m *Model) Init() tea.Cmd {
	m.loadHistory()
	return tea.Batch(m.connectCmd(), m.waitForEvent(), m.typingTickCmd())
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.updateLayout()
	m.syncViewport()
}

func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.helpOpen {
			return m.handleHelpKey(msg)
		}
		if m.filePicker.open {
			return m, m.updateFilePicker(msg)
		}
		if m.addContact.open {
			return m.handleAddContactKey(msg)
		}
		// Help and focus-switching live above the input so `?` always works
		// (including while typing) and `tab` doesn't get eaten by textinput.
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
			// Absorb other keys so the chat input doesn't process them while
			// the drawer is visible.
			return m, nil
		}
		if msg.Type == tea.KeyCtrlN {
			m.openAddContactModal()
			return m, nil
		}
		// ctrl+p: "peer" — toggle the peer detail drawer. (ctrl+i is a
		// terminal synonym for tab, so we use a different binding.)
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
			if m.authFailed {
				m.pushToast("cannot attach: relay auth failed; restart with --relay-token", ToastBad)
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
			if err := m.openFilePicker(); err != nil {
				m.pushToast(fmt.Sprintf("open file picker failed: %v", err), ToastBad)
				return m, nil
			}
			return m, nil
		case tea.KeyEnter:
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
	case clientEventMsg:
		event := transport.Event(msg)
		if event.Err != nil {
			return m, m.handleConnectionError(event.Err)
		}
		if event.Message != nil {
			m.handleProtocolMessage(*event.Message)
		}
		return m, m.waitForEvent()
	case connectResultMsg:
		if msg.err != nil {
			return m, m.handleConnectionError(msg.err)
		}
		m.markConnected(fmt.Sprintf("connected as %s", m.mailbox))
		return m, m.waitForEvent()
	case reconnectResultMsg:
		if msg.err != nil {
			return m, m.handleConnectionError(msg.err)
		}
		m.markConnected(fmt.Sprintf("connected as %s", m.mailbox))
		return m, m.waitForEvent()
	case typingTickMsg:
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
	case sendResultMsg:
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
				// Optimistic item wasn't found (e.g. the chat changed mid-send);
				// fall back to a full reload from disk.
				m.loadHistory()
			}
			m.syncViewport()
		}
		return m, nil
	case typingSendResultMsg:
		if msg.err != nil {
			m.pushToast(fmt.Sprintf("typing indicator failed: %v", msg.err), ToastBad)
		}
		return m, nil
	case addContactResultMsg:
		m.addContact.busy = false
		m.addContact.cancel = nil
		if msg.err != nil {
			m.addContact.error = msg.err.Error()
			return m, nil
		}
		m.finishAddContact(msg.contact, fmt.Sprintf("added verified contact %s", msg.contact.AccountID))
		return m, nil
	case lookupContactResultMsg:
		m.addContact.busy = false
		m.addContact.cancel = nil
		if msg.err != nil {
			m.addContact.error = msg.err.Error()
			return m, nil
		}
		m.finishAddContact(msg.contact, fmt.Sprintf("added relay-directory contact %s", msg.contact.AccountID))
		return m, nil
	case inviteExchangeResultMsg:
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
	case inviteStartedMsg:
		if msg.err != nil {
			m.addContact.busy = false
			m.addContact.cancel = nil
			m.addContact.error = msg.err.Error()
			return m, nil
		}
		m.addContact.code = msg.code
		return m, nil
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
	return m.status
}

// ConnectionState returns the coarse connection state. The App header uses
// this to pick a pill color and glyph; Status() supplies the accompanying
// detail text when one is useful.
func (m *Model) ConnectionState() ConnState {
	switch {
	case m.authFailed:
		return ConnAuthFailed
	case m.disconnected:
		return ConnDisconnected
	case m.connecting && m.reconnectAttempt > 0:
		return ConnReconnecting
	case m.connecting:
		return ConnConnecting
	case m.connected:
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
	return m.reconnectDelay
}

func (m *Model) Mailbox() string {
	return m.mailbox
}

func (m *Model) RecipientMailbox() string {
	return m.recipientMailbox
}

func (m *Model) PeerFingerprint() string {
	return m.peerFingerprint
}

func (m *Model) PeerVerified() bool {
	return m.peerVerified
}

// Toast returns the current ephemeral message and its level, or empty string
// if no toast is active.
func (m *Model) Toast() (string, ToastLevel) {
	if m.toast == nil {
		return "", ToastInfo
	}
	return m.toast.text, m.toast.level
}

// pushToast posts an ephemeral message to the toast slot. The message
// persists for toastLifetime; after that the next typing tick clears it.
func (m *Model) pushToast(text string, level ToastLevel) {
	if text == "" {
		m.toast = nil
		return
	}
	m.toast = &toastState{
		text:      text,
		level:     level,
		expiresAt: time.Now().Add(toastLifetime),
	}
}

func (m *Model) Close() error {
	return m.client.Close()
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

// handleHelpKey closes the help overlay on ?, esc, q, or ctrl+c. Every other
// key is absorbed so the chat input doesn't receive keystrokes meant to
// dismiss the overlay.
func (m *Model) handleHelpKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.helpOpen = false
	case msg.Type == tea.KeyCtrlC:
		m.helpOpen = false
		return m, tea.Quit
	case msg.Type == tea.KeyRunes && (string(msg.Runes) == "?" || string(msg.Runes) == "q"):
		m.helpOpen = false
	}
	return m, nil
}

// toggleFocus flips which pane owns keyboard input. In wide mode this mostly
// affects the border color; in narrow mode it switches which pane is rendered.
func (m *Model) toggleFocus() {
	if m.focus == focusChat {
		m.focus = focusSidebar
		m.input.Blur()
	} else {
		m.focus = focusChat
		m.input.Focus()
	}
}

// jumpToLatest scrolls the viewport all the way down and clears the pending
// incoming-message counter that feeds the "↓ N new" pill.
func (m *Model) jumpToLatest() {
	m.viewport.GotoBottom()
	m.pendingIncoming = 0
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
		return
	}
	m.contacts = append(m.contacts, contactItem{Mailbox: contact.AccountID, Fingerprint: contact.Fingerprint(), Verified: contact.Verified, TrustSource: contact.TrustSource})
	if m.selectedIndex == -1 {
		m.selectedIndex = len(m.contacts) - 1
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
	batch, displayBody, err := prepare(m.recipientMailbox, path)
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
	return m, m.sendCmd(m.recipientMailbox, displayBody, batch)
}
