package chat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/transport"
	"github.com/elpdev/pando/internal/ui/style"
)

type Deps struct {
	Client           transport.Client
	Messaging        *messaging.Service
	Mailbox          string
	RecipientMailbox string
	RelayURL         string
}

type contactItem struct {
	Mailbox     string
	Fingerprint string
	Verified    bool
}

// messageItem is one rendered chat message. We keep these as structured records
// so the grouped renderer can reason about sender/time/delivery state without
// having to parse strings.
type messageItem struct {
	direction    string // "outbound" | "inbound"
	sender       string // mailbox that authored the message
	body         string
	timestamp    time.Time
	messageID    string
	status       deliveryStatus
	isAttachment bool
}

// deliveryStatus is a four-state outbound lifecycle. Inbound messages ignore
// it.
type deliveryStatus int

const (
	statusPending   deliveryStatus = iota // optimistic local append, awaiting relay round-trip
	statusSent                            // send succeeded; waiting for recipient ack
	statusDelivered                       // peer acked
	statusFailed                          // send returned an error
)

type filePickerEntry struct {
	Name  string
	Path  string
	IsDir bool
}

type Model struct {
	client              transport.Client
	messaging           *messaging.Service
	mailbox             string
	recipientMailbox    string
	relayURL            string
	input               textinput.Model
	viewport            viewport.Model
	contacts            []contactItem
	selectedIndex       int
	messageItems        []messageItem
	messages            []string
	status              string
	connecting          bool
	disconnected        bool
	connected           bool
	authFailed          bool
	reconnectAttempt    int
	reconnectDelay      time.Duration
	peerFingerprint     string
	peerVerified        bool
	peerTyping          bool
	peerTypingExpiresAt time.Time
	typingSpinner       spinner.Model
	localTypingSent     bool
	localTypingPeer     string
	localTypingAt       time.Time
	filePickerOpen      bool
	filePickerDir       string
	filePickerEntries   []filePickerEntry
	filePickerSelected  int
	addContactValue     string
	addContactError     string
	addContactImporting bool
	addContactOpen      bool
	helpOpen            bool
	focus               focusState
	pendingIncoming     int
	unread              map[string]int
	toast               *toastState
	width               int
	height              int
	sidebarWidth        int
}

// focusState tracks which pane owns keyboard input. In wide mode both panes
// are visible and focus only decorates borders + directs ↑/↓; in narrow mode
// only the focused pane renders.
type focusState int

const (
	focusChat    focusState = iota // input + viewport + conversation
	focusSidebar                   // contact list
)

// narrowThreshold is the terminal width below which the sidebar and
// conversation can't coexist comfortably. Below this, only the focused pane
// renders.
const narrowThreshold = 60

// ConnState is the coarse connection state used by the app header to pick a
// glyph and color. Call ConnectionState() to read it.
type ConnState int

const (
	ConnConnecting ConnState = iota
	ConnConnected
	ConnReconnecting
	ConnDisconnected
	ConnAuthFailed
)

// ToastLevel controls the color of an ephemeral message shown below the
// viewport.
type ToastLevel int

const (
	ToastInfo ToastLevel = iota
	ToastWarn
	ToastBad
)

type toastState struct {
	text      string
	level     ToastLevel
	expiresAt time.Time
}

const toastLifetime = 3 * time.Second

type clientEventMsg transport.Event
type connectResultMsg struct{ err error }
type reconnectResultMsg struct{ err error }
type typingTickMsg time.Time
type typingSendResultMsg struct{ err error }
type addContactResultMsg struct {
	contact *identity.Contact
	err     error
}
type sendResultMsg struct {
	recipient string
	messageID string
	body      string
	err       error
}

const (
	typingAnimationInterval = 350 * time.Millisecond
	typingIdleTimeout       = 2 * time.Second
	typingStateActive       = "active"
	typingStateIdle         = "idle"
	attachmentModePhoto     = "photo"
	attachmentModeVoice     = "voice"
	attachmentModeFile      = "file"
	addContactLimit         = 16384
)

