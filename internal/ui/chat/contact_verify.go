package chat

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/ui/style"
)

// contactVerifyModal renders the "mark contact verified" confirmation view
// inside the command palette frame. State is captured at Open time from the
// active peer and reset on Close.
type contactVerifyModal struct {
	mailbox     string
	fingerprint string
}

// contactVerifyConfirmedMsg is dispatched when the user confirms the verify
// prompt. Model responds by marking the contact verified and closing the
// palette with a success toast.
type contactVerifyConfirmedMsg struct {
	mailbox string
}

func (m *contactVerifyModal) Open(ctx viewOpenCtx) tea.Cmd {
	m.mailbox = ctx.peerMailbox
	m.fingerprint = ctx.peerFingerprint
	return nil
}

func (m *contactVerifyModal) Close() {
	*m = contactVerifyModal{}
}

func (m *contactVerifyModal) Update(msg tea.Msg) (bool, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch key.Type {
	case tea.KeyEnter:
		mailbox := m.mailbox
		return true, func() tea.Msg { return contactVerifyConfirmedMsg{mailbox: mailbox} }
	}
	switch key.String() {
	case "y", "v":
		mailbox := m.mailbox
		return true, func() tea.Msg { return contactVerifyConfirmedMsg{mailbox: mailbox} }
	case "n", "q":
		return true, paletteBackCmd()
	}
	return false, nil
}

func (m *contactVerifyModal) Body(width, _ int) string {
	bodyWidth := max(1, width)
	rows := []string{
		style.PaletteMeta.Width(bodyWidth).Render("Mark this contact as manually verified after confirming the fingerprint out-of-band."),
		style.Muted.Render("Mailbox") + "\n" + style.PaletteInput.Width(bodyWidth).Padding(0, 1).Render(m.mailbox),
		style.Muted.Render("Fingerprint") + "\n" + style.PaletteInput.Width(bodyWidth).Padding(0, 1).Render(style.FormatFingerprint(m.fingerprint)),
	}
	return strings.Join(rows, "\n\n")
}

func (m *contactVerifyModal) Subtitle() string {
	return "This confirms you trust this contact's identity."
}

func (m *contactVerifyModal) Footer() string {
	return "enter verify · y verify · esc cancel"
}

func (m *Model) canVerifyActiveContact() bool {
	return m.peer.mailbox != "" && !m.peer.isRoom
}

func (m *Model) handleContactVerifyConfirmedMsg(msg contactVerifyConfirmedMsg) (*Model, tea.Cmd) {
	contact, err := m.messaging.MarkContactVerified(msg.mailbox, true)
	if err != nil {
		m.pushToast("verify contact failed: "+err.Error(), ToastBad)
		m.commandPalette.Close()
		if m.ui.focus == focusChat {
			m.input.Focus()
		}
		return m, nil
	}
	m.upsertContact(contact)
	m.syncRecipientDetails()
	m.commandPalette.Close()
	if m.ui.focus == focusChat {
		m.input.Focus()
	}
	m.pushToast("verified contact "+contact.AccountID, ToastInfo)
	return m, nil
}
