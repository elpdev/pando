package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) syncViewport() {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		return
	}
	wasAtBottom := m.viewport.AtBottom()
	m.viewport.SetContent(strings.Join(m.msgs.rendered, "\n"))
	if wasAtBottom {
		m.viewport.GotoBottom()
		m.msgs.pendingIncoming = 0
	}
}

func (m *Model) syncViewportToBottom() {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		return
	}
	m.viewport.SetContent(strings.Join(m.msgs.rendered, "\n"))
	m.viewport.GotoBottom()
	m.msgs.pendingIncoming = 0
}

func (m *Model) syncInputPlaceholder() {
	if m.conn.authFailed {
		m.input.Placeholder = "Relay auth failed. Restart with --relay-token"
		return
	}
	if m.peer.mailbox == "" {
		m.input.Placeholder = "Select a contact to start chatting"
		return
	}
	if m.peer.isRoom {
		if !m.peer.joined {
			m.input.Placeholder = "Press enter to join #general"
			return
		}
		m.input.Placeholder = "Message #general"
		return
	}
	m.input.Placeholder = fmt.Sprintf("Message %s", m.peer.mailbox)
}

func (m *Model) updateLayout() {
	if m.ui.width <= 0 || m.ui.height <= 0 {
		return
	}
	if m.ui.width < narrowThreshold {
		m.ui.sidebarWidth = m.ui.width
		m.viewport.Width = max(1, m.ui.width)
		m.viewport.Height = max(1, m.ui.height-5)
		m.filePicker.SetSize(m.conversationWidth(), m.ui.height)
		return
	}
	sidebarWidth := 28
	if m.ui.width < 80 {
		sidebarWidth = max(20, m.ui.width/3)
	}
	if sidebarWidth > m.ui.width-20 {
		sidebarWidth = max(18, m.ui.width/2)
	}
	m.ui.sidebarWidth = sidebarWidth
	conversationWidth := max(1, m.ui.width-m.ui.sidebarWidth-1)
	m.viewport.Width = conversationWidth
	m.viewport.Height = max(1, m.ui.height-5)
	m.filePicker.SetSize(conversationWidth, m.ui.height)
}

func (m *Model) conversationWidth() int {
	if m.ui.width < narrowThreshold {
		return max(1, m.ui.width)
	}
	return max(1, m.ui.width-m.ui.sidebarWidth-1)
}

func (m *Model) renderJumpPill(width int) string {
	if m.msgs.pendingIncoming <= 0 {
		return ""
	}
	pill := style.StatusInfo.Bold(true).Render(fmt.Sprintf("%s %d new  end", style.GlyphJumpToLatest, m.msgs.pendingIncoming))
	pad := width - lipgloss.Width(pill)
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + pill
}

func (m *Model) renderToast() string {
	if m.ui.toast == nil {
		return ""
	}
	switch m.ui.toast.level {
	case ToastWarn:
		return style.StatusWarn.Render(m.ui.toast.text)
	case ToastBad:
		return style.StatusBad.Render(m.ui.toast.text)
	default:
		return style.Muted.Render(m.ui.toast.text)
	}
}
