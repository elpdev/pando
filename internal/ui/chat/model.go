package chat

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/transport"
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
	peerFingerprint     string
	peerVerified        bool
	peerTyping          bool
	peerTypingExpiresAt time.Time
	typingFrame         int
	localTypingSent     bool
	localTypingPeer     string
	localTypingAt       time.Time
	width               int
	height              int
	sidebarWidth        int
}

type clientEventMsg transport.Event
type reconnectResultMsg struct{ err error }
type typingTickMsg time.Time
type typingSendResultMsg struct{ err error }
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
)

func New(deps Deps) *Model {
	input := textinput.New()
	input.Focus()
	input.CharLimit = 4096
	input.Prompt = "> "

	vp := viewport.New(0, 0)
	vp.SetContent("")

	m := &Model{
		client:           deps.Client,
		messaging:        deps.Messaging,
		mailbox:          deps.Mailbox,
		recipientMailbox: deps.RecipientMailbox,
		relayURL:         deps.RelayURL,
		input:            input,
		viewport:         vp,
		status:           fmt.Sprintf("connecting to %s as %s", deps.RelayURL, deps.Mailbox),
		connecting:       true,
		selectedIndex:    -1,
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
		switch msg.Type {
		case tea.KeyUp:
			m.moveSelection(-1)
			return m, nil
		case tea.KeyDown:
			m.moveSelection(1)
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
				m.status = "cannot send: relay auth failed; restart with --relay-token"
				return m, nil
			}
			if m.recipientMailbox == "" {
				m.status = "select a contact from the sidebar first"
				return m, nil
			}
			if !m.connected {
				m.status = "relay is not connected; waiting to reconnect"
				return m, nil
			}
			if strings.HasPrefix(body, "/send-photo") {
				path := parseAttachmentPath(strings.TrimSpace(strings.TrimPrefix(body, "/send-photo")))
				if path == "" {
					m.status = "usage: /send-photo <path>"
					return m, nil
				}
				batch, displayBody, err := m.messaging.PreparePhotoOutgoing(m.recipientMailbox, path)
				if err != nil {
					m.status = err.Error()
					return m, nil
				}
				m.messages = append(m.messages, fmt.Sprintf("you -> %s: %s", m.recipientMailbox, displayBody))
				m.input.SetValue("")
				m.resetLocalTypingState()
				m.syncViewport()
				return m, m.sendCmd(m.recipientMailbox, displayBody, batch)
			}
			if strings.HasPrefix(body, "/send-voice") {
				path := parseAttachmentPath(strings.TrimSpace(strings.TrimPrefix(body, "/send-voice")))
				if path == "" {
					m.status = "usage: /send-voice <path>"
					return m, nil
				}
				batch, displayBody, err := m.messaging.PrepareVoiceOutgoing(m.recipientMailbox, path)
				if err != nil {
					m.status = err.Error()
					return m, nil
				}
				m.messages = append(m.messages, fmt.Sprintf("you -> %s: %s", m.recipientMailbox, displayBody))
				m.input.SetValue("")
				m.resetLocalTypingState()
				m.syncViewport()
				return m, m.sendCmd(m.recipientMailbox, displayBody, batch)
			}
			batch, err := m.messaging.EncryptOutgoing(m.recipientMailbox, body)
			if err != nil {
				m.status = err.Error()
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
			if transport.IsUnauthorized(event.Err) {
				m.handleAuthFailure(event.Err)
				return m, nil
			}
			m.status = fmt.Sprintf("disconnected: %v", event.Err)
			m.disconnected = true
			m.connected = false
			m.resetLocalTypingState()
			return m, m.reconnectCmd()
		}
		if event.Message != nil {
			m.handleProtocolMessage(*event.Message)
		}
		return m, m.waitForEvent()
	case reconnectResultMsg:
		if msg.err != nil {
			if transport.IsUnauthorized(msg.err) {
				m.handleAuthFailure(msg.err)
				return m, nil
			}
			m.disconnected = true
			m.connected = false
			m.resetLocalTypingState()
			return m, m.reconnectCmd()
		}
		m.status = fmt.Sprintf("reconnected to %s, waiting for subscribe ack", m.relayURL)
		return m, m.waitForEvent()
	case typingTickMsg:
		now := time.Time(msg)
		if m.peerTyping && !m.peerTypingExpiresAt.IsZero() && !now.Before(m.peerTypingExpiresAt) {
			m.clearPeerTyping()
		}
		if m.peerTyping {
			m.typingFrame = (m.typingFrame + 1) % 3
		}
		var cmd tea.Cmd
		if m.localTypingSent && !m.localTypingAt.IsZero() && now.Sub(m.localTypingAt) >= typingIdleTimeout {
			cmd = m.sendTypingCmd(m.localTypingPeer, typingStateIdle)
			m.resetLocalTypingState()
		}
		return m, tea.Batch(m.typingTickCmd(), cmd)
	case sendResultMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("send failed: %v", msg.err)
			return m, nil
		}
		if err := m.messaging.SaveSent(msg.recipient, msg.messageID, msg.body); err != nil {
			m.status = fmt.Sprintf("save history failed: %v", err)
			return m, nil
		}
		if msg.recipient == m.recipientMailbox {
			m.loadHistory()
			m.syncViewport()
		}
		return m, nil
	case typingSendResultMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("typing indicator failed: %v", msg.err)
		}
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
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m *Model) Status() string {
	if m.recipientMailbox == "" {
		return m.status + " | no active chat"
	}
	return fmt.Sprintf("%s | peer=%s %s", m.status, verificationLabel(m.peerVerified), m.peerFingerprint)
}

