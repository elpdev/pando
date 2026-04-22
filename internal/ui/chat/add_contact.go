package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/style"
)

type addContactMode int

const (
	addContactModeChooser addContactMode = iota
	addContactModePaste
	addContactModeLookup
	addContactModeInviteChoice
	addContactModeInviteStart
	addContactModeInviteAccept
)

const inviteExchangeTimeout = 2 * time.Minute

type addContactCompletedMsg struct {
	contact   *identity.Contact
	toastText string
}

type addContactImportResultMsg struct {
	contact *identity.Contact
	err     error
}

type addContactLookupResultMsg struct {
	contact *identity.Contact
	err     error
}

type addContactInviteExchangeResultMsg struct {
	contact   *identity.Contact
	err       error
	cancelled bool
}

type addContactInviteStartedMsg struct {
	code string
	err  error
}

type addContactDeps struct {
	messaging         *messaging.Service
	ensureRelayClient func() (RelayClient, error)
	relayConfigured   func() bool
}

type addContactModal struct {
	deps        addContactDeps
	mode        addContactMode
	selected    int
	value       string
	code        string
	error       string
	busy        bool
	preview     *identity.Contact
	cancel      context.CancelFunc
	lookupInput textinput.Model
	inviteInput textinput.Model
}

func newAddContactModal(deps addContactDeps) addContactModal {
	modal := addContactModal{deps: deps}
	modal.lookupInput = newAddContactInput("mailbox")
	modal.inviteInput = newAddContactInput("invite code")
	modal.mode = addContactModeChooser
	return modal
}

func newAddContactInput(placeholder string) textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.CharLimit = addContactLimit
	return input
}

func (m *addContactModal) Open(viewOpenCtx) tea.Cmd {
	deps := m.deps
	*m = newAddContactModal(deps)
	return nil
}

func (m *addContactModal) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	deps := m.deps
	*m = newAddContactModal(deps)
}

func (m *addContactModal) Update(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Esc at the top-level chooser belongs to the palette (exits the
		// view). All other Esc cases are handled by the mode-specific key
		// handlers below so they can back out one mode at a time.
		if msg.Type == tea.KeyEsc && m.mode == addContactModeChooser && !m.busy {
			return false, nil
		}
		return true, m.updateKey(msg)
	}
	return m.updateActiveInput(msg)
}

func (m *addContactModal) updateActiveInput(msg tea.Msg) (bool, tea.Cmd) {
	if m.busy {
		return false, nil
	}
	switch m.mode {
	case addContactModeLookup:
		cmd := m.updateLookupInput(msg)
		return cmd != nil, cmd
	case addContactModeInviteAccept:
		cmd := m.updateInviteInput(msg)
		return cmd != nil, cmd
	}
	return false, nil
}

func (m *addContactModal) updateKey(msg tea.KeyMsg) tea.Cmd {
	if m.busy {
		if msg.Type == tea.KeyEsc && m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		return nil
	}

	switch m.mode {
	case addContactModeChooser:
		return m.updateChooserKey(msg)
	case addContactModePaste:
		return m.updatePasteKey(msg)
	case addContactModeLookup:
		return m.updateLookupKey(msg)
	case addContactModeInviteChoice:
		return m.updateInviteChoiceKey(msg)
	case addContactModeInviteStart:
		return m.updateInviteStartKey(msg)
	case addContactModeInviteAccept:
		return m.updateInviteAcceptKey(msg)
	}
	return nil
}

func (m *addContactModal) syncLookupValue() {
	m.value = m.lookupInput.Value()
}

func (m *addContactModal) syncInviteValue() {
	m.value = m.inviteInput.Value()
}

func (m *addContactModal) setMode(mode addContactMode) tea.Cmd {
	m.mode = mode
	m.selected = 0
	m.error = ""
	m.preview = nil
	m.value = ""
	m.code = ""
	m.lookupInput.Blur()
	m.inviteInput.Blur()
	switch mode {
	case addContactModeLookup:
		m.lookupInput.Reset()
		return m.lookupInput.Focus()
	case addContactModeInviteAccept:
		m.inviteInput.Reset()
		return m.inviteInput.Focus()
	}
	return nil
}

func (m *addContactModal) startAsync(cancel context.CancelFunc) {
	m.busy = true
	m.cancel = cancel
	m.error = ""
}

func (m *addContactModal) finishAsync(err error) {
	m.busy = false
	m.cancel = nil
	if err == nil {
		m.error = ""
		return
	}
	m.error = err.Error()
}

func (m *addContactModal) relayConfigured() bool {
	return m.deps.relayConfigured != nil && m.deps.relayConfigured()
}

// Body renders the active mode's body inside the palette frame. It is the
// palette-view entry point into the existing mode renderers.
func (m *addContactModal) Body(width, height int) string {
	body := m.View(width, height)
	if m.error != "" {
		body = strings.Join([]string{body, style.StatusBad.Width(width).Render(m.error)}, "\n\n")
	}
	return body
}

func (m *addContactModal) Subtitle() string {
	return "Secure onboarding for a new peer."
}

func (m *addContactModal) Footer() string {
	return m.footer()
}

func (m *Model) relayConfigured() bool {
	return strings.TrimSpace(m.relay.url) != ""
}

func completeAddContactCmd(contact *identity.Contact, toastText string) tea.Cmd {
	return func() tea.Msg {
		return addContactCompletedMsg{contact: contact, toastText: toastText}
	}
}

func addContactToastText(source string, contact *identity.Contact) string {
	return fmt.Sprintf("added %s contact %s", source, contact.AccountID)
}

func (m *Model) handleAddContactCompletedMsg(msg addContactCompletedMsg) (*Model, tea.Cmd) {
	m.upsertContact(msg.contact)
	m.selectContact(msg.contact.AccountID)
	m.activateSelectedContact()
	m.commandPalette.Close()
	if m.ui.focus == focusChat {
		m.input.Focus()
	}
	m.pushToast(msg.toastText, ToastInfo)
	return m, nil
}

func (m *Model) handleAddContactImportResult(msg addContactImportResultMsg) (*Model, tea.Cmd) {
	return m, m.addContact.handleImportResult(msg)
}

func (m *Model) handleAddContactLookupResult(msg addContactLookupResultMsg) (*Model, tea.Cmd) {
	return m, m.addContact.handleLookupResult(msg)
}

func (m *Model) handleAddContactInviteExchangeResult(msg addContactInviteExchangeResultMsg) (*Model, tea.Cmd) {
	return m, m.addContact.handleInviteExchangeResult(msg)
}

func (m *Model) handleAddContactInviteStarted(msg addContactInviteStartedMsg) (*Model, tea.Cmd) {
	return m, m.addContact.handleInviteStarted(msg)
}
