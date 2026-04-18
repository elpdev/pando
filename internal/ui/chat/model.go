package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/chatui/internal/messaging"
	"github.com/elpdev/chatui/internal/protocol"
	"github.com/elpdev/chatui/internal/transport"
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
	reconnectAttempt int
	peerFingerprint  string
	peerVerified     bool
	width            int
	height           int
}

type clientEventMsg transport.Event
type reconnectResultMsg struct{ err error }
type sendResultMsg struct {
	body string
	err  error
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
		messages: []string{
			fmt.Sprintf("Encrypted chat ready. Target mailbox: %s", deps.RecipientMailbox),
			fmt.Sprintf("Your fingerprint: %s", deps.Messaging.Identity().Fingerprint()),
			fmt.Sprintf("Peer fingerprint: %s (%s)", peerFingerprint, verificationLabel(peerVerified)),
		},
		connecting:      true,
		peerFingerprint: peerFingerprint,
		peerVerified:    peerVerified,
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
			if !m.connected {
				m.status = "relay is not connected; waiting to reconnect"
				return m, nil
			}
			envelopes, err := m.messaging.EncryptOutgoing(m.recipientMailbox, body)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.messages = append(m.messages, fmt.Sprintf("you -> %s: %s", m.recipientMailbox, body))
			m.input.SetValue("")
			m.syncViewport()
			return m, m.sendCmd(body, envelopes)
		}
	case clientEventMsg:
		event := transport.Event(msg)
		if event.Err != nil {
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
		if err := m.messaging.SaveSent(m.recipientMailbox, msg.body); err != nil {
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
			m.disconnected = false
			m.reconnectAttempt = 0
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
			if result.ContactUpdated != nil && result.ContactUpdated.AccountID == m.recipientMailbox {
				m.peerFingerprint = result.ContactUpdated.Fingerprint()
				m.peerVerified = result.ContactUpdated.Verified
				m.status = fmt.Sprintf("updated device bundle for %s", result.ContactUpdated.AccountID)
				return
			}
			if result.Control {
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

func (m *Model) sendCmd(body string, envelopes []protocol.Envelope) tea.Cmd {
	return func() tea.Msg {
		for _, envelope := range envelopes {
			if err := m.client.Send(envelope); err != nil {
				return sendResultMsg{body: body, err: err}
			}
		}
		return sendResultMsg{body: body}
	}
}

func (m *Model) loadHistory() {
	records, err := m.messaging.History(m.recipientMailbox)
	if err != nil {
		m.status = fmt.Sprintf("load history failed: %v", err)
		return
	}
	for _, record := range records {
		ts := record.Timestamp.Format(time.Kitchen)
		if record.Direction == "outbound" {
			m.messages = append(m.messages, fmt.Sprintf("[%s] you -> %s: %s", ts, m.recipientMailbox, record.Body))
			continue
		}
		m.messages = append(m.messages, fmt.Sprintf("[%s] %s -> %s: %s", ts, record.PeerMailbox, m.mailbox, record.Body))
	}
}

func verificationLabel(verified bool) string {
	if verified {
		return "verified"
	}
	return "unverified"
}
