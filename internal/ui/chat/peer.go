package chat

import (
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/store"
)

func (m *Model) PeerFingerprint() string {
	return m.peer.fingerprint
}

func (m *Model) PeerVerified() bool {
	return m.peer.verified
}

func (m *Model) PeerLabel() string {
	if m.peer.isRoom && m.peer.label != "" {
		return m.peer.label
	}
	if m.peer.mailbox != "" {
		return m.peer.mailbox
	}
	return ""
}

// toggleFocus flips which pane owns keyboard input. In wide mode this mostly
// affects the border color; in narrow mode it switches which pane is rendered.
func (m *Model) toggleFocus() {
	if m.ui.focus == focusChat {
		m.ui.focus = focusSidebar
		m.input.Blur()
	} else {
		m.ui.focus = focusChat
		m.input.Focus()
	}
}

// jumpToLatest scrolls the viewport all the way down and clears the pending
// incoming-message counter that feeds the "↓ N new" pill.
func (m *Model) jumpToLatest() {
	m.msgs.followLatest = true
	m.viewport.GotoBottom()
	m.msgs.pendingIncoming = 0
}

func (m *Model) upsertContact(contact *identity.Contact) {
	if contact == nil {
		return
	}
	for idx := range m.contacts {
		if m.contacts[idx].Mailbox != contact.AccountID {
			continue
		}
		m.contacts[idx].Fingerprint = contact.Fingerprint()
		m.contacts[idx].Verified = contact.Verified
		m.contacts[idx].TrustSource = contact.TrustSource
		m.contacts[idx].Label = contact.AccountID
		return
	}
	m.contacts = append(m.contacts, contactItem{Mailbox: contact.AccountID, Label: contact.AccountID, Fingerprint: contact.Fingerprint(), Verified: contact.Verified, TrustSource: contact.TrustSource})
	if m.selectedIndex == -1 {
		m.selectedIndex = len(m.contacts) - 1
	}
}

func (m *Model) syncRoomContact(state *store.RoomState) {
	if state == nil {
		return
	}
	for idx := range m.contacts {
		if !m.contacts[idx].IsRoom {
			continue
		}
		m.contacts[idx].Joined = state.Joined
		m.contacts[idx].MemberCount = len(state.Members)
		m.contacts[idx].Label = messaging.DefaultRoomLabel()
		if m.peer.isRoom && m.peer.mailbox == state.ID {
			m.peer.label = messaging.DefaultRoomLabel()
			m.peer.joined = state.Joined
			m.peer.memberCount = len(state.Members)
			m.syncInputPlaceholder()
		}
		return
	}
}

func (m *Model) findContact(mailbox string) *contactItem {
	for idx := range m.contacts {
		if m.contacts[idx].Mailbox == mailbox {
			return &m.contacts[idx]
		}
	}
	return nil
}
