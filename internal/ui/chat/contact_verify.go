package chat

import tea "github.com/charmbracelet/bubbletea"

type contactVerifyModal struct {
	open        bool
	mailbox     string
	fingerprint string
}

type contactVerifyConfirmedMsg struct{}

type contactVerifyClosedMsg struct{}

func (m *contactVerifyModal) Open(mailbox, fingerprint string) {
	m.open = true
	m.mailbox = mailbox
	m.fingerprint = fingerprint
}

func (m *contactVerifyModal) Close() {
	*m = contactVerifyModal{}
}

func (m *contactVerifyModal) Update(msg tea.Msg) (bool, tea.Cmd) {
	if !m.open {
		return false, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch keyMsg.Type {
	case tea.KeyEsc:
		return true, func() tea.Msg { return contactVerifyClosedMsg{} }
	case tea.KeyEnter:
		return true, func() tea.Msg { return contactVerifyConfirmedMsg{} }
	}
	switch keyMsg.String() {
	case "y", "v":
		return true, func() tea.Msg { return contactVerifyConfirmedMsg{} }
	case "n", "q":
		return true, func() tea.Msg { return contactVerifyClosedMsg{} }
	default:
		return true, nil
	}
}

func (m *Model) canVerifyActiveContact() bool {
	return m.peer.mailbox != "" && !m.peer.isRoom
}

func (m *Model) openContactVerifyModal() tea.Cmd {
	if !m.canVerifyActiveContact() {
		return nil
	}
	m.contactVerify.Open(m.peer.mailbox, m.peer.fingerprint)
	m.input.Blur()
	return nil
}

func (m *Model) closeContactVerifyModal(keepStatus bool) {
	m.contactVerify.Close()
	if !keepStatus {
		m.pushToast("verify contact cancelled", ToastInfo)
	}
	if m.ui.focus == focusChat {
		m.input.Focus()
	}
}

func (m *Model) handleContactVerifyConfirmedMsg(_ contactVerifyConfirmedMsg) (*Model, tea.Cmd) {
	contact, err := m.messaging.MarkContactVerified(m.contactVerify.mailbox, true)
	if err != nil {
		m.pushToast("verify contact failed: "+err.Error(), ToastBad)
		return m, nil
	}
	m.upsertContact(contact)
	m.syncRecipientDetails()
	m.closeContactVerifyModal(true)
	m.pushToast("verified contact "+contact.AccountID, ToastInfo)
	return m, nil
}

func (m *Model) handleContactVerifyClosedMsg(_ contactVerifyClosedMsg) (*Model, tea.Cmd) {
	m.closeContactVerifyModal(false)
	return m, nil
}
