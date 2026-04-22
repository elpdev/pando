package chat

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *addContactModal) updateChooserKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveSelection(-1, len(m.chooserItems()))
		return nil
	case tea.KeyDown, tea.KeyCtrlN:
		m.moveSelection(1, len(m.chooserItems()))
		return nil
	case tea.KeyEnter:
		return m.activateSelectedChooserItem()
	case tea.KeyRunes:
		switch strings.ToLower(string(msg.Runes)) {
		case "p":
			m.selected = 0
		case "l":
			m.selected = 1
		case "i":
			m.selected = 2
		default:
			return nil
		}
		return m.activateSelectedChooserItem()
	default:
		return nil
	}
}

func (m *addContactModal) updatePasteKey(msg tea.KeyMsg) tea.Cmd {
	if m.preview != nil {
		switch msg.Type {
		case tea.KeyEsc:
			m.preview = nil
			m.error = ""
		case tea.KeyCtrlS:
			trimmed := strings.TrimSpace(m.value)
			m.startAsync(nil)
			return importPasteCmd(m.deps.messaging, trimmed)
		}
		return nil
	}

	switch msg.Type {
	case tea.KeyEsc:
		return m.setMode(addContactModeChooser)
	case tea.KeyCtrlS:
		trimmed := strings.TrimSpace(m.value)
		if trimmed == "" {
			m.error = "invite input is empty"
			return nil
		}
		contact, err := m.deps.messaging.PreviewContactInviteText(trimmed)
		if err != nil {
			m.error = err.Error()
			return nil
		}
		m.error = ""
		m.preview = contact
		return nil
	case tea.KeyEnter, tea.KeyCtrlJ:
		m.appendPasteText("\n")
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
		m.deletePasteRune()
	case tea.KeyCtrlU:
		m.value = ""
		m.error = ""
	case tea.KeyRunes:
		m.appendPasteText(string(msg.Runes))
	}
	return nil
}

func (m *addContactModal) updateLookupKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		return m.setMode(addContactModeChooser)
	case tea.KeyEnter, tea.KeyCtrlS:
		mailbox := strings.TrimSpace(m.lookupInput.Value())
		if mailbox == "" {
			m.error = "mailbox is required"
			return nil
		}
		m.syncLookupValue()
		return m.startLookup(mailbox)
	}
	return m.updateLookupInput(msg)
}

func (m *addContactModal) updateInviteChoiceKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		return m.setMode(addContactModeChooser)
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveSelection(-1, len(m.inviteChoiceItems()))
		return nil
	case tea.KeyDown, tea.KeyCtrlN:
		m.moveSelection(1, len(m.inviteChoiceItems()))
		return nil
	case tea.KeyEnter:
		return m.activateSelectedInviteChoiceItem()
	case tea.KeyRunes:
		switch strings.ToLower(string(msg.Runes)) {
		case "s":
			m.selected = 0
		case "a":
			m.selected = 1
		default:
			return nil
		}
		return m.activateSelectedInviteChoiceItem()
	default:
		return nil
	}
}

func (m *addContactModal) updateInviteStartKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type != tea.KeyEsc {
		return nil
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	return m.setMode(addContactModeInviteChoice)
}

func (m *addContactModal) updateInviteAcceptKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		return m.setMode(addContactModeInviteChoice)
	case tea.KeyEnter, tea.KeyCtrlS:
		code := strings.TrimSpace(m.inviteInput.Value())
		if code == "" {
			m.error = "invite code is required"
			return nil
		}
		m.syncInviteValue()
		return m.acceptInvite(code)
	}
	return m.updateInviteInput(msg)
}

func (m *addContactModal) updateLookupInput(msg tea.Msg) tea.Cmd {
	before := m.lookupInput.Value()
	var cmd tea.Cmd
	m.lookupInput, cmd = m.lookupInput.Update(msg)
	m.syncLookupValue()
	if m.lookupInput.Value() != before {
		m.error = ""
	}
	return cmd
}

func (m *addContactModal) updateInviteInput(msg tea.Msg) tea.Cmd {
	before := m.inviteInput.Value()
	var cmd tea.Cmd
	m.inviteInput, cmd = m.inviteInput.Update(msg)
	m.syncInviteValue()
	if m.inviteInput.Value() != before {
		m.error = ""
	}
	return cmd
}

