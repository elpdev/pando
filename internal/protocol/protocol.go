package protocol

import (
	"errors"
	"strings"
	"time"
)

const (
	MessageTypeSubscribe          = "subscribe"
	MessageTypeSubscribeChallenge = "subscribe-challenge"
	MessageTypePublish            = "publish"
	MessageTypeIncoming           = "incoming"
	MessageTypeAck                = "ack"
	MessageTypeError              = "error"
)

type Envelope struct {
	ID                           string    `json:"id"`
	ClientMessageID              string    `json:"client_message_id,omitempty"`
	SenderMailbox                string    `json:"sender_mailbox"`
	RecipientMailbox             string    `json:"recipient_mailbox"`
	Body                         string    `json:"body,omitempty"`
	BodyEncoding                 string    `json:"body_encoding,omitempty"`
	Ciphertext                   string    `json:"ciphertext,omitempty"`
	Nonce                        string    `json:"nonce,omitempty"`
	SenderDeviceSigningPublic    string    `json:"sender_device_signing_public,omitempty"`
	SenderDeviceEncryptionPublic string    `json:"sender_device_encryption_public,omitempty"`
	Signature                    string    `json:"signature,omitempty"`
	ExpiresAt                    time.Time `json:"expires_at,omitempty"`
	Timestamp                    time.Time `json:"timestamp"`
}

type SubscribeRequest struct {
	Mailbox            string    `json:"mailbox"`
	DeviceSigningKey   string    `json:"device_signing_key,omitempty"`
	DeviceProof        string    `json:"device_proof,omitempty"`
	ChallengeNonce     string    `json:"challenge_nonce,omitempty"`
	ChallengeExpiresAt time.Time `json:"challenge_expires_at,omitempty"`
}

type SubscribeChallenge struct {
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
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
	Type      string              `json:"type"`
	Subscribe *SubscribeRequest   `json:"subscribe,omitempty"`
	Challenge *SubscribeChallenge `json:"challenge,omitempty"`
	Publish   *PublishRequest     `json:"publish,omitempty"`
	Incoming  *Envelope           `json:"incoming,omitempty"`
	Ack       *Ack                `json:"ack,omitempty"`
	Error     *Error              `json:"error,omitempty"`
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
		if strings.TrimSpace(m.Subscribe.DeviceSigningKey) == "" {
			return errors.New("device signing key is required")
		}
		if strings.TrimSpace(m.Subscribe.DeviceProof) == "" {
			return errors.New("device proof is required")
		}
		if strings.TrimSpace(m.Subscribe.ChallengeNonce) == "" {
			return errors.New("challenge nonce is required")
		}
		if m.Subscribe.ChallengeExpiresAt.IsZero() {
			return errors.New("challenge expiry is required")
		}
	case MessageTypeSubscribeChallenge:
		if m.Challenge == nil {
			return errors.New("challenge payload is required")
		}
		if strings.TrimSpace(m.Challenge.Nonce) == "" {
			return errors.New("challenge nonce is required")
		}
		if m.Challenge.ExpiresAt.IsZero() {
			return errors.New("challenge expiry is required")
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

func SubscribeProofBytes(mailbox, nonce string, expiresAt time.Time) []byte {
	return []byte("subscribe\n" + mailbox + "\n" + nonce + "\n" + expiresAt.UTC().Format(time.RFC3339Nano))
}

func ValidateEnvelope(envelope Envelope) error {
	if strings.TrimSpace(envelope.SenderMailbox) == "" {
		return errors.New("sender mailbox is required")
	}
	if strings.TrimSpace(envelope.RecipientMailbox) == "" {
		return errors.New("recipient mailbox is required")
	}
	if strings.TrimSpace(envelope.Body) == "" && strings.TrimSpace(envelope.Ciphertext) == "" {
		return errors.New("message body or ciphertext is required")
	}
	if strings.TrimSpace(envelope.Ciphertext) != "" {
		if strings.TrimSpace(envelope.BodyEncoding) == "" {
			return errors.New("body encoding is required for ciphertext messages")
		}
		if strings.TrimSpace(envelope.Nonce) == "" {
			return errors.New("nonce is required for ciphertext messages")
		}
		if strings.TrimSpace(envelope.SenderDeviceSigningPublic) == "" {
			return errors.New("sender device signing public key is required for ciphertext messages")
		}
		if strings.TrimSpace(envelope.SenderDeviceEncryptionPublic) == "" {
			return errors.New("sender device encryption public key is required for ciphertext messages")
		}
		if strings.TrimSpace(envelope.Signature) == "" {
			return errors.New("signature is required for ciphertext messages")
		}
	}
	return nil
}
