package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/ui/style"
)

type addContactMenuItem struct {
	key      string
	label    string
	hint     string
	disabled bool
}

func (m addContactModal) Overlay(_ string, width, height int) string {
	modalWidth := min(max(58, width*2/3), max(40, width-6))
	modalHeight := min(max(15, height*2/3), max(12, height-4))
	if modalWidth <= 0 || modalHeight <= 0 {
		return ""
	}

	title := style.ModalTitle.Render("Add Contact")
	parts := []string{title, m.View(modalWidth-6, modalHeight)}
	if m.error != "" {
		parts = append(parts, style.StatusBad.Width(modalWidth-6).Render(m.error))
	}
	parts = append(parts, style.Subtle.Render(m.footer()))

	modal := style.Modal.Width(modalWidth).Padding(1, 2).Render(strings.Join(parts, "\n\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceBackground(style.BackdropTint))
}

func (m addContactModal) View(width, modalHeight int) string {
	switch m.mode {
	case addContactModeChooser:
		return m.renderChooser(width)
	case addContactModePaste:
		return m.renderPaste(width, modalHeight)
	case addContactModeLookup:
		return m.renderLookup(width)
	case addContactModeInviteChoice:
		return m.renderInviteChoice(width)
	case addContactModeInviteStart:
		return m.renderInviteStart(width)
	case addContactModeInviteAccept:
		return m.renderInviteAccept(width)
	default:
		return ""
	}
}

func (m addContactModal) renderChooser(width int) string {
	return m.renderMenu(width,
		style.Dim.Width(width).Render("How do you want to add this contact?"),
		[]addContactMenuItem{
			{key: "p", label: "paste invite", hint: "paste an invite code or bundle"},
			{key: "l", label: "lookup mailbox", hint: "resolve via the trusted relay directory", disabled: !m.relayConfigured()},
			{key: "i", label: "invite exchange", hint: "share a short code with a peer", disabled: !m.relayConfigured()},
		},
	)
}

func (m addContactModal) renderPaste(width, modalHeight int) string {
	if m.preview != nil {
		return m.renderPreview(width)
	}
	inputHeight := max(5, modalHeight-12)
	description := style.Dim.Width(width).Render("Paste a raw invite code or the full invite text. Pressing ctrl+s parses it and shows a preview before anything is saved.")
	return strings.Join([]string{description, m.renderPasteEditor(width, inputHeight)}, "\n\n")
}

func (m addContactModal) renderLookup(width int) string {
	m.lookupInput.Width = max(1, width-4)
	status := ""
	if m.busy {
		status = style.Subtle.Render("looking up...")
	}
	return renderAddContactForm(
		width,
		"Look up a contact in the trusted relay directory. The peer must have run `pando contact publish-directory` first.",
		style.InputBorder.Width(width).Padding(0, 1).Render(m.lookupInput.View()),
		status,
	)
}

func (m addContactModal) renderInviteChoice(width int) string {
	return m.renderMenu(width,
		style.Dim.Width(width).Render("Invite exchange: are you creating a new code or accepting one?"),
		[]addContactMenuItem{
			{key: "s", label: "start", hint: "generate a code and wait for the peer"},
			{key: "a", label: "accept", hint: "enter a code the peer shared with you"},
		},
	)
}

func (m addContactModal) renderInviteStart(width int) string {
	codeValue := style.Bright.Bold(true).Render(m.code)
	if m.code == "" {
		codeValue = style.Muted.Render("generating...")
	}
	status := ""
	if m.busy {
		status = style.Subtle.Render("waiting for peer...")
	}
	return renderAddContactForm(
		width,
		"Share this code with the other person, then wait while they accept it. Press Esc to cancel, or `a` to switch to accepting their code instead.",
		style.InputBorder.Width(width).Padding(0, 1).Render(style.Muted.Render("invite code")+"  "+codeValue),
		status,
	)
}

func (m addContactModal) renderInviteAccept(width int) string {
	m.inviteInput.Width = max(1, width-4)
	status := ""
	if m.busy {
		status = style.Subtle.Render("waiting for peer...")
	}
	return renderAddContactForm(
		width,
		"Enter the code the other person shared. Press Enter to submit.",
		style.InputBorder.Width(width).Padding(0, 1).Render(m.inviteInput.View()),
		status,
	)
}

func (m addContactModal) renderPreview(width int) string {
	c := m.preview
	if c == nil {
		return ""
	}
	fp := c.Fingerprint()
	deviceCount := len(c.ActiveDevices())
	row := func(label, value string) string {
		return style.Muted.Render(label) + "  " + value
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join([]string{
		style.Bold.Render("parsed invite"),
		row("account    ", style.PeerAccentStyle(fp).Bold(true).Render(c.AccountID)),
		row("fingerprint", style.Bright.Render(style.FormatFingerprint(fp))),
		row("devices    ", style.Bright.Render(fmt.Sprintf("%d active", deviceCount))),
	}, "\n"))
}

func (m addContactModal) renderPasteEditor(width, height int) string {
	content := m.value
	if content == "" {
		content = style.Muted.Render("account: alice\nfingerprint: ...\ninvite-code: ...")
	} else {
		content += style.CursorBlock.Render("█")
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	visible := strings.Join(lines, "\n")
	meta := style.Subtle.Render(fmt.Sprintf("%d chars", len([]rune(m.value))))
	if len(m.value) >= addContactLimit {
		meta = style.StatusBad.Render(fmt.Sprintf("input limit reached (%d chars)", addContactLimit))
	}
	box := style.InputBorder.Width(width).Height(height).Padding(0, 1).Render(visible)
	return strings.Join([]string{box, meta}, "\n")
}

func (m addContactModal) renderMenu(width int, description string, items []addContactMenuItem) string {
	lines := []string{description}
	for _, item := range items {
		label := style.Bright.Render(item.label)
		hint := style.Muted.Render(item.hint)
		if item.disabled {
			label = style.Muted.Render(item.label)
			hint = style.Muted.Render("(no relay configured)")
		}
		lines = append(lines, style.Bold.Render("["+item.key+"]")+"  "+label+"  "+hint)
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func renderAddContactForm(width int, description string, boxContent string, status string) string {
	parts := []string{style.Dim.Width(width).Render(description), boxContent}
	if status != "" {
		parts = append(parts, status)
	}
	return strings.Join(parts, "\n\n")
}

func (m addContactModal) footer() string {
	if m.busy {
		return "esc cancel"
	}
	switch m.mode {
	case addContactModeChooser:
		return "p paste   l lookup   i invite   esc cancel"
	case addContactModePaste:
		if m.preview != nil {
			return "ctrl+s import and verify   esc back"
		}
		return "enter newline  ctrl+s preview  ctrl+u clear  esc back"
	case addContactModeLookup:
		return "enter submit  ctrl+u clear  esc back"
	case addContactModeInviteChoice:
		return "s start  a accept  esc back"
	case addContactModeInviteStart:
		return "esc back"
	case addContactModeInviteAccept:
		return "enter submit  ctrl+u clear  esc back"
	default:
		return ""
	}
}
