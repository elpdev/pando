package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) handleProtocolMessage(msg protocol.Message) {
	switch msg.Type {
	case protocol.MessageTypeAck:
		m.handleIncomingAck()
	case protocol.MessageTypeIncoming:
		if msg.Incoming == nil {
			return
		}
		result, err := m.messaging.HandleIncoming(*msg.Incoming)
		if err != nil {
			m.pushToast(fmt.Sprintf("incoming message failed: %v", err), ToastBad)
			return
		}
		if result == nil || result.Duplicate {
			return
		}
		for _, envelope := range result.AckEnvelopes {
			if err := m.client.Send(envelope); err != nil {
				m.pushToast(fmt.Sprintf("delivery ack failed: %v", err), ToastBad)
				break
			}
		}
		if result.ContactUpdated != nil {
			m.upsertContact(result.ContactUpdated)
			if result.ContactUpdated.AccountID == m.peer.mailbox {
				m.syncRecipientDetails()
				if text := contactUpdateToast(result); text != "" {
					m.pushToast(text, ToastInfo)
				}
			}
			return
		}
		if result.Control {
			m.handleIncomingControl(result)
			return
		}
		m.handleIncomingChat(result, *msg.Incoming)
	case protocol.MessageTypeError:
		m.handleIncomingError(msg.Error)
	}
}

func contactUpdateToast(result *messaging.IncomingResult) string {
	if result == nil || result.ContactUpdated == nil {
		return ""
	}
	switch result.ContactChange {
	case messaging.ContactUpdateDeviceAdded:
		return fmt.Sprintf("new device added for %s", result.ContactUpdated.AccountID)
	case messaging.ContactUpdateDeviceRevoked:
		return fmt.Sprintf("device revoked for %s", result.ContactUpdated.AccountID)
	case messaging.ContactUpdateDeviceRotated:
		return fmt.Sprintf("device keys rotated for %s", result.ContactUpdated.AccountID)
	case messaging.ContactUpdateDeviceChanged:
		return fmt.Sprintf("device list changed for %s", result.ContactUpdated.AccountID)
	default:
		return ""
	}
}

func (m *Model) handleIncomingAck() {
	if m.conn.connecting {
		m.markConnected(fmt.Sprintf("connected as %s", m.mailbox))
	}
}

func (m *Model) handleIncomingChat(result *messaging.IncomingResult, envelope protocol.Envelope) {
	if result.RoomID != "" {
		if err := m.messaging.SaveDefaultRoomReceived(result.PeerAccountID, envelope.SenderMailbox, result.MessageID, result.Body, envelope.Timestamp); err != nil {
			m.pushToast(fmt.Sprintf("save room history failed: %v", err), ToastBad)
			return
		}
		if m.peer.isRoom && m.peer.mailbox == result.RoomID {
			m.appendMessageItem(messageItem{direction: "inbound", sender: result.PeerAccountID, body: result.Body, timestamp: envelope.Timestamp, messageID: result.MessageID})
			m.syncViewport()
			return
		}
		m.markUnread(result.RoomID)
		m.pushToast(fmt.Sprintf("new message in %s", messaging.DefaultRoomLabel()), ToastInfo)
		return
	}
	if err := m.messaging.SaveReceived(result.PeerAccountID, result.Body, envelope.Timestamp); err != nil {
		m.pushToast(fmt.Sprintf("save history failed: %v", err), ToastBad)
		return
	}
	if result.PeerAccountID == m.peer.mailbox {
		m.clearPeerTyping()
		m.appendMessageItem(messageItem{
			direction:    "inbound",
			sender:       envelope.SenderMailbox,
			body:         result.Body,
			timestamp:    envelope.Timestamp,
			messageID:    result.MessageID,
			isAttachment: attachmentBodyPattern(result.Body),
		})
		m.syncViewport()
		return
	}
	m.markUnread(result.PeerAccountID)
	m.pushToast(fmt.Sprintf("new message from %s", result.PeerAccountID), ToastInfo)
}

