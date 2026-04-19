package messaging

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/session"
	"github.com/elpdev/pando/internal/store"
)

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
	incomingAttachments map[string]*incomingAttachment
}

func New(store *store.ClientStore, mailbox string) (*Service, bool, error) {
	id, created, err := store.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return nil, false, err
	}
	if err := id.Validate(); err != nil {
		return nil, false, err
	}
	return &Service{store: store, identity: id, incomingAttachments: make(map[string]*incomingAttachment)}, created, nil
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

func (s *Service) PreparePhotoOutgoing(recipientAccountID, path string) (*OutgoingBatch, string, error) {
	return s.prepareAttachmentOutgoing(recipientAccountID, path, attachmentTypePhoto)
}

func (s *Service) PrepareVoiceOutgoing(recipientAccountID, path string) (*OutgoingBatch, string, error) {
	return s.prepareAttachmentOutgoing(recipientAccountID, path, attachmentTypeVoice)
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

func (s *Service) prepareAttachmentOutgoing(recipientAccountID, path, attachmentType string) (*OutgoingBatch, string, error) {
	contact, err := s.store.LoadContact(recipientAccountID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, "", missingContactError(recipientAccountID)
		}
		return nil, "", err
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", attachmentLabel(attachmentType), err)
	}
	filename := filepath.Base(path)
	mimeType := detectAttachmentMIMEType(filename, bytes, attachmentType)
	if err := validateAttachmentMIMEType(path, mimeType, attachmentType); err != nil {
		return nil, "", err
	}
	payloads, _, err := buildAttachmentChunkPayloads(attachmentType, filename, mimeType, bytes)
	if err != nil {
		return nil, "", err
	}
	updateEnvelopes, err := s.contactUpdateEnvelopes(contact)
	if err != nil {
		return nil, "", err
	}
	envelopes := make([]protocol.Envelope, 0, len(updateEnvelopes)+(len(payloads)*len(contact.ActiveDevices())))
	envelopes = append(envelopes, updateEnvelopes...)
	for _, payload := range payloads {
		chunkEnvelopes, err := session.Encrypt(s.identity, contact, payload)
		if err != nil {
			return nil, "", err
		}
		envelopes = append(envelopes, chunkEnvelopes...)
	}
	return &OutgoingBatch{Envelopes: envelopes}, fmt.Sprintf("%s sent: %s", attachmentLabel(attachmentType), sanitizeAttachmentName(filename)), nil
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
	body, err := session.Decrypt(s.identity, contact, envelope)
	if err != nil {
		return nil, err
	}
	payload, ok, err := decodeContentPayload(body)
	if err != nil {
		return nil, err
	}
	if ok && payload.Kind == contentKindAttachmentChunk {
		message, done, err := s.handleIncomingAttachmentChunk(contact.AccountID, payload.AttachmentChunk)
		if err != nil {
			return nil, err
		}
		if !done {
			return &IncomingResult{Control: true, PeerAccountID: contact.AccountID}, nil
		}
		return &IncomingResult{PeerAccountID: contact.AccountID, Body: message}, nil
	}
	if ok && payload.Kind == contentKindDeliveryAck {
		ack, err := s.parseDeliveryAckPayload(payload)
		if err != nil {
			return nil, err
		}
		if err := s.MarkDelivered(contact.AccountID, ack.MessageID, ack.DeliveredAt); err != nil {
			return nil, err
		}
		return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, MessageID: ack.MessageID}, nil
	}
	if ok && payload.Kind == contentKindTyping {
		typing, err := s.parseTypingPayload(payload)
		if err != nil {
			return nil, err
		}
		return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, TypingState: typing.State, TypingExpiresAt: typing.ExpiresAt}, nil
	}
	ackEnvelopes, err := s.deliveryAckEnvelopes(envelope)
	if err != nil {
		return nil, err
	}
	return &IncomingResult{PeerAccountID: contact.AccountID, Body: body, MessageID: envelope.ClientMessageID, AckEnvelopes: ackEnvelopes}, nil
}

