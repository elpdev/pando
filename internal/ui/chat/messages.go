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
		if m.connecting {
			m.markConnected(fmt.Sprintf("connected as %s", m.mailbox))
		}
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
		if len(result.AckEnvelopes) != 0 {
			for _, envelope := range result.AckEnvelopes {
				if err := m.client.Send(envelope); err != nil {
					m.pushToast(fmt.Sprintf("delivery ack failed: %v", err), ToastBad)
					break
				}
			}
		}
		if result.ContactUpdated != nil {
			m.upsertContact(result.ContactUpdated)
			if result.ContactUpdated.AccountID == m.recipientMailbox {
				m.syncRecipientDetails()
				m.pushToast(fmt.Sprintf("updated device bundle for %s", result.ContactUpdated.AccountID), ToastInfo)
			}
			return
		}
		if result.Control {
			if result.TypingState != "" {
				if result.PeerAccountID == m.recipientMailbox {
					if result.TypingState == messaging.TypingStateActive {
						m.typing.peerVisible = true
						m.typing.peerExpiresAt = result.TypingExpiresAt
						m.typing.spinner = newTypingSpinner()
					} else {
						m.clearPeerTyping()
					}
				}
				return
			}
			if result.MessageID != "" {
				if result.PeerAccountID == m.recipientMailbox {
					if !m.updateMessageStatus(result.MessageID, statusDelivered) {
						m.loadHistory()
					}
					m.syncViewport()
				}
			}
			return
		}
		if err := m.messaging.SaveReceived(result.PeerAccountID, result.Body, msg.Incoming.Timestamp); err != nil {
			m.pushToast(fmt.Sprintf("save history failed: %v", err), ToastBad)
			return
		}
		if result.PeerAccountID == m.recipientMailbox {
			m.clearPeerTyping()
			m.appendMessageItem(messageItem{
				direction:    "inbound",
				sender:       msg.Incoming.SenderMailbox,
				body:         result.Body,
				timestamp:    msg.Incoming.Timestamp,
				messageID:    result.MessageID,
				isAttachment: attachmentBodyPattern(result.Body),
			})
			m.syncViewport()
			return
		}
		m.markUnread(result.PeerAccountID)
		m.pushToast(fmt.Sprintf("new message from %s", result.PeerAccountID), ToastInfo)
	case protocol.MessageTypeError:
		if msg.Error != nil {
			m.pushToast(fmt.Sprintf("relay error: %s", msg.Error.Message), ToastBad)
		}
	}
}

func (m *Model) appendMessageItem(item messageItem) {
	wasAtBottom := m.viewport.AtBottom()
	m.messageItems = append(m.messageItems, item)
	m.renderMessages()
	if item.direction == "inbound" && !wasAtBottom {
		m.pendingIncoming++
	}
}

func (m *Model) renderMessages() {
	const groupGap = 5 * time.Minute
	m.messages = m.messages[:0]

	var prevSender string
	var prevTS time.Time
	for i, item := range m.messageItems {
		startGroup := i == 0 || item.sender != prevSender || item.timestamp.Sub(prevTS) > groupGap
		if startGroup {
			if i > 0 {
				m.messages = append(m.messages, "")
			}
			m.messages = append(m.messages, m.renderGroupHeader(item))
		}
		m.messages = append(m.messages, m.renderMessageBody(item))
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
		nameStyled = style.PeerAccentStyle(m.peerFingerprint).Bold(true).Render(name)
	}
	ts := ""
	if !item.timestamp.IsZero() {
		ts = item.timestamp.Format(time.Kitchen)
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
	for i := range m.messageItems {
		if m.messageItems[i].direction != "outbound" || m.messageItems[i].messageID != messageID {
			continue
		}
		if m.messageItems[i].status == status {
			return true
		}
		m.messageItems[i].status = status
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
