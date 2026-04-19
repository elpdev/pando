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
	m.viewport.SetContent(strings.Join(m.messages, "\n"))
	if wasAtBottom {
		m.viewport.GotoBottom()
		m.pendingIncoming = 0
	}
}

func (m *Model) syncViewportToBottom() {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		return
	}
	m.viewport.SetContent(strings.Join(m.messages, "\n"))
	m.viewport.GotoBottom()
	m.pendingIncoming = 0
}

func (m *Model) syncInputPlaceholder() {
	if m.authFailed {
		m.input.Placeholder = "Relay auth failed. Restart with --relay-token"
		return
	}
	if m.recipientMailbox == "" {
		m.input.Placeholder = "Select a contact to start chatting"
		return
	}
	m.input.Placeholder = fmt.Sprintf("Message %s", m.recipientMailbox)
}

func (m *Model) updateLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	if m.width < narrowThreshold {
		m.sidebarWidth = m.width
		m.viewport.Width = max(1, m.width)
		m.viewport.Height = max(1, m.height-5)
		return
	}
	sidebarWidth := 28
	if m.width < 80 {
		sidebarWidth = max(20, m.width/3)
	}
	if sidebarWidth > m.width-20 {
		sidebarWidth = max(18, m.width/2)
	}
	m.sidebarWidth = sidebarWidth
	conversationWidth := max(1, m.width-m.sidebarWidth-1)
	m.viewport.Width = conversationWidth
	m.viewport.Height = max(1, m.height-5)
}

func (m *Model) conversationWidth() int {
	if m.width < narrowThreshold {
		return max(1, m.width)
	}
	return max(1, m.width-m.sidebarWidth-1)
}

func (m *Model) renderJumpPill(width int) string {
	if m.pendingIncoming <= 0 {
		return ""
	}
	pill := style.StatusInfo.Bold(true).Render(fmt.Sprintf("%s %d new  end", style.GlyphJumpToLatest, m.pendingIncoming))
	pad := width - lipgloss.Width(pill)
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + pill
}
