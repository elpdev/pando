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
	modalWidth := paletteWidth(width)
	modalHeight := paletteHeight(height)
	if modalWidth <= 0 || modalHeight <= 0 {
		return ""
	}

	bodyParts := []string{m.View(modalWidth-6, modalHeight)}
	if m.error != "" {
		bodyParts = append(bodyParts, style.StatusBad.Width(modalWidth-6).Render(m.error))
	}
	return renderPaletteOverlay(width, height, "Add Contact", "Secure onboarding for a new peer.", bodyParts, m.footer())
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
	lines := []string{style.PaletteMeta.Width(width).Render("Choose a trust path, then confirm it from the keyboard.")}
	for idx, item := range m.chooserItems() {
		detail := item.hint
		meta := strings.ToUpper(item.key)
		if item.disabled {
			detail = "relay required"
			meta = "OFF"
		}
		lines = append(lines, renderPaletteListItem(width, idx == m.selected, item.label, detail, meta))
	}
	return strings.Join(lines, "\n")
}

func (m addContactModal) renderPaste(width, modalHeight int) string {
	if m.preview != nil {
		return m.renderPreview(width)
	}
	inputHeight := max(5, modalHeight-12)
	description := style.PaletteMeta.Width(width).Render("Paste an invite code or the exported invite text. `ctrl+s` previews it before import.")
	return strings.Join([]string{description, m.renderPasteEditor(width, inputHeight)}, "\n\n")
}

func (m addContactModal) renderLookup(width int) string {
	m.lookupInput.Width = max(1, width-4)
	status := ""
	if m.busy {
		status = style.PaletteMeta.Render("looking up...")
	}
	return renderAddContactForm(
		width,
		"Look up a contact in the trusted relay directory. They need to publish their directory entry first.",
		style.PaletteInput.Width(width).Padding(0, 1).Render(m.lookupInput.View()),
		status,
	)
}

func (m addContactModal) renderInviteChoice(width int) string {
	lines := []string{style.PaletteMeta.Width(width).Render("Invite exchange lets two peers verify each other with one short shared code.")}
	for idx, item := range m.inviteChoiceItems() {
		lines = append(lines, renderPaletteListItem(width, idx == m.selected, item.label, item.hint, strings.ToUpper(item.key)))
	}
	return strings.Join(lines, "\n")
}

func (m addContactModal) renderInviteStart(width int) string {
	codeValue := style.Bright.Bold(true).Render(m.code)
	if m.code == "" {
		codeValue = style.Muted.Render("generating...")
	}
	status := ""
	if m.busy {
		status = style.PaletteMeta.Render("waiting for peer...")
	}
	return renderAddContactForm(
		width,
		"Share this code, then wait for the other person to accept it. Press Esc to cancel, or `a` to switch sides.",
		style.PaletteInput.Width(width).Padding(0, 1).Render(style.Muted.Render("invite code")+"  "+codeValue),
		status,
	)
}

func (m addContactModal) renderInviteAccept(width int) string {
	m.inviteInput.Width = max(1, width-4)
	status := ""
	if m.busy {
		status = style.PaletteMeta.Render("waiting for peer...")
	}
	return renderAddContactForm(
		width,
		"Enter the code the other person shared, then press Enter to continue.",
		style.PaletteInput.Width(width).Padding(0, 1).Render(m.inviteInput.View()),
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
	box := style.PaletteInput.Width(width).Height(height).Padding(0, 1).Render(visible)
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
	parts := []string{style.PaletteMeta.Width(width).Render(description), boxContent}
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
		return "up/down move · enter choose · p/l/i jump · esc cancel"
	case addContactModePaste:
		if m.preview != nil {
			return "ctrl+s import and verify · esc back"
		}
		return "enter newline · ctrl+s preview · ctrl+u clear · esc back"
	case addContactModeLookup:
		return "enter submit · ctrl+u clear · esc back"
	case addContactModeInviteChoice:
		return "up/down move · enter choose · s/a jump · esc back"
	case addContactModeInviteStart:
		return "esc back"
	case addContactModeInviteAccept:
		return "enter submit · ctrl+u clear · esc back"
	default:
		return ""
	}
}

func (m addContactModal) chooserItems() []addContactMenuItem {
	return []addContactMenuItem{
		{key: "p", label: "Paste Invite", hint: "Import a full invite code or bundle."},
		{key: "l", label: "Lookup Mailbox", hint: "Resolve a peer from the trusted relay directory.", disabled: !m.relayConfigured()},
		{key: "i", label: "Invite Exchange", hint: "Verify a peer together with a short rendezvous code.", disabled: !m.relayConfigured()},
	}
}

func (m addContactModal) inviteChoiceItems() []addContactMenuItem {
	return []addContactMenuItem{
		{key: "s", label: "Start New Code", hint: "Generate a code and wait for the other person."},
		{key: "a", label: "Accept Shared Code", hint: "Enter a code someone already shared with you."},
	}
}
