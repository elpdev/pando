package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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

type Model struct {
	client           transport.Client
	messaging        *messaging.Service
	mailbox          string
	recipientMailbox string
	relayURL         string
	input            textinput.Model
	viewport         viewport.Model
	messages         []string
	status           string
	connecting       bool
	disconnected     bool
	connected        bool
	authFailed       bool
	reconnectAttempt int
	peerFingerprint  string
	peerVerified     bool
	width            int
	height           int
}

type clientEventMsg transport.Event
type reconnectResultMsg struct{ err error }
type sendResultMsg struct {
	messageID string
	body      string
	err       error
}

func New(deps Deps) *Model {
	input := textinput.New()
	input.Placeholder = "Type a message"
	input.Focus()
	input.CharLimit = 4096
	input.Prompt = "> "

	vp := viewport.New(0, 0)
	vp.SetContent("")

	peerFingerprint := "unknown"
	peerVerified := false
	if deps.Messaging != nil {
		if contact, err := deps.Messaging.Contact(deps.RecipientMailbox); err == nil {
			peerFingerprint = contact.Fingerprint()
			peerVerified = contact.Verified
		}
	}

	return &Model{
		client:           deps.Client,
		messaging:        deps.Messaging,
		mailbox:          deps.Mailbox,
		recipientMailbox: deps.RecipientMailbox,
		relayURL:         deps.RelayURL,
		input:            input,
		viewport:         vp,
		status:           fmt.Sprintf("connecting to %s as %s", deps.RelayURL, deps.Mailbox),
		messages:         initialMessages(deps.RecipientMailbox, deps.Messaging.Identity().Fingerprint(), peerFingerprint, peerVerified),
		connecting:       true,
		peerFingerprint:  peerFingerprint,
		peerVerified:     peerVerified,
	}
}

func (m *Model) Init() tea.Cmd {
	m.loadHistory()
	return tea.Batch(m.connectCmd(), m.waitForEvent())
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = max(1, width)
	m.viewport.Height = max(1, height)
	m.syncViewport()
}

func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			body := strings.TrimSpace(m.input.Value())
			if body == "" {
				return m, nil
			}
			if m.authFailed {
				m.status = "cannot send: relay auth failed; restart with --relay-token"
				return m, nil
			}
			if !m.connected {
				m.status = "relay is not connected; waiting to reconnect"
				return m, nil
			}
			if strings.HasPrefix(body, "/send-photo") {
				path := strings.TrimSpace(strings.TrimPrefix(body, "/send-photo"))
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
				m.syncViewport()
				return m, m.sendCmd(displayBody, batch)
			}
			batch, err := m.messaging.EncryptOutgoing(m.recipientMailbox, body)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.messages = append(m.messages, fmt.Sprintf("you -> %s: %s", m.recipientMailbox, body))
			m.input.SetValue("")
			m.syncViewport()
			return m, m.sendCmd(body, batch)
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
			return m, m.reconnectCmd()
		}
		m.status = fmt.Sprintf("reconnected to %s, waiting for subscribe ack", m.relayURL)
		return m, m.waitForEvent()
	case sendResultMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("send failed: %v", msg.err)
			return m, nil
		}
		if err := m.messaging.SaveSent(m.recipientMailbox, msg.messageID, msg.body); err != nil {
			m.status = fmt.Sprintf("save history failed: %v", err)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	return strings.Join([]string{m.viewport.View(), m.input.View()}, "\n")
}

func (m *Model) Status() string {
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
			m.input.Placeholder = "Type a message"
			m.status = fmt.Sprintf("connected to relay, subscribed as %s", m.mailbox)
		}
	case protocol.MessageTypeIncoming:
		if msg.Incoming != nil {
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
			if result.ContactUpdated != nil && result.ContactUpdated.AccountID == m.recipientMailbox {
				m.peerFingerprint = result.ContactUpdated.Fingerprint()
				m.peerVerified = result.ContactUpdated.Verified
				m.status = fmt.Sprintf("updated device bundle for %s", result.ContactUpdated.AccountID)
				return
			}
			if result.Control {
				if result.MessageID != "" {
					m.loadHistory()
					m.syncViewport()
					m.status = fmt.Sprintf("delivery acknowledged for %s", result.MessageID)
				}
				return
			}
			if err := m.messaging.SaveReceived(result.PeerAccountID, result.Body, msg.Incoming.Timestamp); err != nil {
				m.status = fmt.Sprintf("save history failed: %v", err)
			}
			ts := msg.Incoming.Timestamp.Format(time.Kitchen)
			m.messages = append(m.messages, fmt.Sprintf("[%s] %s -> %s: %s", ts, msg.Incoming.SenderMailbox, msg.Incoming.RecipientMailbox, result.Body))
			m.syncViewport()
		}
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

func (m *Model) sendCmd(body string, batch *messaging.OutgoingBatch) tea.Cmd {
	return func() tea.Msg {
		if batch == nil {
			return sendResultMsg{body: body}
		}
		for _, envelope := range batch.Envelopes {
			if err := m.client.Send(envelope); err != nil {
				return sendResultMsg{messageID: batch.MessageID, body: body, err: err}
			}
		}
		return sendResultMsg{messageID: batch.MessageID, body: body}
	}
}

func (m *Model) loadHistory() {
	m.messages = initialMessages(m.recipientMailbox, m.messaging.Identity().Fingerprint(), m.peerFingerprint, m.peerVerified)
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
}

func initialMessages(recipientMailbox, ownFingerprint, peerFingerprint string, peerVerified bool) []string {
	return []string{
		fmt.Sprintf("Encrypted chat ready. Target mailbox: %s", recipientMailbox),
		fmt.Sprintf("Your fingerprint: %s", ownFingerprint),
		fmt.Sprintf("Peer fingerprint: %s (%s)", peerFingerprint, verificationLabel(peerVerified)),
	}
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
	m.input.Placeholder = "Relay auth failed. Restart with --relay-token"
}