func (m *Model) Mailbox() string {
	return m.mailbox
}

func (m *Model) RecipientMailbox() string {
	return m.recipientMailbox
}

func (m *Model) Close() error {
	return m.client.Close()
}

func (m *Model) handleProtocolMessage(msg protocol.Message) {
	switch msg.Type {
	case protocol.MessageTypeAck:
		if m.connecting {
			m.connecting = false
			m.connected = true
			m.authFailed = false
			m.disconnected = false
			m.reconnectAttempt = 0
			m.syncInputPlaceholder()
			m.status = fmt.Sprintf("connected to relay, subscribed as %s", m.mailbox)
		}
	case protocol.MessageTypeIncoming:
		if msg.Incoming == nil {
			return
		}
		result, err := m.messaging.HandleIncoming(*msg.Incoming)
		if err != nil {
			m.status = fmt.Sprintf("incoming message failed: %v", err)
			return
		}
		if result == nil || result.Duplicate {
			return
		}
		if len(result.AckEnvelopes) != 0 {
			for _, envelope := range result.AckEnvelopes {
				if err := m.client.Send(envelope); err != nil {
					m.status = fmt.Sprintf("delivery ack failed: %v", err)
					break
				}
			}
		}
		if result.ContactUpdated != nil {
			m.upsertContact(result.ContactUpdated)
			if result.ContactUpdated.AccountID == m.recipientMailbox {
				m.syncRecipientDetails()
				m.status = fmt.Sprintf("updated device bundle for %s", result.ContactUpdated.AccountID)
			}
			return
		}
		if result.Control {
			if result.TypingState != "" {
				if result.PeerAccountID == m.recipientMailbox {
					if result.TypingState == typingStateActive {
						m.peerTyping = true
						m.peerTypingExpiresAt = result.TypingExpiresAt
						m.typingFrame = 0
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
				m.status = fmt.Sprintf("delivery acknowledged for %s", result.MessageID)
			}
			return
		}
		if err := m.messaging.SaveReceived(result.PeerAccountID, result.Body, msg.Incoming.Timestamp); err != nil {
			m.status = fmt.Sprintf("save history failed: %v", err)
			return
		}
		if result.PeerAccountID == m.recipientMailbox {
			m.clearPeerTyping()
			ts := msg.Incoming.Timestamp.Format(time.Kitchen)
			m.messages = append(m.messages, fmt.Sprintf("[%s] %s -> %s: %s", ts, msg.Incoming.SenderMailbox, msg.Incoming.RecipientMailbox, result.Body))
			m.syncViewport()
			m.status = fmt.Sprintf("message received from %s", result.PeerAccountID)
			return
		}
		m.status = fmt.Sprintf("new message from %s", result.PeerAccountID)
	case protocol.MessageTypeError:
		if msg.Error != nil {
			m.status = fmt.Sprintf("relay error: %s", msg.Error.Message)
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
			return clientEventMsg(transport.Event{Err: err})
		}
		return nil
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
	m.status = fmt.Sprintf("reconnecting to relay in %s", delay)
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
		m.status = fmt.Sprintf("load contacts failed: %v", err)
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
	m.status = fmt.Sprintf("opened chat with %s", m.recipientMailbox)
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
		m.status = fmt.Sprintf("load history failed: %v", err)
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
	border := lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderRight(true).BorderLeft(false).BorderTop(false).BorderBottom(false).BorderForeground(lipgloss.Color("238"))
	title := lipgloss.NewStyle().Bold(true).Render("Contacts")
	lines := []string{title, lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("up/down to browse, enter to open")}
	if len(m.contacts) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("No contacts yet."))
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Import one with pandoctl add-contact."))
		return border.Width(m.sidebarWidth).Height(max(1, m.height)).Render(strings.Join(lines, "\n"))
	}
	for idx, contact := range m.contacts {
		mailboxStyle := lipgloss.NewStyle()
		if idx == m.selectedIndex {
			mailboxStyle = mailboxStyle.Background(lipgloss.Color("238"))
		}
		if contact.Mailbox == m.recipientMailbox {
			mailboxStyle = mailboxStyle.Bold(true).Foreground(lipgloss.Color("86"))
		}
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		statusText := "unverified"
		if contact.Verified {
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
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
			lipgloss.NewStyle().Bold(true).Render("No chat selected"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Pick a contact from the sidebar to load the conversation."),
			lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Verified contacts are labeled directly in the roster."),
			"",
			m.input.View(),
		}
		return lipgloss.NewStyle().Width(width).Render(strings.Join(empty, "\n"))
	}
	header := []string{
		lipgloss.NewStyle().Bold(true).Render(m.recipientMailbox),
		lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(fmt.Sprintf("fingerprint %s  %s", m.peerFingerprint, verificationLabel(m.peerVerified))),
		m.viewport.View(),
		m.renderTypingIndicator(),
		m.input.View(),
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(header, "\n"))
}

func (m *Model) renderTypingIndicator() string {
	if !m.peerTyping || m.recipientMailbox == "" {
		return ""
	}
	dots := strings.Repeat(".", m.typingFrame+1)
	return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true).Render(fmt.Sprintf("%s is typing%s", m.recipientMailbox, dots))
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
	m.typingFrame = 0
}
