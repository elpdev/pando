package chat

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/messaging"
)

func parseAttachmentPath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) >= 2 {
		switch {
		case path[0] == '"' && path[len(path)-1] == '"':
			if unquoted, err := strconv.Unquote(path); err == nil {
				path = unquoted
			} else {
				path = path[1 : len(path)-1]
			}
		case path[0] == '\'' && path[len(path)-1] == '\'':
			path = path[1 : len(path)-1]
		}
	}
	return strings.ReplaceAll(path, `\ `, " ")
}

func (m *Model) handleAttachmentCommand(prefix, body string, prepare func(string, string) (*messaging.OutgoingBatch, string, error)) (*Model, tea.Cmd) {
	path := parseAttachmentPath(strings.TrimSpace(strings.TrimPrefix(body, prefix)))
	if path == "" {
		m.pushToast(fmt.Sprintf("usage: %s <path>", prefix), ToastWarn)
		return m, nil
	}
	if err := m.queuePendingAttachment(path, prefix); err != nil {
		m.pushToast(err.Error(), ToastBad)
		return m, nil
	}
	m.input.SetValue("")
	m.syncComposer()
	return m, nil
}

func (m *Model) syncComposer() {
	width := m.conversationWidth()
	if width <= 0 {
		return
	}
	innerWidth := max(8, width-4)
	m.input.SetWidth(innerWidth)
	rows := composerRowsForValue(m.input.Value(), innerWidth-lipgloss.Width(m.input.Prompt))
	m.ui.composerRows = rows
	m.input.SetHeight(rows)
}

func composerRowsForValue(value string, width int) int {
	if width <= 0 {
		return 1
	}
	lines := strings.Split(value, "\n")
	rows := 0
	for _, line := range lines {
		lineWidth := lipgloss.Width(line)
		if lineWidth == 0 {
			rows++
			continue
		}
		rows += (lineWidth-1)/width + 1
	}
	if rows < 1 {
		rows = 1
	}
	return min(6, rows)
}
