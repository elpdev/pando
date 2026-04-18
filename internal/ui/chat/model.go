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
	width            int
	height           int
}

type clientEventMsg transport.Event
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
		},
		connecting: true,
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
			if body == "" || m.disconnected {
				return m, nil
			}
			envelope, err := m.messaging.EncryptOutgoing(m.recipientMailbox, body)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.messages = append(m.messages, fmt.Sprintf("you -> %s: %s", m.recipientMailbox, body))
			m.input.SetValue("")
			m.syncViewport()
			return m, m.sendCmd(body, envelope)
		}
	case clientEventMsg:
		event := transport.Event(msg)
		if event.Err != nil {
			m.status = fmt.Sprintf("disconnected: %v", event.Err)
			m.disconnected = true
			m.connected = false
			return m, nil
		}
		if event.Message != nil {
			m.handleProtocolMessage(*event.Message)
		}
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
	return m.status
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
			m.status = fmt.Sprintf("connected to relay, subscribed as %s", m.mailbox)
		}
	case protocol.MessageTypeIncoming:
		if msg.Incoming != nil {
			body, err := m.messaging.DecryptIncoming(*msg.Incoming)
			if err != nil {
				m.status = fmt.Sprintf("decrypt failed: %v", err)
				return
			}
			if err := m.messaging.SaveReceived(msg.Incoming.SenderMailbox, body, msg.Incoming.Timestamp); err != nil {
				m.status = fmt.Sprintf("save history failed: %v", err)
			}
			ts := msg.Incoming.Timestamp.Format(time.Kitchen)
			m.messages = append(m.messages, fmt.Sprintf("[%s] %s -> %s: %s", ts, msg.Incoming.SenderMailbox, msg.Incoming.RecipientMailbox, body))
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
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.client.Connect(ctx); err != nil {
			return clientEventMsg(transport.Event{Err: err})
		}
		return nil
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

func (m *Model) sendCmd(body string, envelope protocol.Envelope) tea.Cmd {
	return func() tea.Msg {
		return sendResultMsg{body: body, err: m.client.Send(envelope)}
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