func (m *addContactModal) startLookup(mailbox string) tea.Cmd {
	m.startAsync(nil)
	return lookupContactCmd(m.deps.messaging, m.deps.ensureRelayClient, mailbox)
}

func (m *addContactModal) startInvite() tea.Cmd {
	code, err := generateInviteCode()
	if err != nil {
		return func() tea.Msg { return addContactInviteStartedMsg{err: err} }
	}
	ctx, cancel := context.WithTimeout(context.Background(), inviteExchangeTimeout)
	m.code = code
	m.startAsync(cancel)
	return tea.Batch(
		func() tea.Msg { return addContactInviteStartedMsg{code: code} },
		runInviteExchangeCmd(ctx, m.deps.messaging, m.deps.ensureRelayClient, code),
	)
}

func (m *addContactModal) acceptInvite(code string) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), inviteExchangeTimeout)
	m.startAsync(cancel)
	return runInviteExchangeCmd(ctx, m.deps.messaging, m.deps.ensureRelayClient, code)
}

func (m *addContactModal) handleImportResult(msg addContactImportResultMsg) tea.Cmd {
	m.finishAsync(msg.err)
	if msg.err != nil {
		return nil
	}
	return completeAddContactCmd(msg.contact, addContactToastText("verified", msg.contact))
}

func (m *addContactModal) handleLookupResult(msg addContactLookupResultMsg) tea.Cmd {
	m.finishAsync(msg.err)
	if msg.err != nil {
		return nil
	}
	return completeAddContactCmd(msg.contact, addContactToastText("relay-directory", msg.contact))
}

func (m *addContactModal) handleInviteExchangeResult(msg addContactInviteExchangeResultMsg) tea.Cmd {
	m.busy = false
	m.cancel = nil
	if msg.cancelled {
		m.error = "cancelled"
		return nil
	}
	if msg.err != nil {
		m.error = msg.err.Error()
		return nil
	}
	return completeAddContactCmd(msg.contact, addContactToastText("invite-code", msg.contact))
}

func (m *addContactModal) handleInviteStarted(msg addContactInviteStartedMsg) tea.Cmd {
	if msg.err != nil {
		m.finishAsync(msg.err)
		return nil
	}
	m.code = msg.code
	return nil
}

func (m *addContactModal) appendPasteText(text string) {
	if text == "" || len([]rune(m.value)) >= addContactLimit {
		return
	}
	remaining := addContactLimit - len([]rune(m.value))
	runes := []rune(text)
	if len(runes) > remaining {
		runes = runes[:remaining]
	}
	m.value += string(runes)
	m.error = ""
}

func (m *addContactModal) deletePasteRune() {
	runes := []rune(m.value)
	if len(runes) == 0 {
		return
	}
	m.value = string(runes[:len(runes)-1])
	m.error = ""
}

func (m *addContactModal) moveSelection(delta, total int) {
	if total <= 0 {
		m.selected = 0
		return
	}
	m.selected = (m.selected + delta) % total
	if m.selected < 0 {
		m.selected += total
	}
}

func (m *addContactModal) activateSelectedChooserItem() tea.Cmd {
	items := m.chooserItems()
	if len(items) == 0 || m.selected < 0 || m.selected >= len(items) {
		return nil
	}
	item := items[m.selected]
	if item.disabled {
		m.error = "no relay configured"
		return nil
	}
	switch item.key {
	case "p":
		return m.setMode(addContactModePaste)
	case "l":
		return m.setMode(addContactModeLookup)
	case "i":
		return m.setMode(addContactModeInviteChoice)
	default:
		return nil
	}
}

func (m *addContactModal) activateSelectedInviteChoiceItem() tea.Cmd {
	items := m.inviteChoiceItems()
	if len(items) == 0 || m.selected < 0 || m.selected >= len(items) {
		return nil
	}
	switch items[m.selected].key {
	case "s":
		m.mode = addContactModeInviteStart
		m.selected = 0
		m.error = ""
		m.value = ""
		m.code = ""
		return m.startInvite()
	case "a":
		return m.setMode(addContactModeInviteAccept)
	default:
		return nil
	}
}
