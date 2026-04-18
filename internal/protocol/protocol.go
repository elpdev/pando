package protocol

import (
	"errors"
	"strings"
	"time"
)

const (
	MessageTypeSubscribe = "subscribe"
	MessageTypePublish   = "publish"
	MessageTypeIncoming  = "incoming"
	MessageTypeAck       = "ack"
	MessageTypeError     = "error"
)

type Envelope struct {
	ID               string    `json:"id"`
	SenderMailbox    string    `json:"sender_mailbox"`
	RecipientMailbox string    `json:"recipient_mailbox"`
	Body             string    `json:"body"`
	Timestamp        time.Time `json:"timestamp"`
}

type SubscribeRequest struct {
	Mailbox string `json:"mailbox"`
}

type PublishRequest struct {
	Envelope Envelope `json:"envelope"`
}

type Ack struct {
	ID string `json:"id"`
}

type Error struct {
	Message string `json:"message"`
}

type Message struct {
	Type      string            `json:"type"`
	Subscribe *SubscribeRequest `json:"subscribe,omitempty"`
	Publish   *PublishRequest   `json:"publish,omitempty"`
	Incoming  *Envelope         `json:"incoming,omitempty"`
	Ack       *Ack              `json:"ack,omitempty"`
	Error     *Error            `json:"error,omitempty"`
}

func (m Message) Validate() error {
	switch m.Type {
	case MessageTypeSubscribe:
		if m.Subscribe == nil {
			return errors.New("subscribe payload is required")
		}
		if strings.TrimSpace(m.Subscribe.Mailbox) == "" {
			return errors.New("mailbox is required")
		}
	case MessageTypePublish:
		if m.Publish == nil {
			return errors.New("publish payload is required")
		}
		if err := ValidateEnvelope(m.Publish.Envelope); err != nil {
			return err
		}
	case MessageTypeIncoming:
		if m.Incoming == nil {
			return errors.New("incoming payload is required")
		}
		if err := ValidateEnvelope(*m.Incoming); err != nil {
			return err
		}
	case MessageTypeAck:
		if m.Ack == nil || strings.TrimSpace(m.Ack.ID) == "" {
			return errors.New("ack id is required")
		}
	case MessageTypeError:
		if m.Error == nil || strings.TrimSpace(m.Error.Message) == "" {
			return errors.New("error message is required")
		}
	default:
		return errors.New("unknown message type")
	}

	return nil

}

func ValidateEnvelope(envelope Envelope) error {
	if strings.TrimSpace(envelope.SenderMailbox) == "" {
		return errors.New("sender mailbox is required")
	}
	if strings.TrimSpace(envelope.RecipientMailbox) == "" {
		return errors.New("recipient mailbox is required")
	}
	if strings.TrimSpace(envelope.Body) == "" {
		return errors.New("message body is required")
	}
	return nil
}
