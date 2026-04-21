package chat

import "strings"

func (m *Model) rememberDraft(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		m.drafts.index = -1
		m.drafts.saved = ""
		return
	}
	if n := len(m.drafts.history); n > 0 && m.drafts.history[n-1] == value {
		m.drafts.index = -1
		m.drafts.saved = ""
		return
	}
	m.drafts.history = append(m.drafts.history, value)
	if len(m.drafts.history) > 50 {
		m.drafts.history = append([]string(nil), m.drafts.history[len(m.drafts.history)-50:]...)
	}
	m.drafts.index = -1
	m.drafts.saved = ""
}

func (m *Model) browseDraftHistory(delta int) bool {
	if len(m.drafts.history) == 0 {
		return false
	}
	current := m.input.Value()
	if delta < 0 {
		if m.drafts.index == -1 {
			m.drafts.saved = current
			m.drafts.index = len(m.drafts.history) - 1
		} else if m.drafts.index > 0 {
			m.drafts.index--
		} else {
			return false
		}
	} else {
		if m.drafts.index == -1 {
			return false
		}
		if m.drafts.index < len(m.drafts.history)-1 {
			m.drafts.index++
		} else {
			m.drafts.index = -1
			m.input.SetValue(m.drafts.saved)
			m.syncComposer()
			return true
		}
	}
	m.input.SetValue(m.drafts.history[m.drafts.index])
	m.syncComposer()
	return true
}
