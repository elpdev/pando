package chat

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) sendRoomHistorySyncCmd() tea.Cmd {
	if !m.peer.isRoom || !m.peer.joined || m.roomSync.active {
		return nil
	}
	batch, requestID, err := m.messaging.RequestDefaultRoomHistory()
	if err != nil {
		return func() tea.Msg { return roomHistorySyncResultMsg{err: err} }
	}
	if batch == nil || len(batch.Envelopes) == 0 {
		return func() tea.Msg { return roomHistorySyncResultMsg{skipped: "no room members available for history sync"} }
	}
	m.roomSync.active = true
	m.roomSync.requestID = requestID
	m.roomSync.startedAt = time.Now().UTC()
	m.roomSync.lastRequestedAt = m.roomSync.startedAt
	m.roomSync.syncedCount = 0
	return func() tea.Msg {
		for _, envelope := range batch.Envelopes {
			if err := m.client.Send(envelope); err != nil {
				return roomHistorySyncResultMsg{requestID: requestID, err: err}
			}
		}
		return roomHistorySyncResultMsg{requestID: requestID}
	}
}

func (m *Model) clearRoomSync() {
	m.roomSync.active = false
	m.roomSync.requestID = ""
	m.roomSync.startedAt = time.Time{}
	m.roomSync.syncedCount = 0
}
