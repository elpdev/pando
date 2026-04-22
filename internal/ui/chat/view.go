package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/media"
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
		return m.clearInlineMedia(m.renderHelpModal(view))
	}
	if m.addContact.open {
		return m.clearInlineMedia(m.addContact.Overlay(view, m.ui.width, m.ui.height))
	}
	if m.addRelay.open {
		return m.clearInlineMedia(m.addRelay.Overlay(m.ui.width, m.ui.height))
	}
	if m.contactRequestSend.open {
		return m.clearInlineMedia(m.contactRequestSend.Overlay(m.ui.width, m.ui.height))
	}
	if m.contactVerify.open {
		return m.clearInlineMedia(m.contactVerify.Overlay(m.ui.width, m.ui.height))
	}
	if m.contactRequests.open {
		return m.clearInlineMedia(m.contactRequests.Overlay(m.ui.width, m.ui.height))
	}
	if m.commandPalette.open {
		return m.clearInlineMedia(m.commandPalette.View(view, m.ui.width, m.ui.height, m.PeerLabel()))
	}
	if m.peerDetailOpen {
		return m.clearInlineMedia(m.renderPeerDetailModal(view))
	}
	return view
}

func (m *Model) renderSidebar() string {
	title := style.Bold.Render("Contacts")
	lines := []string{title, style.Subtle.Render(fmt.Sprintf("%d chats", len(m.contacts)))}
	if m.pendingRequestsCount > 0 {
		badge := style.UnreadBadge.Render(fmt.Sprintf("%s%d", style.GlyphUnreadDot, m.pendingRequestsCount))
		lines = append(lines, fmt.Sprintf("%s  %s", style.Muted.Render("Requests"), badge))
	}
	if len(m.contacts) == 0 {
		lines = append(lines, style.Muted.Render("No contacts. Press ctrl+p and choose Add contact."))
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
			mailbox := contact.Label
			if isActive {
				if contact.IsRoom {
					mailbox = style.StatusInfo.Bold(true).Render(mailbox)
				} else {
					mailbox = style.PeerAccentStyle(contact.Fingerprint).Bold(true).Render(mailbox)
				}
			}
			badge := ""
			if n := m.Unread(contact.Mailbox); n > 0 {
				badge = " " + style.UnreadBadge.Render(fmt.Sprintf("%s%d", style.GlyphUnreadDot, n))
			}
			if contact.IsRoom {
				status := style.Muted.Render("joined")
				if !contact.Joined {
					status = style.StatusWarn.Render("not joined")
				}
				lines = append(lines, fmt.Sprintf("%s %s%s  %s", marker, mailbox, badge, status))
				continue
			}
			statusStyle := style.UnverifiedWarn
			statusText := identity.TrustLabel(contact.TrustSource, contact.Verified)
			if contact.Verified {
				statusStyle = style.VerifiedOk
			}
			if contact.TrustSource == identity.TrustSourceUnverified {
				statusStyle = style.UnverifiedWarn
			}
			row := fmt.Sprintf("%s %s%s  %s", marker, mailbox, badge, statusStyle.Render(statusText))
			if isActive {
				row = style.ActiveRow.Width(max(1, m.ui.sidebarWidth-1)).Render(row)
			}
			lines = append(lines, row)
		}
	}
	content := strings.Join(lines, "\n")
	if m.ui.width < narrowThreshold {
		return lipgloss.NewStyle().Width(m.ui.sidebarWidth).Height(max(1, m.ui.height)).Render(content)
	}
	border := style.SidebarBorder
	if m.ui.focus == focusSidebar {
		border = style.SidebarBorderFocused
	}
	return border.Width(m.ui.sidebarWidth).Height(max(1, m.ui.height)).Render(content)
}