func New(deps Deps) *Model {
	input := textinput.New()
	input.Focus()
	input.CharLimit = 4096
	input.Prompt = "> "

	vp := viewport.New(0, 0)
	vp.SetContent("")

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Muted

	m := &Model{
		client:           deps.Client,
		messaging:        deps.Messaging,
		mailbox:          deps.Mailbox,
		recipientMailbox: deps.RecipientMailbox,
		relayURL:         deps.RelayURL,
		input:            input,
		viewport:         vp,
		typingSpinner:    sp,
		status:           fmt.Sprintf("connecting as %s", deps.Mailbox),
		connecting:       true,
		selectedIndex:    -1,
		filePickerDir:    defaultFilePickerDir(),
		unread:           map[string]int{},
	}
	m.loadContacts(deps.RecipientMailbox)
	m.syncRecipientDetails()
	m.syncInputPlaceholder()
	return m
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
		if m.filePickerOpen {
			return m, m.updateFilePicker(msg)
		}
		if m.addContactOpen {
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
		if msg.Type == tea.KeyCtrlN {
			m.openAddContactModal()
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
		if m.peerTyping && !m.peerTypingExpiresAt.IsZero() && !now.Before(m.peerTypingExpiresAt) {
			m.clearPeerTyping()
		}
		if m.toast != nil && !now.Before(m.toast.expiresAt) {
			m.toast = nil
		}
		var spCmd tea.Cmd
		if m.peerTyping {
			m.typingSpinner, spCmd = m.typingSpinner.Update(spinner.TickMsg{Time: now})
		}
		var cmd tea.Cmd
		if m.localTypingSent && !m.localTypingAt.IsZero() && now.Sub(m.localTypingAt) >= typingIdleTimeout {
			cmd = m.sendTypingCmd(m.localTypingPeer, typingStateIdle)
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
		m.addContactImporting = false
		if msg.err != nil {
			m.addContactError = msg.err.Error()
			return m, nil
		}
		m.upsertContact(msg.contact)
		m.selectContact(msg.contact.AccountID)
		m.activateSelectedContact()
		m.closeAddContactModal(true)
		m.pushToast(fmt.Sprintf("added verified contact %s", msg.contact.AccountID), ToastInfo)
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

func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	var view string
	if m.width < narrowThreshold {
		// Narrow terminals can't fit both panes. Render whichever pane has
		// focus; tab toggles between them.
		if m.focus == focusSidebar {
			view = m.renderSidebar()
		} else {
			view = m.renderConversation()
		}
	} else {
		left := m.renderSidebar()
		right := m.renderConversation()
		view = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	if m.helpOpen {
		return m.renderHelpModal(view)
	}
	if m.addContactOpen {
		return m.renderAddContactModal(view)
	}
	return view
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

func (m *Model) handleProtocolMessage(msg protocol.Message) {
	switch msg.Type {
	case protocol.MessageTypeAck:
		if m.connecting {
			m.markConnected(fmt.Sprintf("connected as %s", m.mailbox))
		}
	case protocol.MessageTypeIncoming:
		if msg.Incoming == nil {
			return
		}
		result, err := m.messaging.HandleIncoming(*msg.Incoming)
		if err != nil {
			m.pushToast(fmt.Sprintf("incoming message failed: %v", err), ToastBad)
			return
		}
		if result == nil || result.Duplicate {
			return
		}
		if len(result.AckEnvelopes) != 0 {
			for _, envelope := range result.AckEnvelopes {
				if err := m.client.Send(envelope); err != nil {
					m.pushToast(fmt.Sprintf("delivery ack failed: %v", err), ToastBad)
					break
				}
			}
		}
		if result.ContactUpdated != nil {
			m.upsertContact(result.ContactUpdated)
			if result.ContactUpdated.AccountID == m.recipientMailbox {
				m.syncRecipientDetails()
				m.pushToast(fmt.Sprintf("updated device bundle for %s", result.ContactUpdated.AccountID), ToastInfo)
			}
			return
		}
		if result.Control {
			if result.TypingState != "" {
				if result.PeerAccountID == m.recipientMailbox {
					if result.TypingState == typingStateActive {
						m.peerTyping = true
						m.peerTypingExpiresAt = result.TypingExpiresAt
						m.typingSpinner = spinner.New()
						m.typingSpinner.Spinner = spinner.Dot
						m.typingSpinner.Style = style.Muted
					} else {
						m.clearPeerTyping()
					}
				}
				return
			}
			if result.MessageID != "" {
				if result.PeerAccountID == m.recipientMailbox {
					if !m.updateMessageStatus(result.MessageID, statusDelivered) {
						m.loadHistory()
					}
					m.syncViewport()
				}
			}
			return
		}
		if err := m.messaging.SaveReceived(result.PeerAccountID, result.Body, msg.Incoming.Timestamp); err != nil {
			m.pushToast(fmt.Sprintf("save history failed: %v", err), ToastBad)
			return
		}
		if result.PeerAccountID == m.recipientMailbox {
			m.clearPeerTyping()
			m.appendMessageItem(messageItem{
				direction:    "inbound",
				sender:       msg.Incoming.SenderMailbox,
				body:         result.Body,
				timestamp:    msg.Incoming.Timestamp,
				messageID:    result.MessageID,
				isAttachment: attachmentBodyPattern(result.Body),
			})
			m.syncViewport()
			return
		}
		m.markUnread(result.PeerAccountID)
		m.pushToast(fmt.Sprintf("new message from %s", result.PeerAccountID), ToastInfo)
	case protocol.MessageTypeError:
		if msg.Error != nil {
			m.pushToast(fmt.Sprintf("relay error: %s", msg.Error.Message), ToastBad)
		}
	}
}

// syncViewport pushes the current m.messages slice into the viewport and
// preserves the user's scroll position when they have scrolled up. Callers
// that need to force the viewport to the latest message (chat switch,
// outbound send, end-key) should call syncViewportToBottom instead.
func (m *Model) syncViewport() {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		return
	}
	wasAtBottom := m.viewport.AtBottom()
	m.viewport.SetContent(strings.Join(m.messages, "\n"))
	if wasAtBottom {
		m.viewport.GotoBottom()
		m.pendingIncoming = 0
	}
}

// syncViewportToBottom sets the viewport content and always scrolls to the
// latest message, clearing the pending-incoming counter.
func (m *Model) syncViewportToBottom() {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		return
	}
	m.viewport.SetContent(strings.Join(m.messages, "\n"))
	m.viewport.GotoBottom()
	m.pendingIncoming = 0
}

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

func (m *Model) typingTickCmd() tea.Cmd {
	return tea.Tick(typingAnimationInterval, func(t time.Time) tea.Msg {
		return typingTickMsg(t)
	})
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

func (m *Model) sendTypingCmd(recipient, state string) tea.Cmd {
	if recipient == "" || m.authFailed || !m.connected {
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

func (m *Model) loadContacts(initialMailbox string) {
	contacts, err := m.messaging.Contacts()
	if err != nil {
		m.pushToast(fmt.Sprintf("load contacts failed: %v", err), ToastBad)
		return
	}
	m.contacts = make([]contactItem, 0, len(contacts))
	for _, contact := range contacts {
		m.contacts = append(m.contacts, contactItem{
			Mailbox:     contact.AccountID,
			Fingerprint: contact.Fingerprint(),
			Verified:    contact.Verified,
		})
	}
	m.selectedIndex = -1
	for idx := range m.contacts {
		if m.contacts[idx].Mailbox == initialMailbox {
			m.selectedIndex = idx
			return
		}
	}
	if len(m.contacts) != 0 {
		m.selectedIndex = 0
	}
}

func (m *Model) moveSelection(delta int) {
	if len(m.contacts) == 0 {
		return
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
		return
	}
	m.selectedIndex += delta
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
	if m.selectedIndex >= len(m.contacts) {
		m.selectedIndex = len(m.contacts) - 1
	}
}

func (m *Model) selectContact(mailbox string) {
	for idx := range m.contacts {
		if m.contacts[idx].Mailbox == mailbox {
			m.selectedIndex = idx
			return
		}
	}
}

func (m *Model) openAddContactModal() {
	m.addContactOpen = true
	m.addContactError = ""
	m.addContactImporting = false
	m.addContactValue = ""
	m.input.Blur()
}

func (m *Model) closeAddContactModal(keepStatus bool) {
	m.addContactOpen = false
	m.addContactImporting = false
	m.addContactError = ""
	m.addContactValue = ""
	if !keepStatus {
		m.pushToast("add contact cancelled", ToastInfo)
	}
	m.input.Focus()
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

func (m *Model) handleAddContactKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.closeAddContactModal(false)
		return m, nil
	case tea.KeyCtrlS:
		if m.addContactImporting {
			return m, nil
		}
		trimmed := strings.TrimSpace(m.addContactValue)
		if trimmed == "" {
			m.addContactError = "invite input is empty"
			return m, nil
		}
		m.addContactError = ""
		m.addContactImporting = true
		return m, m.importContactCmd(trimmed)
	case tea.KeyEnter, tea.KeyCtrlJ:
		m.appendAddContactText("\n")
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
		m.deleteAddContactRune()
		return m, nil
	case tea.KeyCtrlU:
		m.addContactValue = ""
		m.addContactError = ""
		return m, nil
	case tea.KeyRunes:
		m.appendAddContactText(string(msg.Runes))
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) appendAddContactText(text string) {
	if text == "" || len([]rune(m.addContactValue)) >= addContactLimit {
		return
	}
	remaining := addContactLimit - len([]rune(m.addContactValue))
	runes := []rune(text)
	if len(runes) > remaining {
		runes = runes[:remaining]
	}
	m.addContactValue += string(runes)
	m.addContactError = ""
}

func (m *Model) deleteAddContactRune() {
	runes := []rune(m.addContactValue)
	if len(runes) == 0 {
		return
	}
	m.addContactValue = string(runes[:len(runes)-1])
	m.addContactError = ""
}

func (m *Model) importContactCmd(text string) tea.Cmd {
	return func() tea.Msg {
		contact, err := m.messaging.ImportContactInviteText(text, true)
		if err != nil {
			return addContactResultMsg{err: err}
		}
		return addContactResultMsg{contact: contact}
	}
}

func (m *Model) activateSelectedContact() bool {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.contacts) {
		return false
	}
	m.recipientMailbox = m.contacts[m.selectedIndex].Mailbox
	m.clearUnread(m.recipientMailbox)
	m.syncRecipientDetails()
	m.clearPeerTyping()
	m.loadHistory()
	m.syncViewportToBottom()
	m.syncInputPlaceholder()
	m.focus = focusChat
	m.input.Focus()
	return true
}

// markUnread increments the unread count for a peer. No-op for the currently
// open chat (those messages are visible already).
func (m *Model) markUnread(peer string) {
	if peer == "" || peer == m.recipientMailbox {
		return
	}
	if m.unread == nil {
		m.unread = map[string]int{}
	}
	m.unread[peer]++
}

// clearUnread resets the unread count for a peer. Called on chat open.
func (m *Model) clearUnread(peer string) {
	if m.unread == nil {
		return
	}
	delete(m.unread, peer)
}

// Unread returns the unread-message count for a peer, or zero.
func (m *Model) Unread(peer string) int {
	if m.unread == nil {
		return 0
	}
	return m.unread[peer]
}

func (m *Model) loadHistory() {
	m.messageItems = nil
	m.messages = nil
	if m.recipientMailbox == "" {
		m.syncViewport()
		return
	}
	records, err := m.messaging.History(m.recipientMailbox)
	if err != nil {
		m.pushToast(fmt.Sprintf("load history failed: %v", err), ToastBad)
		return
	}
	for _, record := range records {
		item := messageItem{
			direction:    record.Direction,
			body:         record.Body,
			timestamp:    record.Timestamp,
			messageID:    record.MessageID,
			isAttachment: attachmentBodyPattern(record.Body),
		}
		if record.Direction == "outbound" {
			item.sender = m.mailbox
			item.status = statusSent
			if record.Delivered {
				item.status = statusDelivered
			}
		} else {
			item.sender = record.PeerMailbox
		}
		m.messageItems = append(m.messageItems, item)
	}
	if len(m.messageItems) == 0 {
		m.viewport.SetContent(style.Muted.Render("No messages yet."))
		return
	}
	m.renderMessages()
	m.syncViewportToBottom()
}

// appendMessageItem appends a new message and refreshes the derived string
// slice. Used by the optimistic-append paths (enter-to-send, attachments,
// incoming messages in the active chat). Incoming messages that arrive while
// the user has scrolled up feed the "↓ N new" pill counter.
func (m *Model) appendMessageItem(item messageItem) {
	wasAtBottom := m.viewport.AtBottom()
	m.messageItems = append(m.messageItems, item)
	m.renderMessages()
	if item.direction == "inbound" && !wasAtBottom {
		m.pendingIncoming++
	}
}

// renderMessages rebuilds m.messages from m.messageItems. Consecutive messages
// from the same sender within groupGap are collapsed under a single
// "name · HH:MM PM" header. Outbound messages show a right-aligned delivery
// glyph.
func (m *Model) renderMessages() {
	const groupGap = 5 * time.Minute
	m.messages = m.messages[:0]

	var prevSender string
	var prevTS time.Time
	for i, item := range m.messageItems {
		startGroup := i == 0 || item.sender != prevSender || item.timestamp.Sub(prevTS) > groupGap
		if startGroup {
			if i > 0 {
				m.messages = append(m.messages, "")
			}
			m.messages = append(m.messages, m.renderGroupHeader(item))
		}
		m.messages = append(m.messages, m.renderMessageBody(item))
		prevSender = item.sender
		prevTS = item.timestamp
	}
}

func (m *Model) renderGroupHeader(item messageItem) string {
	name := item.sender
	var nameStyled string
	if item.direction == "outbound" {
		nameStyled = style.Bold.Render("you")
	} else {
		nameStyled = style.PeerAccentStyle(m.peerFingerprint).Bold(true).Render(name)
	}
	ts := ""
	if !item.timestamp.IsZero() {
		ts = item.timestamp.Format(time.Kitchen)
	}
	suffix := style.Muted.Render(" " + style.GroupSep + " " + ts)
	return nameStyled + suffix
}

func (m *Model) renderMessageBody(item messageItem) string {
	bodyStyle := lipgloss.NewStyle()
	if item.isAttachment {
		bodyStyle = style.Italic
	}
	body := "  " + bodyStyle.Render(item.body)

	if item.direction != "outbound" {
		return body
	}
	glyph, glyphStyle := deliveryGlyphFor(item.status)
	if glyph == "" {
		return body
	}
	tick := " " + glyphStyle.Render(glyph)
	width := m.viewport.Width
	if width <= 0 {
		return body + tick
	}
	pad := width - lipgloss.Width(body) - lipgloss.Width(tick)
	if pad < 1 {
		pad = 1
	}
	return body + strings.Repeat(" ", pad) + tick
}

func batchMessageID(batch *messaging.OutgoingBatch) string {
	if batch == nil {
		return ""
	}
	return batch.MessageID
}

// updateMessageStatus flips the delivery status on a specific outgoing message
// and re-renders. Used by sendResultMsg (pending -> sent / failed) and by
// delivery-ack events (sent -> delivered).
func (m *Model) updateMessageStatus(messageID string, status deliveryStatus) bool {
	if messageID == "" {
		return false
	}
	for i := range m.messageItems {
		if m.messageItems[i].direction != "outbound" || m.messageItems[i].messageID != messageID {
			continue
		}
		if m.messageItems[i].status == status {
			return true
		}
		m.messageItems[i].status = status
		m.renderMessages()
		return true
	}
	return false
}

func deliveryGlyphFor(s deliveryStatus) (string, lipgloss.Style) {
	switch s {
	case statusPending:
		return style.GlyphDeliveryPending, style.DeliveryPending
	case statusSent:
		return style.GlyphDeliverySent, style.DeliverySent
	case statusDelivered:
		return style.GlyphDeliveryDelivered, style.DeliveryDelivered
	case statusFailed:
		return style.GlyphDeliveryFailed, style.DeliveryFailed
	}
	return "", lipgloss.NewStyle()
}

// attachmentBodyPattern returns true for messages that start with one of the
// attachment prefixes the TUI emits ("photo sent:", "voice note sent:",
// "file sent:"). Used to italicize attachment lines.
func attachmentBodyPattern(body string) bool {
	for _, prefix := range []string{"photo sent:", "voice note sent:", "file sent:"} {
		if strings.HasPrefix(body, prefix) {
			return true
		}
	}
	return false
}

func (m *Model) syncRecipientDetails() {
	m.peerFingerprint = "unknown"
	m.peerVerified = false
	if m.recipientMailbox == "" {
		return
	}
	contact := m.findContact(m.recipientMailbox)
	if contact == nil {
		if stored, err := m.messaging.Contact(m.recipientMailbox); err == nil {
			m.peerFingerprint = stored.Fingerprint()
			m.peerVerified = stored.Verified
		}
		return
	}
	m.peerFingerprint = contact.Fingerprint
	m.peerVerified = contact.Verified
}

func (m *Model) syncInputPlaceholder() {
	if m.authFailed {
		m.input.Placeholder = "Relay auth failed. Restart with --relay-token"
		return
	}
	if m.recipientMailbox == "" {
		m.input.Placeholder = "Select a contact to start chatting"
		return
	}
	m.input.Placeholder = fmt.Sprintf("Message %s", m.recipientMailbox)
}

func (m *Model) updateLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	// Narrow terminals render one pane at a time — the sidebar "width" for
	// layout bookkeeping is the full terminal so the conversation gets all of
	// it when focused.
	if m.width < narrowThreshold {
		m.sidebarWidth = m.width
		m.viewport.Width = max(1, m.width)
		m.viewport.Height = max(1, m.height-5)
		return
	}
	sidebarWidth := 28
	if m.width < 80 {
		sidebarWidth = max(20, m.width/3)
	}
	if sidebarWidth > m.width-20 {
		sidebarWidth = max(18, m.width/2)
	}
	m.sidebarWidth = sidebarWidth
	conversationWidth := max(1, m.width-m.sidebarWidth-1)
	m.viewport.Width = conversationWidth
	m.viewport.Height = max(1, m.height-5)
}

func (m *Model) renderSidebar() string {
	title := style.Bold.Render("Contacts")
	shortcut := "up/down browse  enter open  ctrl+n add  tab switch pane"
	if m.addContactOpen {
		shortcut = "add contact open  ctrl+s import  esc cancel"
	}
	lines := []string{title, style.Muted.Render(shortcut)}
	if len(m.contacts) == 0 {
		lines = append(lines, style.Muted.Render("No contacts. Press ctrl+n."))
	} else {
		for idx, contact := range m.contacts {
			lines = append(lines, m.renderSidebarRow(idx, contact))
		}
	}
	content := strings.Join(lines, "\n")
	// Narrow mode: sidebar owns the whole screen, no right border.
	if m.width < narrowThreshold {
		return lipgloss.NewStyle().Width(m.sidebarWidth).Height(max(1, m.height)).Render(content)
	}
	return style.SidebarBorder.Width(m.sidebarWidth).Height(max(1, m.height)).Render(content)
}

func (m *Model) renderSidebarRow(idx int, contact contactItem) string {
	isCursor := idx == m.selectedIndex
	isActive := contact.Mailbox == m.recipientMailbox

	// Leading marker column is always 2 characters wide so rows stay aligned:
	//   "▌●" cursor on active, "▌ " cursor only, " ●" active only, "  " none.
	cursorGlyph := " "
	if isCursor {
		cursorGlyph = style.PeerAccentStyle(contact.Fingerprint).Render(style.GlyphCursorRow)
	}
	activeGlyph := " "
	if isActive {
		activeGlyph = style.StatusOk.Render(style.GlyphActiveChat)
	}
	marker := cursorGlyph + activeGlyph

	// Mailbox takes peer accent + bold when active, default foreground otherwise.
	mailbox := contact.Mailbox
	if isActive {
		mailbox = style.PeerAccentStyle(contact.Fingerprint).Bold(true).Render(mailbox)
	}

	// Unread badge ("●N") rendered only when positive.
	badge := ""
	if n := m.Unread(contact.Mailbox); n > 0 {
		badge = " " + style.UnreadBadge.Render(fmt.Sprintf("%s%d", style.GlyphUnreadDot, n))
	}

	// Verification label on the right.
	statusStyle := style.UnverifiedWarn
	statusText := "unverified"
	if contact.Verified {
		statusStyle = style.VerifiedOk
		statusText = "verified"
	}

	return fmt.Sprintf("%s %s%s  %s", marker, mailbox, badge, statusStyle.Render(statusText))
}

func (m *Model) renderConversation() string {
	width := m.conversationWidth()
	if m.recipientMailbox == "" {
		empty := []string{
			style.Bold.Render("No chat selected"),
			style.Muted.Render("Pick a contact from the sidebar to load the conversation."),
			style.Muted.Render("Press ctrl+n to import a verified contact without leaving the TUI."),
			style.Muted.Render("Verified contacts are labeled directly in the roster."),
			"",
			m.input.View(),
		}
		return lipgloss.NewStyle().Width(width).Render(strings.Join(empty, "\n"))
	}
	if m.filePickerOpen {
		return m.renderFilePicker(width)
	}
	peerHeading := style.PeerAccentStyle(m.peerFingerprint).Bold(true).Render(m.recipientMailbox)
	header := []string{
		peerHeading,
		style.Muted.Render("ctrl+o attach file  |  ? help  |  tab switch pane"),
		m.viewport.View(),
		m.renderJumpPill(width),
		m.renderToast(),
		m.renderTypingIndicator(),
		m.input.View(),
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(header, "\n"))
}

// conversationWidth is the effective width of the conversation pane. In narrow
// mode the sidebar is hidden, so the pane gets the full terminal width.
func (m *Model) conversationWidth() int {
	if m.width < narrowThreshold {
		return max(1, m.width)
	}
	return max(1, m.width-m.sidebarWidth-1)
}

// renderJumpPill renders the "↓ N new" hint right-aligned to the conversation
// width. Empty string when no new messages have arrived while scrolled up.
func (m *Model) renderJumpPill(width int) string {
	if m.pendingIncoming <= 0 {
		return ""
	}
	pill := style.StatusInfo.Bold(true).Render(fmt.Sprintf("%s %d new  end", style.GlyphJumpToLatest, m.pendingIncoming))
	pad := width - lipgloss.Width(pill)
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + pill
}

func (m *Model) renderAddContactModal(base string) string {
	modalWidth := min(max(58, m.width*2/3), max(40, m.width-6))
	modalHeight := min(max(15, m.height*2/3), max(12, m.height-4))
	if modalWidth <= 0 || modalHeight <= 0 {
		return base
	}
	bodyWidth := max(24, modalWidth-6)
	inputHeight := max(5, modalHeight-10)
	title := style.Bright.Bold(true).Render("Add Contact")
	description := style.Dim.Width(bodyWidth).Render("Paste a raw invite code or the full invite text. Imported contacts are marked verified immediately and opened right away.")
	input := m.renderAddContactEditor(bodyWidth, inputHeight)
	footerText := "enter newline  ctrl+s import  ctrl+u clear  esc cancel"
	if m.addContactImporting {
		footerText = "importing contact..."
	}
	footer := style.Subtle.Render(footerText)
	parts := []string{title, description, input}
	if m.addContactError != "" {
		parts = append(parts, style.StatusBad.Width(bodyWidth).Render(m.addContactError))
	}
	parts = append(parts, footer)
	modal := style.Modal.Width(modalWidth).Padding(1, 2).Render(strings.Join(parts, "\n\n"))
	background := style.Faint.Render(base)
	return strings.Join([]string{background, lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)}, "\n")
}

func (m *Model) renderAddContactEditor(width, height int) string {
	content := m.addContactValue
	if content == "" {
		content = style.Muted.Render("account: alice\nfingerprint: ...\ninvite-code: ...")
	} else {
		content += style.CursorBlock.Render("█")
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	visible := strings.Join(lines, "\n")
	meta := style.Subtle.Render(fmt.Sprintf("%d chars", len([]rune(m.addContactValue))))
	if len(m.addContactValue) >= addContactLimit {
		meta = style.StatusBad.Render(fmt.Sprintf("input limit reached (%d chars)", addContactLimit))
	}
	box := style.InputBorder.Width(width).Height(height).Padding(0, 1).Render(visible)
	return strings.Join([]string{box, meta}, "\n")
}

// helpShortcut is one entry in the help overlay — a key label and a brief
// description. Kept as data rather than pre-formatted strings so the overlay
// can pad the two columns consistently.
type helpShortcut struct {
	keys string
	desc string
}

var helpSectionNavigation = []helpShortcut{
	{"↑ ↓", "browse contacts"},
	{"⏎", "open selected chat / send"},
	{"tab", "switch pane"},
	{"end / G", "jump to latest message"},
	{"ctrl+c", "quit"},
}

var helpSectionMessaging = []helpShortcut{
	{"ctrl+n", "add contact"},
	{"ctrl+o", "attach file"},
	{"/send-photo <path>", "attach photo via path"},
	{"/send-voice <path>", "attach voice via path"},
	{"/send-file <path>", "attach file via path"},
	{"ctrl+u", "clear input"},
	{"?", "toggle this help"},
	{"esc", "close overlay"},
}

func (m *Model) renderHelpModal(base string) string {
	modalWidth := min(max(64, m.width*2/3), max(40, m.width-6))
	modalHeight := min(max(18, m.height*2/3), max(14, m.height-4))
	if modalWidth <= 0 || modalHeight <= 0 {
		return base
	}
	colWidth := max(20, (modalWidth-6)/2)

	title := style.Bright.Bold(true).Render("Help")
	navTitle := style.Bold.Render("Navigation")
	msgTitle := style.Bold.Render("Messaging")
	nav := renderHelpColumn(helpSectionNavigation, colWidth)
	msg := renderHelpColumn(helpSectionMessaging, colWidth)
	columns := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{navTitle, nav}, "\n")),
		"  ",
		lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{msgTitle, msg}, "\n")),
	)
	footer := style.Subtle.Render("? or esc to close")
	body := strings.Join([]string{title, columns, footer}, "\n\n")
	modal := style.Modal.Width(modalWidth).Padding(1, 2).Render(body)
	background := style.Faint.Render(base)
	return strings.Join([]string{background, lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)}, "\n")
}

