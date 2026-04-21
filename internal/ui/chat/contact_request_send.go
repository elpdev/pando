package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
)

type contactRequestSendDeps struct {
	messaging         *messaging.Service
	ensureRelayClient func() (RelayClient, error)
	relayConfigured   func() bool
	relayURL          func() string
	relayToken        func() string
	publishEnvelopes  func(context.Context, string, string, []protocol.Envelope) error
}

type contactRequestSendModal struct {
	deps    contactRequestSendDeps
	open    bool
	inputs  []textinput.Model
	focused int
	busy    bool
	error   string
}

type contactRequestSendResultMsg struct {
	request *store.ContactRequest
	err     error
}

type contactRequestSendClosedMsg struct{}

func newContactRequestSendModal(deps contactRequestSendDeps) contactRequestSendModal {
	mailbox := textinput.New()
	mailbox.Placeholder = "mailbox"
	note := textinput.New()
	note.Placeholder = "optional note"
	note.CharLimit = 280
	return contactRequestSendModal{deps: deps, inputs: []textinput.Model{mailbox, note}}
}

func (m *contactRequestSendModal) Open() tea.Cmd {
	*m = newContactRequestSendModal(m.deps)
	m.open = true
	return m.inputs[0].Focus()
}

func (m *contactRequestSendModal) Close() {
	deps := m.deps
	*m = newContactRequestSendModal(deps)
}

func (m *contactRequestSendModal) Update(msg tea.Msg) (bool, tea.Cmd) {
	if !m.open {
		return false, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if m.busy {
		if keyMsg.Type == tea.KeyEsc {
			return true, nil
		}
		return true, nil
	}
	switch keyMsg.Type {
	case tea.KeyEsc:
		return true, func() tea.Msg { return contactRequestSendClosedMsg{} }
	case tea.KeyTab, tea.KeyShiftTab, tea.KeyUp, tea.KeyDown:
		return true, m.moveFocus(keyMsg)
	case tea.KeyEnter:
		mailbox := strings.TrimSpace(m.inputs[0].Value())
		if mailbox == "" {
			m.error = "mailbox is required"
			return true, nil
		}
		if !m.deps.relayConfigured() {
			m.error = "no relay configured"
			return true, nil
		}
		m.busy = true
		m.error = ""
		note := strings.TrimSpace(m.inputs[1].Value())
		return true, contactRequestSendCmd(m.deps, mailbox, note)
	}
	var cmd tea.Cmd
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	m.error = ""
	return true, cmd
}

func (m *contactRequestSendModal) moveFocus(msg tea.KeyMsg) tea.Cmd {
	m.inputs[m.focused].Blur()
	if msg.Type == tea.KeyShiftTab || msg.Type == tea.KeyUp {
		m.focused = (m.focused + len(m.inputs) - 1) % len(m.inputs)
	} else {
		m.focused = (m.focused + 1) % len(m.inputs)
	}
	return m.inputs[m.focused].Focus()
}

func contactRequestSendCmd(deps contactRequestSendDeps, mailbox, note string) tea.Cmd {
	return func() tea.Msg {
		client, err := deps.ensureRelayClient()
		if err != nil {
			return contactRequestSendResultMsg{err: err}
		}
		entry, err := client.LookupDirectoryEntry(mailbox)
		if err != nil {
			return contactRequestSendResultMsg{err: err}
		}
		if !entry.Entry.Discoverable {
			return contactRequestSendResultMsg{err: fmt.Errorf("contact %s is not discoverable", mailbox)}
		}
		envelopes, request, err := deps.messaging.ContactRequestEnvelopes(entry, note)
		if err != nil {
			return contactRequestSendResultMsg{err: err}
		}
		if err := deps.publishEnvelopes(context.Background(), deps.relayURL(), deps.relayToken(), envelopes); err != nil {
			return contactRequestSendResultMsg{err: err}
		}
		if err := deps.messaging.SaveContactRequest(request); err != nil {
			return contactRequestSendResultMsg{err: err}
		}
		return contactRequestSendResultMsg{request: request}
	}
}

func (m *Model) openContactRequestSendModal() tea.Cmd {
	if !m.relayConfigured() {
		m.pushToast("no relay configured", ToastBad)
		return nil
	}
	m.input.Blur()
	return m.contactRequestSend.Open()
}

func (m *Model) closeContactRequestSendModal(keepStatus bool) {
	m.contactRequestSend.Close()
	if !keepStatus {
		m.pushToast("send contact request cancelled", ToastInfo)
	}
	if m.ui.focus == focusChat {
		m.input.Focus()
	}
}

func (m *Model) handleContactRequestSendResult(msg contactRequestSendResultMsg) (*Model, tea.Cmd) {
	m.contactRequestSend.busy = false
	if msg.err != nil {
		m.contactRequestSend.error = msg.err.Error()
		return m, nil
	}
	if msg.request == nil {
		return m, nil
	}
	m.upsertContactRequest(*msg.request)
	m.closeContactRequestSendModal(true)
	m.pushToast(fmt.Sprintf("sent contact request to %s", msg.request.AccountID), ToastInfo)
	return m, nil
}

func (m *Model) handleContactRequestSendClosedMsg(_ contactRequestSendClosedMsg) (*Model, tea.Cmd) {
	m.closeContactRequestSendModal(false)
	return m, nil
}

func publishRelayEnvelopes(ctx context.Context, relayURL, relayToken string, envelopes []protocol.Envelope) error {
	return relayapi.PublishEnvelopes(ctx, relayURL, relayToken, envelopes)
}