func (m *Model) renderConversation() string {
	width := m.conversationWidth()
	if m.filePicker.open {
		return m.clearInlineMedia(m.filePicker.View())
	}
	if m.peer.mailbox == "" {
		hasDirectContacts := false
		for _, contact := range m.contacts {
			if !contact.IsRoom {
				hasDirectContacts = true
				break
			}
		}
		if !hasDirectContacts {
			cardWidth := min(max(40, width-4), max(30, width-2))
			title := style.ModalTitle.Render("Welcome to Pando")
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
				step("2.", "import theirs", "ctrl+p, then Add contact  (or pando contact add --paste)"),
				step("3.", "start typing", "pick them in the sidebar, then hit enter"),
			}, "\n")
			card := style.Modal.Width(cardWidth).Padding(1, 2).Render(body)
			return lipgloss.NewStyle().Width(width).Render(card)
		}
		cardWidth := min(max(56, width-12), max(38, width-4))
		title := style.ModalTitle.Render("Why this exists")
		rule := style.Muted.Render(strings.Repeat("─", max(1, lipgloss.Width(title))))
		body := strings.Join([]string{
			title,
			rule,
			"",
			style.Italic.Render("the right to a private conversation is not a feature."),
			style.Italic.Render("it is a prerequisite for free thought."),
			"",
			style.Bold.Render("conversations should belong only to the people having them."),
			"",
			style.Muted.Render("Pick a contact from the sidebar, or press ctrl+p to add one."),
		}, "\n")
		card := style.Modal.Width(cardWidth).Padding(1, 2).Render(body)
		return lipgloss.Place(width, max(1, m.ui.height), lipgloss.Center, lipgloss.Center, card)
	}
	peerHeading := style.PeerAccentStyle(m.peer.fingerprint).Bold(true).Render(m.peer.mailbox)
	accentColor := style.PeerAccent(m.peer.fingerprint)
	if m.peer.isRoom {
		peerHeading = style.StatusInfo.Bold(true).Render(m.peer.label)
		accentColor = style.RoomAccent
	}
	hint := style.Subtle.Render(style.FormatFingerprintShort(m.peer.fingerprint))
	if m.peer.isRoom {
		hint = style.Subtle.Render(fmt.Sprintf("encrypted room  %d/%d members", m.peer.memberCount, messaging.DefaultRoomCap))
	}
	ruleWidth := min(max(8, lipgloss.Width(peerHeading)+6), max(1, width))
	accentRule := lipgloss.NewStyle().Foreground(accentColor).Render(strings.Repeat("━", ruleWidth))
	sections := []string{
		peerHeading,
		accentRule,
		hint,
		m.renderViewport(),
		m.renderJumpPill(width),
		m.renderToast(),
		m.renderPendingAttachment(width),
		m.renderComposer(width),
	}
	return lipgloss.NewStyle().Width(width).Render(joinNonEmpty(sections...))
}

func (m *Model) renderViewport() string {
	view := m.viewport.View()
	return m.clearInlineMedia(view)
}

func (m *Model) clearInlineMedia(view string) string {
	if prefix := media.ViewportPrefix(); prefix != "" {
		return prefix + view
	}
	return view
}

func (m *Model) renderPendingAttachment(width int) string {
	if m.pending == nil {
		return ""
	}
	kind := "file"
	switch m.pending.kind {
	case messaging.AttachmentTypePhoto:
		kind = "photo"
	case messaging.AttachmentTypeVoice:
		kind = "voice"
	}
	label := fmt.Sprintf("%s %s", strings.ToUpper(kind), m.PendingAttachmentLabel())
	clear := style.Muted.Render("esc clear")
	// InputBorder.Width(n) renders at n+2 cols (border chars sit outside Width),
	// so subtract 2 to keep the final block within the outer conversation width.
	inner := max(1, width-2)
	pad := inner - lipgloss.Width(label) - lipgloss.Width(clear) - 2
	if pad < 1 {
		pad = 1
	}
	line := style.StatusInfo.Bold(true).Render(label) + strings.Repeat(" ", pad) + clear
	return style.InputBorder.Width(inner).Padding(0, 1).Render(line)
}

func (m *Model) renderComposer(width int) string {
	return style.InputBorder.Width(max(1, width-2)).Padding(0, 1).Render(m.input.View())
}

func joinNonEmpty(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, "\n")
}
