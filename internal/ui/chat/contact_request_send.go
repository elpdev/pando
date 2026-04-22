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
	"github.com/elpdev/pando/internal/ui/style"
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
	inputs  []textinput.Model
	focused int
	busy    bool
	error   string
}

type contactRequestSendResultMsg struct {
	request *store.ContactRequest
	err     error
}

func newContactRequestSendModal(deps contactRequestSendDeps) contactRequestSendModal {
	mailbox := textinput.New()
	mailbox.Placeholder = "mailbox"
	note := textinput.New()
	note.Placeholder = "optional note"
	note.CharLimit = 280
	return contactRequestSendModal{deps: deps, inputs: []textinput.Model{mailbox, note}}
}

func (m *contactRequestSendModal) Open(_ viewOpenCtx) tea.Cmd {
	if !m.deps.relayConfigured() {
		return completePaletteCmd("no relay configured", ToastBad)
	}
	*m = newContactRequestSendModal(m.deps)
	return m.inputs[0].Focus()
}

func (m *contactRequestSendModal) Close() {
	*m = newContactRequestSendModal(m.deps)
}

func (m *contactRequestSendModal) Update(msg tea.Msg) (bool, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if keyMsg.Type == tea.KeyEsc {
		return false, nil
	}
	if m.busy {
		return true, nil
	}
	switch keyMsg.Type {
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

func (m *contactRequestSendModal) Body(width, _ int) string {
	bodyWidth := max(1, width)
	lines := []string{
		style.PaletteMeta.Width(bodyWidth).Render("Send an outgoing contact request to a discoverable mailbox on the active relay."),
		renderContactRequestSendInput(bodyWidth, "Mailbox", m.inputs[0], m.focused == 0),
		renderContactRequestSendInput(bodyWidth, "Note", m.inputs[1], m.focused == 1),
	}
	if m.busy {
		lines = append(lines, style.PaletteMeta.Width(bodyWidth).Render("sending request..."))
	}
	if m.error != "" {
		lines = append(lines, style.StatusBad.Width(bodyWidth).Render(m.error))
	}
	return strings.Join(lines, "\n\n")
}

func (m *contactRequestSendModal) Subtitle() string {
	return "Create a pending introduction request without importing the contact yet."
}

func (m *contactRequestSendModal) Footer() string {
	if m.busy {
		return "sending..."
	}
	return "tab move · enter send · esc cancel"
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
	m.commandPalette.Close()
	if m.ui.focus == focusChat {
		m.input.Focus()
	}
	m.pushToast(fmt.Sprintf("sent contact request to %s", msg.request.AccountID), ToastInfo)
	return m, nil
}

func publishRelayEnvelopes(ctx context.Context, relayURL, relayToken string, envelopes []protocol.Envelope) error {
	return relayapi.PublishEnvelopes(ctx, relayURL, relayToken, envelopes)
}

func renderContactRequestSendInput(width int, label string, input textinput.Model, focused bool) string {
	input.Width = max(1, width-2)
	heading := style.Muted.Render(label)
	if focused {
		heading = style.Bright.Render(label)
	}
	return heading + "\n" + style.PaletteInput.Width(width).Padding(0, 1).Render(input.View())
}
