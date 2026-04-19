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
	toast               *toastState
	width               int
	height              int
	sidebarWidth        int
}

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
		if m.filePickerOpen {
			return m, m.updateFilePicker(msg)
		}
		if m.addContactOpen {
			return m.handleAddContactKey(msg)
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
			m.messages = append(m.messages, fmt.Sprintf("you -> %s: %s", m.recipientMailbox, body))
			m.input.SetValue("")
			m.resetLocalTypingState()
			m.syncViewport()
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
			m.pushToast(fmt.Sprintf("send failed: %v", msg.err), ToastBad)
			return m, nil
		}
		if err := m.messaging.SaveSent(msg.recipient, msg.messageID, msg.body); err != nil {
			m.pushToast(fmt.Sprintf("save history failed: %v", err), ToastBad)
			return m, nil
		}
		if msg.recipient == m.recipientMailbox {
			m.loadHistory()
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
	left := m.renderSidebar()
	right := m.renderConversation()
	view := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	if !m.addContactOpen {
		return view
	}
	return m.renderAddContactModal(view)
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
					m.loadHistory()
					m.syncViewport()
				}
				m.pushToast(fmt.Sprintf("delivery acknowledged for %s", result.MessageID), ToastInfo)
			}
			return
		}
		if err := m.messaging.SaveReceived(result.PeerAccountID, result.Body, msg.Incoming.Timestamp); err != nil {
			m.pushToast(fmt.Sprintf("save history failed: %v", err), ToastBad)
			return
		}
		if result.PeerAccountID == m.recipientMailbox {
			m.clearPeerTyping()
			ts := msg.Incoming.Timestamp.Format(time.Kitchen)
			m.messages = append(m.messages, fmt.Sprintf("[%s] %s -> %s: %s", ts, msg.Incoming.SenderMailbox, msg.Incoming.RecipientMailbox, result.Body))
			m.syncViewport()
			return
		}
		m.pushToast(fmt.Sprintf("new message from %s", result.PeerAccountID), ToastInfo)
	case protocol.MessageTypeError:
		if msg.Error != nil {
			m.pushToast(fmt.Sprintf("relay error: %s", msg.Error.Message), ToastBad)
		}
	}
}

func (m *Model) syncViewport() {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		return
	}
	m.viewport.SetContent(strings.Join(m.messages, "\n"))
	m.viewport.GotoBottom()
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
	m.syncRecipientDetails()
	m.clearPeerTyping()
	m.loadHistory()
	m.syncViewport()
	m.syncInputPlaceholder()
	return true
}

func (m *Model) loadHistory() {
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
		ts := record.Timestamp.Format(time.Kitchen)
		if record.Direction == "outbound" {
			status := "pending"
			if record.Delivered {
				status = "delivered"
			}
			m.messages = append(m.messages, fmt.Sprintf("[%s] you -> %s [%s]: %s", ts, m.recipientMailbox, status, record.Body))
			continue
		}
		m.messages = append(m.messages, fmt.Sprintf("[%s] %s -> %s: %s", ts, record.PeerMailbox, m.mailbox, record.Body))
	}
	if len(m.messages) == 0 {
		m.viewport.SetContent("No messages yet.")
		return
	}
	m.syncViewport()
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
	m.viewport.Height = max(1, m.height-4)
}

func (m *Model) renderSidebar() string {
	border := style.SidebarBorder
	title := style.Bold.Render("Contacts")
	shortcut := "up/down browse  enter open  ctrl+n add"
	if m.addContactOpen {
		shortcut = "add contact open  ctrl+s import  esc cancel"
	}
	lines := []string{title, style.Muted.Render(shortcut)}
	if len(m.contacts) == 0 {
		lines = append(lines, style.Muted.Render("No contacts yet."))
		lines = append(lines, style.Muted.Render("Press ctrl+n to add one here."))
		return border.Width(m.sidebarWidth).Height(max(1, m.height)).Render(strings.Join(lines, "\n"))
	}
	for idx, contact := range m.contacts {
		mailboxStyle := lipgloss.NewStyle()
		if idx == m.selectedIndex {
			mailboxStyle = style.Selected
		}
		if contact.Mailbox == m.recipientMailbox {
			mailboxStyle = style.PeerAccentStyle(contact.Fingerprint).Bold(true)
		}
		statusStyle := style.UnverifiedWarn
		statusText := "unverified"
		if contact.Verified {
			statusStyle = style.VerifiedOk
			statusText = "verified"
		}
		line := fmt.Sprintf("%s  %s", mailboxStyle.Render(contact.Mailbox), statusStyle.Render(statusText))
		lines = append(lines, line)
	}
	return border.Width(m.sidebarWidth).Height(max(1, m.height)).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderConversation() string {
	width := max(1, m.width-m.sidebarWidth-1)
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
		style.Muted.Render("ctrl+o attach file  |  /send-photo <path>  |  /send-voice <path>"),
		m.viewport.View(),
		m.renderToast(),
		m.renderTypingIndicator(),
		m.input.View(),
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(header, "\n"))
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
	m.messages = append(m.messages, fmt.Sprintf("you -> %s: %s", m.recipientMailbox, displayBody))
	m.input.SetValue("")
	m.resetLocalTypingState()
	m.syncViewport()
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
	m.messages = append(m.messages, fmt.Sprintf("you -> %s: %s", m.recipientMailbox, displayBody))
	m.input.SetValue("")
	m.resetLocalTypingState()
	m.syncViewport()
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