func renderHelpColumn(entries []helpShortcut, width int) string {
	// Find the widest key so descriptions line up inside the column.
	keyWidth := 0
	for _, e := range entries {
		if w := lipgloss.Width(e.keys); w > keyWidth {
			keyWidth = w
		}
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		pad := keyWidth - lipgloss.Width(e.keys)
		if pad < 0 {
			pad = 0
		}
		keys := style.StatusInfo.Render(e.keys)
		lines = append(lines, keys+strings.Repeat(" ", pad+2)+style.Muted.Render(e.desc))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderTypingIndicator() string {
	if !m.peerTyping || m.recipientMailbox == "" {
		return ""
	}
	return style.Italic.Render(fmt.Sprintf("%s is typing %s", m.recipientMailbox, m.typingSpinner.View()))
}

// renderToast produces the ephemeral-feedback line shown below the viewport.
// Empty string when no toast is active; callers treat that as a blank row.
func (m *Model) renderToast() string {
	if m.toast == nil {
		return ""
	}
	switch m.toast.level {
	case ToastWarn:
		return style.StatusWarn.Render(m.toast.text)
	case ToastBad:
		return style.StatusBad.Render(m.toast.text)
	default:
		return style.Muted.Render(m.toast.text)
	}
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
		return
	}
	m.contacts = append(m.contacts, contactItem{Mailbox: contact.AccountID, Fingerprint: contact.Fingerprint(), Verified: contact.Verified})
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

func verificationLabel(verified bool) string {
	if verified {
		return "verified"
	}
	return "unverified"
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

func (m *Model) handleInputActivity(previousValue, currentValue string) tea.Cmd {
	if previousValue == currentValue {
		return nil
	}
	if m.recipientMailbox == "" || m.authFailed || !m.connected {
		return nil
	}
	now := time.Now().UTC()
	if strings.TrimSpace(currentValue) == "" {
		if !m.localTypingSent || m.localTypingPeer != m.recipientMailbox {
			m.resetLocalTypingState()
			return nil
		}
		cmd := m.sendTypingCmd(m.recipientMailbox, typingStateIdle)
		m.resetLocalTypingState()
		return cmd
	}
	m.localTypingAt = now
	if m.localTypingSent && m.localTypingPeer == m.recipientMailbox {
		return nil
	}
	m.localTypingSent = true
	m.localTypingPeer = m.recipientMailbox
	return m.sendTypingCmd(m.recipientMailbox, typingStateActive)
}

func (m *Model) stopTypingCmd(recipient string) tea.Cmd {
	if recipient == "" || !m.localTypingSent || m.localTypingPeer != recipient {
		m.resetLocalTypingState()
		return nil
	}
	cmd := m.sendTypingCmd(recipient, typingStateIdle)
	m.resetLocalTypingState()
	return cmd
}

func (m *Model) resetLocalTypingState() {
	m.localTypingSent = false
	m.localTypingPeer = ""
	m.localTypingAt = time.Time{}
}

func (m *Model) clearPeerTyping() {
	m.peerTyping = false
	m.peerTypingExpiresAt = time.Time{}
	m.typingSpinner = spinner.New()
	m.typingSpinner.Spinner = spinner.Dot
	m.typingSpinner.Style = style.Muted
}

func defaultFilePickerDir() string {
	if dir, err := os.Getwd(); err == nil && dir != "" {
		return dir
	}
	if dir, err := os.UserHomeDir(); err == nil && dir != "" {
		return dir
	}
	return string(filepath.Separator)
}

func (m *Model) sendAttachment(path, attachmentType string) tea.Cmd {
	var (
		batch       *messaging.OutgoingBatch
		displayBody string
		err         error
	)
	switch attachmentType {
	case attachmentModePhoto:
		batch, displayBody, err = m.messaging.PreparePhotoOutgoing(m.recipientMailbox, path)
	case attachmentModeVoice:
		batch, displayBody, err = m.messaging.PrepareVoiceOutgoing(m.recipientMailbox, path)
	case attachmentModeFile:
		batch, displayBody, err = m.messaging.PrepareFileOutgoing(m.recipientMailbox, path)
	default:
		m.pushToast(fmt.Sprintf("unsupported attachment type %q", attachmentType), ToastBad)
		return nil
	}
	if err != nil {
		m.pushToast(err.Error(), ToastBad)
		return nil
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
	return m.sendCmd(m.recipientMailbox, displayBody, batch)
}

func (m *Model) updateFilePicker(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.closeFilePicker()
		return nil
	case tea.KeyBackspace:
		if err := m.goToParentDirectory(); err != nil {
			m.pushToast(fmt.Sprintf("file picker failed: %v", err), ToastBad)
		}
		return nil
	case tea.KeyUp:
		m.moveFilePickerSelection(-1)
		return nil
	case tea.KeyDown:
		m.moveFilePickerSelection(1)
		return nil
	case tea.KeyEnter:
		entry := m.selectedFilePickerEntry()
		if entry == nil {
			return nil
		}
		if entry.IsDir {
			if err := m.openFilePickerAt(entry.Path); err != nil {
				m.pushToast(fmt.Sprintf("open directory failed: %v", err), ToastBad)
			}
			return nil
		}
		m.closeFilePicker()
		return m.sendAttachment(entry.Path, attachmentModeFile)
	}
	return nil
}

func (m *Model) openFilePicker() error {
	return m.openFilePickerAt(m.filePickerDir)
}

func (m *Model) openFilePickerAt(dir string) error {
	entries, cleanedDir, err := readFilePickerEntries(dir)
	if err != nil {
		return err
	}
	m.filePickerOpen = true
	m.filePickerDir = cleanedDir
	m.filePickerEntries = entries
	m.filePickerSelected = 0
	m.input.Blur()
	return nil
}

func (m *Model) closeFilePicker() {
	m.filePickerOpen = false
	m.filePickerEntries = nil
	m.filePickerSelected = 0
	m.input.Focus()
}

func (m *Model) goToParentDirectory() error {
	parent := filepath.Dir(m.filePickerDir)
	if parent == m.filePickerDir {
		return nil
	}
	return m.openFilePickerAt(parent)
}

func (m *Model) moveFilePickerSelection(delta int) {
	if len(m.filePickerEntries) == 0 {
		return
	}
	m.filePickerSelected += delta
	if m.filePickerSelected < 0 {
		m.filePickerSelected = 0
	}
	if m.filePickerSelected >= len(m.filePickerEntries) {
		m.filePickerSelected = len(m.filePickerEntries) - 1
	}
}

func (m *Model) selectedFilePickerEntry() *filePickerEntry {
	if m.filePickerSelected < 0 || m.filePickerSelected >= len(m.filePickerEntries) {
		return nil
	}
	return &m.filePickerEntries[m.filePickerSelected]
}

func readFilePickerEntries(dir string) ([]filePickerEntry, string, error) {
	cleanedDir := filepath.Clean(dir)
	entries, err := os.ReadDir(cleanedDir)
	if err != nil {
		return nil, "", err
	}
	items := make([]filePickerEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		items = append(items, filePickerEntry{
			Name:  name,
			Path:  filepath.Join(cleanedDir, name),
			IsDir: entry.IsDir(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, cleanedDir, nil
}

func (m *Model) renderFilePicker(width int) string {
	title := style.Bold.Render("Attach File")
	dirLine := style.Muted.Render(m.filePickerDir)
	hint := style.Muted.Render("enter open/select  |  backspace up  |  esc cancel")
	lines := []string{title, dirLine, hint, ""}
	visibleEntries, hiddenAbove, hiddenBelow := m.filePickerVisibleEntries(max(1, m.height-12))
	if len(m.filePickerEntries) == 0 {
		lines = append(lines, style.Muted.Render("This directory is empty."))
	} else {
		if hiddenAbove {
			lines = append(lines, style.Muted.Render("..."))
		}
		for _, visible := range visibleEntries {
			idx := visible.index
			entry := visible.entry
			label := entry.Name
			if entry.IsDir {
				label += string(filepath.Separator)
			}
			rowStyle := lipgloss.NewStyle()
			if entry.IsDir {
				rowStyle = rowStyle.Inherit(style.StatusOk)
			}
			if idx == m.filePickerSelected {
				rowStyle = rowStyle.Inherit(style.Selected).Bold(true)
			}
			lines = append(lines, rowStyle.Render(label))
		}
		if hiddenBelow {
			lines = append(lines, style.Muted.Render("..."))
		}
	}
	modalWidth := min(max(48, width-6), width)
	modalHeight := max(8, m.height-4)
	modal := style.ModalBorder.Padding(1).Width(max(1, modalWidth-4)).Height(max(1, modalHeight-4)).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, max(1, m.height), lipgloss.Center, lipgloss.Center, modal)
}

type filePickerVisibleEntry struct {
	index int
	entry filePickerEntry
}

func (m *Model) filePickerVisibleEntries(maxEntries int) ([]filePickerVisibleEntry, bool, bool) {
	if len(m.filePickerEntries) == 0 {
		return nil, false, false
	}
	if maxEntries <= 0 || len(m.filePickerEntries) <= maxEntries {
		visible := make([]filePickerVisibleEntry, 0, len(m.filePickerEntries))
		for idx, entry := range m.filePickerEntries {
			visible = append(visible, filePickerVisibleEntry{index: idx, entry: entry})
		}
		return visible, false, false
	}
	start := m.filePickerSelected - (maxEntries / 2)
	if start < 0 {
		start = 0
	}
	end := start + maxEntries
	if end > len(m.filePickerEntries) {
		end = len(m.filePickerEntries)
		start = end - maxEntries
	}
	visible := make([]filePickerVisibleEntry, 0, end-start)
	for idx := start; idx < end; idx++ {
		visible = append(visible, filePickerVisibleEntry{index: idx, entry: m.filePickerEntries[idx]})
	}
	return visible, start > 0, end < len(m.filePickerEntries)
}

func attachmentLabel(attachmentType string) string {
	switch attachmentType {
	case attachmentModePhoto:
		return "photo"
	case attachmentModeVoice:
		return "voice note"
	case attachmentModeFile:
		return "file"
	default:
		return "attachment"
	}
}
