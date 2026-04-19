package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	var view string
	if m.width < narrowThreshold {
		if m.focus == focusSidebar {
			view = m.renderSidebar()
		} else {
			view = m.renderConversation()
		}
	} else {
		left := m.renderSidebar()
		right := m.renderConversation()
		view = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	if m.helpOpen {
		return m.renderHelpModal(view)
	}
	if m.addContact.open {
		return m.renderAddContactModal(view)
	}
	if m.peerDetailOpen {
		return m.renderPeerDetailModal(view)
	}
	return view
}

func (m *Model) renderSidebar() string {
	title := style.Bold.Render("Contacts")
	shortcut := "up/down browse  enter open  ctrl+n add  tab switch pane"
	if m.addContact.open {
		shortcut = "add contact open  ctrl+s import  esc cancel"
	}
	lines := []string{title, style.Muted.Render(shortcut)}
	if len(m.contacts) == 0 {
		lines = append(lines, style.Muted.Render("No contacts. Press ctrl+n."))
	} else {
		for idx, contact := range m.contacts {
			lines = append(lines, m.renderSidebarRow(idx, contact))
		}
	}
	content := strings.Join(lines, "\n")
	if m.width < narrowThreshold {
		return lipgloss.NewStyle().Width(m.sidebarWidth).Height(max(1, m.height)).Render(content)
	}
	return style.SidebarBorder.Width(m.sidebarWidth).Height(max(1, m.height)).Render(content)
}

func (m *Model) renderSidebarRow(idx int, contact contactItem) string {
	isCursor := idx == m.selectedIndex
	isActive := contact.Mailbox == m.recipientMailbox
	cursorGlyph := " "
	if isCursor {
		cursorGlyph = style.PeerAccentStyle(contact.Fingerprint).Render(style.GlyphCursorRow)
	}
	activeGlyph := " "
	if isActive {
		activeGlyph = style.StatusOk.Render(style.GlyphActiveChat)
	}
	marker := cursorGlyph + activeGlyph
	mailbox := contact.Mailbox
	if isActive {
		mailbox = style.PeerAccentStyle(contact.Fingerprint).Bold(true).Render(mailbox)
	}
	badge := ""
	if n := m.Unread(contact.Mailbox); n > 0 {
		badge = " " + style.UnreadBadge.Render(fmt.Sprintf("%s%d", style.GlyphUnreadDot, n))
	}
	statusStyle := style.UnverifiedWarn
	statusText := identity.TrustLabel(contact.TrustSource, contact.Verified)
	if contact.Verified {
		statusStyle = style.VerifiedOk
	}
	if contact.TrustSource == identity.TrustSourceUnverified {
		statusStyle = style.UnverifiedWarn
	}
	return fmt.Sprintf("%s %s%s  %s", marker, mailbox, badge, statusStyle.Render(statusText))
}

func (m *Model) renderConversation() string {
	width := m.conversationWidth()
	if m.recipientMailbox == "" {
		return m.renderEmptyConversation(width)
	}
	if m.filePicker.open {
		return m.renderFilePicker(width)
	}
	peerHeading := style.PeerAccentStyle(m.peerFingerprint).Bold(true).Render(m.recipientMailbox)
	header := []string{
		peerHeading,
		style.Muted.Render("ctrl+o attach  |  ctrl+p peer detail  |  ? help"),
		m.viewport.View(),
		m.renderJumpPill(width),
		m.renderToast(),
		m.renderTypingIndicator(),
		m.input.View(),
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(header, "\n"))
}

