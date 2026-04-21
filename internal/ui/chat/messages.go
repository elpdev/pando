package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/ui/media"
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
			m.appendMessageItem(messageItem{kind: transcriptMessage, direction: "inbound", sender: result.PeerAccountID, body: result.Body, timestamp: envelope.Timestamp, messageID: result.MessageID})
			m.syncViewport()
			return
		}
		m.markUnread(result.RoomID)
		m.pushToast(fmt.Sprintf("new message in %s", messaging.DefaultRoomLabel()), ToastInfo)
		return
	}
	if err := m.messaging.SaveReceived(result.PeerAccountID, result.Body, envelope.Timestamp, result.Attachment); err != nil {
		m.pushToast(fmt.Sprintf("save history failed: %v", err), ToastBad)
		return
	}
	if result.PeerAccountID == m.peer.mailbox {
		m.clearPeerTyping()
		m.appendMessageItem(messageItem{
			kind:       transcriptMessage,
			direction:  "inbound",
			sender:     envelope.SenderMailbox,
			body:       result.Body,
			timestamp:  envelope.Timestamp,
			messageID:  result.MessageID,
			attachment: result.Attachment,
		})
		m.syncViewport()
		return
	}
	m.markUnread(result.PeerAccountID)
	m.pushToast(fmt.Sprintf("new message from %s", result.PeerAccountID), ToastInfo)
}

func (m *Model) handleIncomingControl(result *messaging.IncomingResult) {
	if result.ContactRequest != nil {
		m.handleContactRequestUpdate(result.ContactRequest)
		return
	}
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
			m.appendEventItem(time.Now().UTC(), fmt.Sprintf("room history synced: %d messages", count), "sync")
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
	item.kind = transcriptMessage
	if !m.viewport.AtBottom() {
		m.msgs.followLatest = false
	}
	m.msgs.items = append(m.msgs.items, item)
	m.renderMessages()
	if item.direction == "inbound" && !m.msgs.followLatest {
		m.msgs.pendingIncoming++
	}
}

func (m *Model) appendEventItem(ts time.Time, body, meta string) {
	if body == "" {
		return
	}
	m.msgs.items = append(m.msgs.items, messageItem{kind: transcriptEvent, body: body, timestamp: ts, meta: meta})
	m.renderMessages()
}

func (m *Model) renderMessages() {
	const groupGap = 5 * time.Minute
	m.msgs.rendered = m.msgs.rendered[:0]

	var prevSender string
	var prevTS time.Time
	var prevDay string
	for i := range m.msgs.items {
		item := &m.msgs.items[i]
		day := item.timestamp.Local().Format("2006-01-02")
		if !item.timestamp.IsZero() && day != prevDay {
			if len(m.msgs.rendered) > 0 {
				m.msgs.rendered = append(m.msgs.rendered, "")
			}
			m.msgs.rendered = append(m.msgs.rendered, m.renderDaySeparator(item.timestamp))
			prevDay = day
		}
		if item.kind == transcriptEvent {
			m.msgs.rendered = append(m.msgs.rendered, m.renderEventBody(*item))
			prevSender = ""
			prevTS = time.Time{}
			continue
		}
		startGroup := i == 0 || item.sender != prevSender || item.timestamp.Sub(prevTS) > groupGap
		if startGroup {
			if i > 0 {
				m.msgs.rendered = append(m.msgs.rendered, "")
			}
			m.msgs.rendered = append(m.msgs.rendered, m.renderGroupHeader(*item))
		}
		m.msgs.rendered = append(m.msgs.rendered, m.renderMessageBody(item))
		prevSender = item.sender
		prevTS = item.timestamp
	}
}

func (m *Model) renderDaySeparator(ts time.Time) string {
	label := style.Subtle.Render(ts.Local().Format("Mon Jan 2"))
	left := strings.Repeat("─", 2)
	return style.Faint.Render(left) + " " + label + " " + style.Faint.Render(left)
}

func (m *Model) renderEventBody(item messageItem) string {
	meta := "event"
	if item.meta != "" {
		meta = item.meta
	}
	stamp := ""
	if !item.timestamp.IsZero() {
		stamp = item.timestamp.Local().Format(time.Kitchen)
	}
	prefix := style.Subtle.Render(strings.ToUpper(meta))
	if stamp != "" {
		prefix += style.Muted.Render(" " + style.GroupSep + " " + stamp)
	}
	return prefix + "\n  " + style.Muted.Render(item.body)
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

func (m *Model) renderMessageBody(item *messageItem) string {
	bodyStyle := lipgloss.NewStyle()
	if item.attachment != nil {
		bodyStyle = style.Italic
	}
	body := "  " + bodyStyle.Render(item.body)
	if preview := m.renderAttachmentPreview(item); preview != "" {
		body = preview + "\n" + body
	}

	if item.direction != "outbound" {
		return body
	}
	glyph, glyphStyle := deliveryGlyphFor(item.status)
	if glyph == "" {
		return body
	}
	if strings.Contains(body, "\n") {
		return body + "\n  " + glyphStyle.Render(glyph)
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

func (m *Model) renderAttachmentPreview(item *messageItem) string {
	if !isPhotoAttachment(item.attachment) || m.viewport.Width <= 0 {
		return ""
	}
	imageWidth := min(max(12, m.viewport.Width-6), 48)
	if item.imageRendered != "" && item.imageWidth == imageWidth {
		return indentBlock(item.imageRendered, "  ")
	}
	block, _, err := media.RenderFile(item.attachment.LocalPath, imageWidth)
	if err != nil || block == "" {
		item.imageRendered = ""
		item.imageWidth = imageWidth
		return ""
	}
	item.imageRendered = block
	item.imageWidth = imageWidth
	return indentBlock(block, "  ")
}

func indentBlock(block, prefix string) string {
	lines := strings.Split(block, "\n")
	for i := range lines {
		if lines[i] == "" {
			lines[i] = prefix
			continue
		}
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
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
