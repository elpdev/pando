package chat

import (
	"fmt"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) loadContacts(initialMailbox string) {
	contacts, err := m.messaging.Contacts()
	if err != nil {
		m.pushToast(fmt.Sprintf("load contacts failed: %v", err), ToastBad)
		return
	}
	m.contacts = make([]contactItem, 0, len(contacts))
	for _, contact := range contacts {
		m.contacts = append(m.contacts, contactItem{
			Mailbox:     contact.AccountID,
			Fingerprint: contact.Fingerprint(),
			Verified:    contact.Verified,
			TrustSource: contact.TrustSource,
		})
	}
	m.selectedIndex = -1
	for idx := range m.contacts {
		if m.contacts[idx].Mailbox == initialMailbox {
			m.selectedIndex = idx
			return
		}
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
	m.recipientMailbox = m.contacts[m.selectedIndex].Mailbox
	m.clearUnread(m.recipientMailbox)
	m.syncRecipientDetails()
	m.clearPeerTyping()
	m.loadHistory()
	m.syncViewportToBottom()
	m.syncInputPlaceholder()
	m.focus = focusChat
	m.input.Focus()
	return true
}

func (m *Model) markUnread(peer string) {
	if peer == "" || peer == m.recipientMailbox {
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
	m.messageItems = nil
	m.messages = nil
	if m.recipientMailbox == "" {
		m.syncViewport()
		return
	}
	records, err := m.messaging.History(m.recipientMailbox)
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
		m.messageItems = append(m.messageItems, item)
	}
	if len(m.messageItems) == 0 {
		m.viewport.SetContent(style.Muted.Render("No messages yet."))
		return
	}
	m.renderMessages()
	m.syncViewportToBottom()
}

func (m *Model) syncRecipientDetails() {
	m.peerFingerprint = "unknown"
	m.peerVerified = false
	m.peerTrustSource = identity.TrustSourceUnverified
	if m.recipientMailbox == "" {
		return
	}
	contact := m.findContact(m.recipientMailbox)
	if contact == nil {
		if stored, err := m.messaging.Contact(m.recipientMailbox); err == nil {
			m.peerFingerprint = stored.Fingerprint()
			m.peerVerified = stored.Verified
			m.peerTrustSource = stored.TrustSource
		}
		return
	}
	m.peerFingerprint = contact.Fingerprint
	m.peerVerified = contact.Verified
	m.peerTrustSource = contact.TrustSource
}
