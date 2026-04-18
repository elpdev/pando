package messaging

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/session"
	"github.com/elpdev/pando/internal/store"
)

const BodyEncodingContactUpdate = "contact-update-v1"
const BodyEncodingDeliveryAck = "delivery-ack-v1"

type deliveryAck struct {
	MessageID   string    `json:"message_id"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type IncomingResult struct {
	Duplicate      bool
	Control        bool
	PeerAccountID  string
	Body           string
	MessageID      string
	ContactUpdated *identity.Contact
	AckEnvelopes   []protocol.Envelope
}

type OutgoingBatch struct {
	MessageID string
	Envelopes []protocol.Envelope
}

type Service struct {
	store    *store.ClientStore
	identity *identity.Identity
}

func New(store *store.ClientStore, mailbox string) (*Service, bool, error) {
	id, created, err := store.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return nil, false, err
	}
	if err := id.Validate(); err != nil {
		return nil, false, err
	}
	return &Service{store: store, identity: id}, created, nil
}

func (s *Service) Identity() *identity.Identity {
	return s.identity
}

func (s *Service) Contact(mailbox string) (*identity.Contact, error) {
	return s.store.LoadContact(mailbox)
}

func (s *Service) Devices() ([]identity.Device, error) {
	if err := s.identity.Validate(); err != nil {
		return nil, err
	}
	devices := make([]identity.Device, 0, len(s.identity.Devices))
	for _, device := range s.identity.Devices {
		devices = append(devices, device)
	}
	return devices, nil
}

func (s *Service) History(peerMailbox string) ([]store.MessageRecord, error) {
	return s.store.LoadHistory(s.identity, peerMailbox)
}

func (s *Service) SaveSent(peerMailbox, messageID, body string) error {
	return s.store.AppendHistory(s.identity, store.MessageRecord{
		MessageID:   messageID,
		PeerMailbox: peerMailbox,
		Direction:   "outbound",
		Body:        body,
		Timestamp:   time.Now().UTC(),
	})
}

func (s *Service) SaveReceived(peerMailbox, body string, timestamp time.Time) error {
	return s.store.AppendHistory(s.identity, store.MessageRecord{
		PeerMailbox: peerMailbox,
		Direction:   "inbound",
		Body:        body,
		Timestamp:   timestamp,
	})
}

func (s *Service) MarkDelivered(peerMailbox, messageID string, deliveredAt time.Time) error {
	return s.store.MarkHistoryDelivered(s.identity, peerMailbox, messageID, deliveredAt)
}

func (s *Service) EncryptOutgoing(recipientAccountID, body string) (*OutgoingBatch, error) {
	contact, err := s.store.LoadContact(recipientAccountID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, fmt.Errorf("no contact for account %q; import an updated invite first", recipientAccountID)
		}
		return nil, err
	}
	chatEnvelopes, err := session.Encrypt(s.identity, contact, body)
	if err != nil {
		return nil, err
	}
	updateEnvelopes, err := s.contactUpdateEnvelopes(contact)
	if err != nil {
		return nil, err
	}
	messageID := ""
	if len(chatEnvelopes) > 0 {
		messageID = chatEnvelopes[0].ClientMessageID
	}
	return &OutgoingBatch{MessageID: messageID, Envelopes: append(updateEnvelopes, chatEnvelopes...)}, nil
}

func (s *Service) HandleIncoming(envelope protocol.Envelope) (*IncomingResult, error) {
	seen, err := s.store.HasSeenEnvelope(s.identity, envelope.ID)
	if err != nil {
		return nil, err
	}
	if seen {
		return &IncomingResult{Duplicate: true}, nil
	}
	if err := s.store.MarkEnvelopeSeen(s.identity, envelope.ID); err != nil {
		return nil, err
	}

	contact, err := s.store.LoadContactByDeviceMailbox(envelope.SenderMailbox)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, fmt.Errorf("no contact device for sender mailbox %q", envelope.SenderMailbox)
		}
		return nil, err
	}

	if envelope.BodyEncoding == BodyEncodingContactUpdate {
		updated, err := s.applyContactUpdate(contact, envelope)
		if err != nil {
			return nil, err
		}
		return &IncomingResult{Control: true, PeerAccountID: updated.AccountID, ContactUpdated: updated}, nil
	}
	if envelope.BodyEncoding == BodyEncodingDeliveryAck {
		ack, err := s.parseDeliveryAck(envelope)
		if err != nil {
			return nil, err
		}
		if err := s.MarkDelivered(contact.AccountID, ack.MessageID, ack.DeliveredAt); err != nil {
			return nil, err
		}
		return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, MessageID: ack.MessageID}, nil
	}

	body, err := session.Decrypt(s.identity, contact, envelope)
	if err != nil {
		return nil, err
	}
	ackEnvelopes, err := s.deliveryAckEnvelopes(envelope)
	if err != nil {
		return nil, err
	}
	return &IncomingResult{PeerAccountID: contact.AccountID, Body: body, MessageID: envelope.ClientMessageID, AckEnvelopes: ackEnvelopes}, nil
}

func (s *Service) contactUpdateEnvelopes(contact *identity.Contact) ([]protocol.Envelope, error) {
	currentDevice, err := s.identity.CurrentDevice()
	if err != nil {
		return nil, err
	}
	bundleBytes, err := json.Marshal(s.identity.InviteBundle())
	if err != nil {
		return nil, fmt.Errorf("encode contact update bundle: %w", err)
	}
	devices := contact.ActiveDevices()
	envelopes := make([]protocol.Envelope, 0, len(devices))
	for _, device := range devices {
		envelopes = append(envelopes, protocol.Envelope{
			SenderMailbox:    currentDevice.Mailbox,
			RecipientMailbox: device.Mailbox,
			BodyEncoding:     BodyEncodingContactUpdate,
			Body:             string(bundleBytes),
		})
	}
	return envelopes, nil
}

func (s *Service) applyContactUpdate(existing *identity.Contact, envelope protocol.Envelope) (*identity.Contact, error) {
	var bundle identity.InviteBundle
	if err := json.Unmarshal([]byte(envelope.Body), &bundle); err != nil {
		return nil, fmt.Errorf("decode contact update bundle: %w", err)
	}
	updated, err := identity.ContactFromInvite(bundle)
	if err != nil {
		return nil, err
	}
	if existing.Fingerprint() != updated.Fingerprint() || existing.AccountID != updated.AccountID {
		return nil, fmt.Errorf("contact update does not match stored identity for sender %s", envelope.SenderMailbox)
	}
	updated.Verified = existing.Verified
	if err := s.store.SaveContact(updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) deliveryAckEnvelopes(envelope protocol.Envelope) ([]protocol.Envelope, error) {
	if envelope.ClientMessageID == "" {
		return nil, nil
	}
	currentDevice, err := s.identity.CurrentDevice()
	if err != nil {
		return nil, err
	}
	ackBody, err := json.Marshal(deliveryAck{MessageID: envelope.ClientMessageID, DeliveredAt: time.Now().UTC()})
	if err != nil {
		return nil, fmt.Errorf("encode delivery ack: %w", err)
	}
	return []protocol.Envelope{{SenderMailbox: currentDevice.Mailbox, RecipientMailbox: envelope.SenderMailbox, BodyEncoding: BodyEncodingDeliveryAck, Body: string(ackBody)}}, nil
}

func (s *Service) parseDeliveryAck(envelope protocol.Envelope) (*deliveryAck, error) {
	var ack deliveryAck
	if err := json.Unmarshal([]byte(envelope.Body), &ack); err != nil {
		return nil, fmt.Errorf("decode delivery ack: %w", err)
	}
	if ack.MessageID == "" {
		return nil, fmt.Errorf("delivery ack message id is required")
	}
	if ack.DeliveredAt.IsZero() {
		ack.DeliveredAt = time.Now().UTC()
	}
	return &ack, nil
}