func (m *Model) renderEmptyConversation(width int) string {
	if len(m.contacts) == 0 {
		return m.renderWelcomeCard(width)
	}
	lines := []string{
		style.Bold.Render("No chat selected"),
		style.Muted.Render("Pick a contact from the sidebar, or press ctrl+n to import another."),
		"",
		m.input.View(),
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderWelcomeCard(width int) string {
	cardWidth := min(max(40, width-4), max(30, width-2))
	title := style.Bright.Bold(true).Render("Welcome to Pando")
	rule := style.Muted.Render(strings.Repeat("─", max(1, lipgloss.Width(title))))
	step := func(n, label, hint string) string {
		head := style.StatusInfo.Bold(true).Render(n) + "  " + style.Bold.Render(label)
		return head + "\n      " + style.Muted.Render(hint)
	}
	body := strings.Join([]string{
		title,
		rule,
		"",
		step("1.", "share your code", "pando identity invite-code --copy"),
		step("2.", "import theirs", "ctrl+n  (or pando contact add --paste)"),
		step("3.", "start typing", "pick them in the sidebar, then hit enter"),
	}, "\n")
	card := style.Modal.Width(cardWidth).Padding(1, 2).Render(body)
	return lipgloss.NewStyle().Width(width).Render(card + "\n\n" + m.input.View())
}

type helpShortcut struct {
	keys string
	desc string
}

var helpSectionNavigation = []helpShortcut{{"↑ ↓", "browse contacts"}, {"⏎", "open selected chat / send"}, {"tab", "switch pane"}, {"end / G", "jump to latest message"}, {"ctrl+c", "quit"}}

var helpSectionMessaging = []helpShortcut{{"ctrl+n", "add contact"}, {"ctrl+o", "attach file"}, {"ctrl+p", "peer detail"}, {"/send-photo <path>", "attach photo via path"}, {"/send-voice <path>", "attach voice via path"}, {"/send-file <path>", "attach file via path"}, {"ctrl+u", "clear input"}, {"?", "toggle this help"}, {"esc", "close overlay"}}

func (m *Model) renderHelpModal(base string) string {
	modalWidth := min(max(64, m.width*2/3), max(40, m.width-6))
	modalHeight := min(max(18, m.height*2/3), max(14, m.height-4))
	if modalWidth <= 0 || modalHeight <= 0 {
		return base
	}
	colWidth := max(20, (modalWidth-6)/2)
	title := style.Bright.Bold(true).Render("Help")
	navTitle := style.Bold.Render("Navigation")
	msgTitle := style.Bold.Render("Messaging")
	nav := renderHelpColumn(helpSectionNavigation, colWidth)
	msg := renderHelpColumn(helpSectionMessaging, colWidth)
	columns := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{navTitle, nav}, "\n")), "  ", lipgloss.NewStyle().Width(colWidth).Render(strings.Join([]string{msgTitle, msg}, "\n")))
	footer := style.Subtle.Render("? or esc to close")
	body := strings.Join([]string{title, columns, footer}, "\n\n")
	modal := style.Modal.Width(modalWidth).Padding(1, 2).Render(body)
	background := style.Faint.Render(base)
	return strings.Join([]string{background, lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)}, "\n")
}

func (m *Model) renderPeerDetailModal(base string) string {
	modalWidth := min(max(56, m.width*2/3), max(40, m.width-6))
	if modalWidth <= 0 || m.height <= 0 {
		return base
	}
	bodyWidth := max(24, modalWidth-6)
	title := style.Bright.Bold(true).Render("Peer detail")
	mailboxLine := style.PeerAccentStyle(m.peerFingerprint).Bold(true).Render(m.recipientMailbox)
	verifyLabel := identity.TrustLabel(m.peerTrustSource, m.peerVerified)
	verifyLine := style.UnverifiedWarn.Render(verifyLabel)
	if m.peerVerified {
		verifyLine = style.VerifiedOk.Render(verifyLabel)
	}
	fullFp := m.peerFingerprint
	shortFp := style.FormatFingerprintShort(fullFp)
	fpLong := style.FormatFingerprint(fullFp)
	deviceCount := 0
	if contact, err := m.messaging.Contact(m.recipientMailbox); err == nil {
		deviceCount = len(contact.ActiveDevices())
	}
	row := func(label, value string) string {
		padLabel := style.Muted.Render(label)
		return padLabel + "  " + value
	}
	parts := []string{title, mailboxLine + "  " + verifyLine, "", row("fingerprint", style.Bright.Render(fpLong)), row("short     ", style.Muted.Render(shortFp)), row("devices   ", style.Bright.Render(fmt.Sprintf("%d active", deviceCount))), row("relay     ", style.Muted.Render(m.relayURL)), "", style.Subtle.Render("ctrl+p or esc to close")}
	body := lipgloss.NewStyle().Width(bodyWidth).Render(strings.Join(parts, "\n"))
	modal := style.Modal.Width(modalWidth).Padding(1, 2).Render(body)
	background := style.Faint.Render(base)
	return strings.Join([]string{background, lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)}, "\n")
}

func renderHelpColumn(entries []helpShortcut, width int) string {
	keyWidth := 0
	for _, e := range entries {
		if w := lipgloss.Width(e.keys); w > keyWidth {
			keyWidth = w
		}
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		pad := keyWidth - lipgloss.Width(e.keys)
		if pad < 0 {
			pad = 0
		}
		keys := style.StatusInfo.Render(e.keys)
		lines = append(lines, keys+strings.Repeat(" ", pad+2)+style.Muted.Render(e.desc))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderToast() string {
	if m.toast == nil {
		return ""
	}
	switch m.toast.level {
	case ToastWarn:
		return style.StatusWarn.Render(m.toast.text)
	case ToastBad:
		return style.StatusBad.Render(m.toast.text)
	default:
		return style.Muted.Render(m.toast.text)
	}
}