func (s *Service) handleIncomingAttachmentChunk(peerAccountID string, chunk *attachmentChunkPayload) (string, bool, error) {
	if s.incomingAttachments == nil {
		s.incomingAttachments = make(map[string]*incomingAttachment)
	}
	now := time.Now().UTC()
	s.cleanupIncomingAttachments(now)
	if chunk == nil {
		return "", false, fmt.Errorf("attachment payload is required")
	}
	if chunk.AttachmentType != attachmentTypePhoto && chunk.AttachmentType != attachmentTypeVoice {
		return "", false, fmt.Errorf("invalid attachment payload type")
	}
	if chunk.AttachmentID == "" || chunk.Filename == "" || chunk.TotalSize <= 0 || chunk.TotalSize > maxAttachmentSizeBytes || chunk.ChunkCount <= 0 || chunk.ChunkCount > maxAttachmentChunkCount || chunk.ChunkIndex < 0 || chunk.ChunkIndex >= chunk.ChunkCount {
		return "", false, fmt.Errorf("invalid attachment payload metadata")
	}
	bytes, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		return "", false, fmt.Errorf("decode attachment chunk: %w", err)
	}
	if len(bytes) == 0 || len(bytes) > attachmentChunkSizeBytes {
		return "", false, fmt.Errorf("invalid attachment chunk size")
	}
	key := peerAccountID + ":" + chunk.AttachmentID
	pending, ok := s.incomingAttachments[key]
	if !ok {
		if len(s.incomingAttachments) >= maxPendingIncomingAttachments {
			return "", false, fmt.Errorf("too many pending attachments")
		}
		if s.pendingAttachmentCount(peerAccountID) >= maxPendingIncomingAttachmentsPeer {
			return "", false, fmt.Errorf("too many pending attachments for peer %s", peerAccountID)
		}
		pending = &incomingAttachment{
			attachmentType: chunk.AttachmentType,
			filename:       sanitizeAttachmentName(chunk.Filename),
			mimeType:       chunk.MIMEType,
			totalSize:      chunk.TotalSize,
			chunkCount:     chunk.ChunkCount,
			chunks:         make([][]byte, chunk.ChunkCount),
			updatedAt:      now,
		}
		s.incomingAttachments[key] = pending
	}
	if pending.attachmentType != chunk.AttachmentType || pending.chunkCount != chunk.ChunkCount || pending.totalSize != chunk.TotalSize || pending.filename != sanitizeAttachmentName(chunk.Filename) {
		delete(s.incomingAttachments, key)
		return "", false, fmt.Errorf("attachment payload does not match existing transfer")
	}
	pending.updatedAt = now
	if pending.chunks[chunk.ChunkIndex] == nil {
		pending.chunks[chunk.ChunkIndex] = bytes
		pending.received++
	}
	if pending.received != pending.chunkCount {
		return "", false, nil
	}
	assembled := make([]byte, 0, pending.totalSize)
	for _, part := range pending.chunks {
		if part == nil {
			return "", false, fmt.Errorf("attachment transfer is missing chunks")
		}
		assembled = append(assembled, part...)
	}
	if pending.totalSize > 0 && len(assembled) != pending.totalSize {
		return "", false, fmt.Errorf("attachment transfer size mismatch")
	}
	path, err := s.store.SaveAttachment(peerAccountID, chunk.AttachmentID, pending.filename, assembled)
	if err != nil {
		return "", false, err
	}
	delete(s.incomingAttachments, key)
	return fmt.Sprintf("%s received: %s saved to %s", attachmentLabel(pending.attachmentType), pending.filename, path), true, nil
}

func (s *Service) cleanupIncomingAttachments(now time.Time) {
	for key, pending := range s.incomingAttachments {
		if pending == nil || now.Sub(pending.updatedAt) > incomingAttachmentTTL {
			delete(s.incomingAttachments, key)
		}
	}
}

func (s *Service) pendingAttachmentCount(peerAccountID string) int {
	count := 0
	prefix := peerAccountID + ":"
	for key := range s.incomingAttachments {
		if strings.HasPrefix(key, prefix) {
			count++
		}
	}
	return count
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

func attachmentLabel(attachmentType string) string {
	switch attachmentType {
	case attachmentTypePhoto:
		return "photo"
	case attachmentTypeVoice:
		return "voice note"
	default:
		return "attachment"
	}
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
