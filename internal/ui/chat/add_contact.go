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

type addContactClosedMsg struct {
	keepStatus bool
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
	open        bool
	mode        addContactMode
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

func (m *addContactModal) Init() tea.Cmd {
	return nil
}

func (m *addContactModal) Open() tea.Cmd {
	m.open = true
	m.mode = addContactModeChooser
	m.value = ""
	m.code = ""
	m.error = ""
	m.busy = false
	m.preview = nil
	m.cancel = nil
	m.lookupInput = newAddContactInput("mailbox")
	m.inviteInput = newAddContactInput("invite code")
	return nil
}

func (m *addContactModal) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	m.open = false
	m.mode = addContactModeChooser
	m.value = ""
	m.code = ""
	m.error = ""
	m.busy = false
	m.preview = nil
	m.cancel = nil
	m.lookupInput = newAddContactInput("mailbox")
	m.inviteInput = newAddContactInput("invite code")
}

func (m *Model) openAddContactModal() {
	m.addContact = newAddContactModal(addContactDeps{
		messaging:         m.messaging,
		ensureRelayClient: m.ensureRelayClient,
		relayConfigured:   m.relayConfigured,
	})
	m.addContact.Open()
	m.input.Blur()
}

func (m *Model) closeAddContactModal(keepStatus bool) {
	m.addContact.Close()
	if !keepStatus {
		m.pushToast("add contact cancelled", ToastInfo)
	}
	m.input.Focus()
}

func (m *Model) finishAddContact(contact *identity.Contact, toastText string) {
	m.upsertContact(contact)
	m.selectContact(contact.AccountID)
	m.activateSelectedContact()
	m.closeAddContactModal(true)
	m.pushToast(toastText, ToastInfo)
}

func (m *Model) handleAddContactCompletedMsg(msg addContactCompletedMsg) (*Model, tea.Cmd) {
	m.finishAddContact(msg.contact, msg.toastText)
	return m, nil
}

func (m *Model) handleAddContactClosedMsg(msg addContactClosedMsg) (*Model, tea.Cmd) {
	m.closeAddContactModal(msg.keepStatus)
	return m, nil
}

func (m *addContactModal) Update(msg tea.Msg) (bool, tea.Cmd) {
	if !m.open {
		return false, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return true, m.updateKey(msg)
	case addContactImportResultMsg:
		return true, m.handleImportResult(msg)
	case addContactLookupResultMsg:
		return true, m.handleLookupResult(msg)
	case addContactInviteExchangeResultMsg:
		return true, m.handleInviteExchangeResult(msg)
	case addContactInviteStartedMsg:
		return true, m.handleInviteStarted(msg)
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

func (m *Model) relayConfigured() bool {
	return strings.TrimSpace(m.relay.url) != ""
}

func completeAddContactCmd(contact *identity.Contact, toastText string) tea.Cmd {
	return func() tea.Msg {
		return addContactCompletedMsg{contact: contact, toastText: toastText}
	}
}

func closeAddContactCmd(keepStatus bool) tea.Cmd {
	return func() tea.Msg {
		return addContactClosedMsg{keepStatus: keepStatus}
	}
}

func addContactToastText(source string, contact *identity.Contact) string {
	return fmt.Sprintf("added %s contact %s", source, contact.AccountID)
}
