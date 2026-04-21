package chat

import (
	"fmt"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) loadContacts(initialMailbox string) {
	room, err := m.messaging.DefaultRoomState()
	if err != nil {
		m.pushToast(fmt.Sprintf("load room failed: %v", err), ToastBad)
	}
	contacts, err := m.messaging.Contacts()
	if err != nil {
		m.pushToast(fmt.Sprintf("load contacts failed: %v", err), ToastBad)
		return
	}
	m.contacts = make([]contactItem, 0, len(contacts)+1)
	for _, contact := range contacts {
		m.contacts = append(m.contacts, contactItem{
			Mailbox:     contact.AccountID,
			Label:       contact.AccountID,
			Fingerprint: contact.Fingerprint(),
			Verified:    contact.Verified,
			TrustSource: contact.TrustSource,
		})
	}
	if room != nil {
		m.contacts = append(m.contacts, contactItem{
			Mailbox:     messaging.DefaultRoomID,
			Label:       messaging.DefaultRoomLabel(),
			IsRoom:      true,
			Joined:      room.Joined,
			MemberCount: len(room.Members),
		})
	}
	m.selectedIndex = -1
	for idx := range m.contacts {
		if m.contacts[idx].Mailbox == initialMailbox {
			m.selectedIndex = idx
			return
		}
	}
	for idx := range m.contacts {
		if m.contacts[idx].IsRoom {
			continue
		}
		m.selectedIndex = idx
		return
	}
	if len(m.contacts) != 0 {
		m.selectedIndex = 0
	}
}

func (m *Model) moveSelection(delta int) {
	if len(m.contacts) == 0 {
		return
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
		return
	}
	m.selectedIndex += delta
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
	if m.selectedIndex >= len(m.contacts) {
		m.selectedIndex = len(m.contacts) - 1
	}
}

func (m *Model) selectContact(mailbox string) {
	for idx := range m.contacts {
		if m.contacts[idx].Mailbox == mailbox {
			m.selectedIndex = idx
			return
		}
	}
}

func (m *Model) activateSelectedContact() bool {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.contacts) {
		return false
	}
	selected := m.contacts[m.selectedIndex]
	m.peer.mailbox = selected.Mailbox
	m.peer.label = selected.Label
	m.peer.isRoom = selected.IsRoom
	m.peer.joined = selected.Joined
	m.peer.memberCount = selected.MemberCount
	m.clearUnread(m.peer.mailbox)
	m.syncRecipientDetails()
	m.clearPeerTyping()
	m.loadHistory()
	m.syncViewportToBottom()
	m.syncInputPlaceholder()
	m.ui.focus = focusChat
	m.input.Focus()
	return true
}

func (m *Model) markUnread(peer string) {
	if peer == "" || peer == m.peer.mailbox {
		return
	}
	if m.unread == nil {
		m.unread = map[string]int{}
	}
	m.unread[peer]++
}

func (m *Model) clearUnread(peer string) {
	if m.unread == nil {
		return
	}
	delete(m.unread, peer)
}

func (m *Model) Unread(peer string) int {
	if m.unread == nil {
		return 0
	}
	return m.unread[peer]
}

func (m *Model) loadHistory() {
	m.msgs.items = nil
	m.msgs.rendered = nil
	if m.peer.mailbox == "" {
		m.syncViewport()
		return
	}
	if m.peer.isRoom {
		records, err := m.messaging.DefaultRoomHistory()
		if err != nil {
			m.pushToast(fmt.Sprintf("load room history failed: %v", err), ToastBad)
			return
		}
		for _, record := range records {
			item := messageItem{
				direction: "inbound",
				sender:    record.SenderAccountID,
				body:      record.Body,
				timestamp: record.Timestamp,
				messageID: record.MessageID,
			}
			if record.SenderAccountID == m.messaging.Identity().AccountID {
				item.direction = "outbound"
				item.sender = m.messaging.Identity().AccountID
				item.status = statusSent
			}
			m.msgs.items = append(m.msgs.items, item)
		}
		if len(m.msgs.items) == 0 {
			if !m.peer.joined {
				m.viewport.SetContent(style.Muted.Render("Press enter to join #general."))
				return
			}
			m.viewport.SetContent(style.Muted.Render("No room messages yet."))
			return
		}
		m.renderMessages()
		m.syncViewportToBottom()
		return
	}
	records, err := m.messaging.History(m.peer.mailbox)
	if err != nil {
		m.pushToast(fmt.Sprintf("load history failed: %v", err), ToastBad)
		return
	}
	for _, record := range records {
		item := messageItem{
			direction:    record.Direction,
			body:         record.Body,
			timestamp:    record.Timestamp,
			messageID:    record.MessageID,
			isAttachment: attachmentBodyPattern(record.Body),
		}
		if record.Direction == "outbound" {
			item.sender = m.mailbox
			item.status = statusSent
			if record.Delivered {
				item.status = statusDelivered
			}
		} else {
			item.sender = record.PeerMailbox
		}
		m.msgs.items = append(m.msgs.items, item)
	}
	if len(m.msgs.items) == 0 {
		m.viewport.SetContent(style.Muted.Render("No messages yet."))
		return
	}
	m.renderMessages()
	m.syncViewportToBottom()
}

func (m *Model) syncRecipientDetails() {
	if m.peer.isRoom {
		m.peer.fingerprint = messaging.DefaultRoomLabel()
		m.peer.verified = true
		m.peer.trustSource = identity.TrustSourceRelayDirectory
		if room, err := m.messaging.DefaultRoomState(); err == nil {
			m.peer.joined = room.Joined
			m.peer.memberCount = len(room.Members)
		}
		return
	}
	m.peer.fingerprint = "unknown"
	m.peer.verified = false
	m.peer.trustSource = identity.TrustSourceUnverified
	if m.peer.mailbox == "" {
		return
	}
	contact := m.findContact(m.peer.mailbox)
	if contact == nil {
		if stored, err := m.messaging.Contact(m.peer.mailbox); err == nil {
			m.peer.fingerprint = stored.Fingerprint()
			m.peer.verified = stored.Verified
			m.peer.trustSource = stored.TrustSource
		}
		return
	}
	m.peer.fingerprint = contact.Fingerprint
	m.peer.verified = contact.Verified
	m.peer.trustSource = contact.TrustSource
}