func (m *Model) handleIncomingControl(result *messaging.IncomingResult) {
	if result.RoomSync != nil {
		if m.roomSync.requestID != "" && result.RoomSync.RequestID != m.roomSync.requestID {
			return
		}
		m.roomSync.syncedCount += result.RoomSync.Added
		if m.peer.isRoom && m.peer.mailbox == result.RoomID && result.RoomSync.Added > 0 {
			m.loadHistory()
		}
		if result.RoomSync.Complete {
			count := m.roomSync.syncedCount
			m.clearRoomSync()
			if count == 0 {
				m.pushToast("no recent room history available", ToastInfo)
			} else {
				m.pushToast(fmt.Sprintf("synced %d room messages", count), ToastInfo)
			}
		}
		return
	}
	if result.RoomUpdated != nil {
		m.syncRoomContact(result.RoomUpdated)
		if m.peer.isRoom && m.peer.mailbox == result.RoomUpdated.ID {
			m.loadHistory()
		}
		return
	}
	if result.TypingState != "" {
		if result.PeerAccountID != m.peer.mailbox {
			return
		}
		if result.TypingState == messaging.TypingStateActive {
			m.typing.peerVisible = true
			m.typing.peerExpiresAt = result.TypingExpiresAt
			m.typing.spinner = newTypingSpinner()
			return
		}
		m.clearPeerTyping()
		return
	}
	if result.MessageID == "" || result.PeerAccountID != m.peer.mailbox {
		return
	}
	if !m.updateMessageStatus(result.MessageID, statusDelivered) {
		m.loadHistory()
	}
	m.syncViewport()
}

func (m *Model) handleIncomingError(msg *protocol.Error) {
	if msg != nil {
		m.pushToast(fmt.Sprintf("relay error: %s", msg.Message), ToastBad)
	}
}

func (m *Model) appendMessageItem(item messageItem) {
	wasAtBottom := m.viewport.AtBottom()
	m.msgs.items = append(m.msgs.items, item)
	m.renderMessages()
	if item.direction == "inbound" && !wasAtBottom {
		m.msgs.pendingIncoming++
	}
}

func (m *Model) renderMessages() {
	const groupGap = 5 * time.Minute
	m.msgs.rendered = m.msgs.rendered[:0]

	var prevSender string
	var prevTS time.Time
	for i, item := range m.msgs.items {
		startGroup := i == 0 || item.sender != prevSender || item.timestamp.Sub(prevTS) > groupGap
		if startGroup {
			if i > 0 {
				m.msgs.rendered = append(m.msgs.rendered, "")
			}
			m.msgs.rendered = append(m.msgs.rendered, m.renderGroupHeader(item))
		}
		m.msgs.rendered = append(m.msgs.rendered, m.renderMessageBody(item))
		prevSender = item.sender
		prevTS = item.timestamp
	}
}

func (m *Model) renderGroupHeader(item messageItem) string {
	name := item.sender
	var nameStyled string
	if item.direction == "outbound" {
		nameStyled = style.Bold.Render("you")
	} else {
		if m.peer.isRoom {
			nameStyled = style.Bright.Render(name)
		} else {
			nameStyled = style.PeerAccentStyle(m.peer.fingerprint).Bold(true).Render(name)
		}
	}
	ts := ""
	if !item.timestamp.IsZero() {
		ts = item.timestamp.Local().Format(time.Kitchen)
	}
	suffix := style.Muted.Render(" " + style.GroupSep + " " + ts)
	return nameStyled + suffix
}

func (m *Model) renderMessageBody(item messageItem) string {
	bodyStyle := lipgloss.NewStyle()
	if item.isAttachment {
		bodyStyle = style.Italic
	}
	body := "  " + bodyStyle.Render(item.body)

	if item.direction != "outbound" {
		return body
	}
	glyph, glyphStyle := deliveryGlyphFor(item.status)
	if glyph == "" {
		return body
	}
	tick := " " + glyphStyle.Render(glyph)
	width := m.viewport.Width
	if width <= 0 {
		return body + tick
	}
	pad := width - lipgloss.Width(body) - lipgloss.Width(tick)
	if pad < 1 {
		pad = 1
	}
	return body + strings.Repeat(" ", pad) + tick
}

func batchMessageID(batch *messaging.OutgoingBatch) string {
	if batch == nil {
		return ""
	}
	return batch.MessageID
}

func (m *Model) updateMessageStatus(messageID string, status deliveryStatus) bool {
	if messageID == "" {
		return false
	}
	for i := range m.msgs.items {
		if m.msgs.items[i].direction != "outbound" || m.msgs.items[i].messageID != messageID {
			continue
		}
		if m.msgs.items[i].status == status {
			return true
		}
		m.msgs.items[i].status = status
		m.renderMessages()
		return true
	}
	return false
}

func deliveryGlyphFor(s deliveryStatus) (string, lipgloss.Style) {
	switch s {
	case statusPending:
		return style.GlyphDeliveryPending, style.DeliveryPending
	case statusSent:
		return style.GlyphDeliverySent, style.DeliverySent
	case statusDelivered:
		return style.GlyphDeliveryDelivered, style.DeliveryDelivered
	case statusFailed:
		return style.GlyphDeliveryFailed, style.DeliveryFailed
	}
	return "", lipgloss.NewStyle()
}

func attachmentBodyPattern(body string) bool {
	for _, prefix := range []string{"photo sent:", "voice note sent:", "file sent:"} {
		if strings.HasPrefix(body, prefix) {
			return true
		}
	}
	return false
}
