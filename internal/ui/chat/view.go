package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) View() string {
	if m.ui.width <= 0 || m.ui.height <= 0 {
		return ""
	}
	var view string
	if m.ui.width < narrowThreshold {
		if m.ui.focus == focusSidebar {
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
		return m.addContact.Overlay(view, m.ui.width, m.ui.height)
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
			isCursor := idx == m.selectedIndex
			isActive := contact.Mailbox == m.peer.mailbox
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
			lines = append(lines, fmt.Sprintf("%s %s%s  %s", marker, mailbox, badge, statusStyle.Render(statusText)))
		}
	}
	content := strings.Join(lines, "\n")
	if m.ui.width < narrowThreshold {
		return lipgloss.NewStyle().Width(m.ui.sidebarWidth).Height(max(1, m.ui.height)).Render(content)
	}
	return style.SidebarBorder.Width(m.ui.sidebarWidth).Height(max(1, m.ui.height)).Render(content)
}

func (m *Model) renderConversation() string {
	width := m.conversationWidth()
	if m.peer.mailbox == "" {
		if len(m.contacts) == 0 {
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
		lines := []string{
			style.Bold.Render("No chat selected"),
			style.Muted.Render("Pick a contact from the sidebar, or press ctrl+n to import another."),
			"",
			m.input.View(),
		}
		return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
	}
	if m.filePicker.open {
		return m.filePicker.View()
	}
	peerHeading := style.PeerAccentStyle(m.peer.fingerprint).Bold(true).Render(m.peer.mailbox)
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
