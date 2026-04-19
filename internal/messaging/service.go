package messaging

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/session"
	"github.com/elpdev/pando/internal/store"
)

// DirectoryClient is the subset of the relay API needed to resolve a contact
// from the trusted directory. Kept as an interface so tests can stub it.
type DirectoryClient interface {
	LookupDirectoryEntry(mailbox string) (*relayapi.SignedDirectoryEntry, error)
}

const BodyEncodingContactUpdate = "contact-update-v1"

const (
	incomingAttachmentTTL             = 15 * time.Minute
	maxPendingIncomingAttachments     = 128
	maxPendingIncomingAttachmentsPeer = 16
	typingIndicatorTTL                = 5 * time.Second
)

type deliveryAck struct {
	MessageID   string    `json:"message_id"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type IncomingResult struct {
	Duplicate       bool
	Control         bool
	PeerAccountID   string
	Body            string
	MessageID       string
	ContactUpdated  *identity.Contact
	AckEnvelopes    []protocol.Envelope
	TypingState     string
	TypingExpiresAt time.Time
}

type OutgoingBatch struct {
	MessageID string
	Envelopes []protocol.Envelope
}

type Service struct {
	store               *store.ClientStore
	identity            *identity.Identity
	incomingAttachments *incomingAttachmentAssembler
}

func New(store *store.ClientStore, mailbox string) (*Service, bool, error) {
	id, created, err := store.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return nil, false, err
	}
	if err := id.Validate(); err != nil {
		return nil, false, err
	}
	return &Service{store: store, identity: id, incomingAttachments: newIncomingAttachmentAssembler(store)}, created, nil
}

func (s *Service) Identity() *identity.Identity {
	return s.identity
}

func (s *Service) Contact(mailbox string) (*identity.Contact, error) {
	return s.store.LoadContact(mailbox)
}

func (s *Service) Contacts() ([]identity.Contact, error) {
	return s.store.ListContacts()
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
			return nil, missingContactError(recipientAccountID)
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

func (s *Service) TypingEnvelopes(recipientAccountID, state string) ([]protocol.Envelope, error) {
	switch state {
	case typingStateActive, typingStateIdle:
	default:
		return nil, fmt.Errorf("invalid typing state %q", state)
	}
	contact, err := s.store.LoadContact(recipientAccountID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, missingContactError(recipientAccountID)
		}
		return nil, err
	}
	payload := contentPayload{Kind: contentKindTyping, Typing: &typingIndicator{State: state}}
	if state == typingStateActive {
		payload.Typing.ExpiresAt = time.Now().UTC().Add(typingIndicatorTTL)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode typing payload: %w", err)
	}
	return session.Encrypt(s.identity, contact, string(body))
}

func missingContactError(recipientAccountID string) error {
	return fmt.Errorf("no contact for account %q; import their invite first with pando contact add --mailbox <your-mailbox> --paste", recipientAccountID)
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

	contact, err := s.resolveIncomingSender(envelope.SenderMailbox)
	if err != nil {
		return nil, err
	}

	if envelope.BodyEncoding == BodyEncodingContactUpdate {
		updated, err := s.applyContactUpdate(contact, envelope)
		if err != nil {
			return nil, err
		}
		return &IncomingResult{Control: true, PeerAccountID: updated.AccountID, ContactUpdated: updated}, nil
	}
	body, err := session.Decrypt(s.identity, contact, envelope)
	if err != nil {
		return nil, err
	}
	payload, ok, err := decodeContentPayload(body)
	if err != nil {
		return nil, err
	}
	if ok {
		result, handled, err := s.handleIncomingPayload(contact, payload)
		if err != nil {
			return nil, err
		}
		if handled {
			return result, nil
		}
	}
	ackEnvelopes, err := s.deliveryAckEnvelopes(envelope)
	if err != nil {
		return nil, err
	}
	return &IncomingResult{PeerAccountID: contact.AccountID, Body: body, MessageID: envelope.ClientMessageID, AckEnvelopes: ackEnvelopes}, nil
}

func (s *Service) resolveIncomingSender(senderMailbox string) (*identity.Contact, error) {
	contact, err := s.store.LoadContactByDeviceMailbox(senderMailbox)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, fmt.Errorf("no contact device for sender mailbox %q", senderMailbox)
		}
		return nil, err
	}
	return contact, nil
}

func (s *Service) handleIncomingPayload(contact *identity.Contact, payload *contentPayload) (*IncomingResult, bool, error) {
	switch payload.Kind {
	case contentKindAttachmentChunk:
		message, done, err := s.handleIncomingAttachmentChunk(contact.AccountID, payload.AttachmentChunk)
		if err != nil {
			return nil, true, err
		}
		if !done {
			return &IncomingResult{Control: true, PeerAccountID: contact.AccountID}, true, nil
		}
		return &IncomingResult{PeerAccountID: contact.AccountID, Body: message}, true, nil
	case contentKindDeliveryAck:
		ack, err := s.parseDeliveryAckPayload(payload)
		if err != nil {
			return nil, true, err
		}
		if err := s.MarkDelivered(contact.AccountID, ack.MessageID, ack.DeliveredAt); err != nil {
			return nil, true, err
		}
		return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, MessageID: ack.MessageID}, true, nil
	case contentKindTyping:
		typing, err := s.parseTypingPayload(payload)
		if err != nil {
			return nil, true, err
		}
		return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, TypingState: typing.State, TypingExpiresAt: typing.ExpiresAt}, true, nil
	default:
		return nil, false, nil
	}
}

func (s *Service) handleIncomingAttachmentChunk(peerAccountID string, chunk *attachmentChunkPayload) (string, bool, error) {
	if s.incomingAttachments == nil {
		s.incomingAttachments = newIncomingAttachmentAssembler(s.store)
	}
	return s.incomingAttachments.handleChunk(peerAccountID, chunk)
}

func validateAttachmentMIMEType(path, mimeType, attachmentType string) error {
	switch attachmentType {
	case attachmentTypePhoto:
		if strings.HasPrefix(mimeType, "image/") {
			return nil
		}
		return fmt.Errorf("%s is not a supported image file", path)
	case attachmentTypeVoice:
		if strings.HasPrefix(mimeType, "audio/") {
			return nil
		}
		return fmt.Errorf("%s is not a supported audio file", path)
	case attachmentTypeFile:
		return nil
	default:
		return fmt.Errorf("unsupported attachment type %q", attachmentType)
	}
}

func detectAttachmentMIMEType(filename string, bytes []byte, attachmentType string) string {
	mimeType := http.DetectContentType(bytes)
	ext := strings.ToLower(filepath.Ext(filename))
	if attachmentType == attachmentTypeVoice && ext == ".m4a" && (mimeType == "application/octet-stream" || mimeType == "application/mp4" || mimeType == "video/mp4") {
		return "audio/mp4"
	}
	if mimeType == "application/octet-stream" && ext != "" {
		if byExt := mime.TypeByExtension(ext); byExt != "" {
			return byExt
		}
	}
	return mimeType
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
	updated.TrustSource = existing.TrustSource
	updated.NormalizeTrust()
	if err := s.store.SaveContact(updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) deliveryAckEnvelopes(envelope protocol.Envelope) ([]protocol.Envelope, error) {
	if envelope.ClientMessageID == "" {
		return nil, nil
	}
	contact, err := s.store.LoadContactByDeviceMailbox(envelope.SenderMailbox)
	if err != nil {
		return nil, err
	}
	ackBody, err := json.Marshal(contentPayload{Kind: contentKindDeliveryAck, DeliveryAck: &deliveryAck{MessageID: envelope.ClientMessageID, DeliveredAt: time.Now().UTC()}})
	if err != nil {
		return nil, fmt.Errorf("encode delivery ack: %w", err)
	}
	return session.Encrypt(s.identity, contact, string(ackBody))
}

func (s *Service) parseDeliveryAckPayload(payload *contentPayload) (*deliveryAck, error) {
	if payload == nil || payload.DeliveryAck == nil {
		return nil, fmt.Errorf("delivery ack payload is required")
	}
	ack := *payload.DeliveryAck
	if ack.MessageID == "" {
		return nil, fmt.Errorf("delivery ack message id is required")
	}
	if ack.DeliveredAt.IsZero() {
		ack.DeliveredAt = time.Now().UTC()
	}
	return &ack, nil
}

func (s *Service) parseTypingPayload(payload *contentPayload) (*typingIndicator, error) {
	if payload == nil || payload.Typing == nil {
		return nil, fmt.Errorf("typing payload is required")
	}
	typing := *payload.Typing
	switch typing.State {
	case typingStateActive:
		if typing.ExpiresAt.IsZero() {
			return nil, fmt.Errorf("typing expiry is required")
		}
	case typingStateIdle:
		typing.ExpiresAt = time.Time{}
	default:
		return nil, fmt.Errorf("invalid typing state %q", typing.State)
	}
	return &typing, nil
}
