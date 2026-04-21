package messaging

import (
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
)

// DirectoryClient is the subset of the relay API needed to resolve a contact
// from the trusted directory. Kept as an interface so tests can stub it.
type DirectoryClient interface {
	LookupDirectoryEntry(mailbox string) (*relayapi.SignedDirectoryEntry, error)
	LookupDirectoryEntryByDeviceMailbox(mailbox string) (*relayapi.SignedDirectoryEntry, error)
	ListDiscoverableEntries() ([]relayapi.SignedDirectoryEntry, error)
}

type IncomingResult struct {
	Duplicate     bool
	Control       bool
	PeerAccountID string
	RoomID        string

	// Body and AckEnvelopes are set for chat messages. Control messages may set
	// ContactUpdated, MessageID, or TypingState instead.
	Body         string
	Attachment   *store.AttachmentRecord
	AckEnvelopes []protocol.Envelope

	// MessageID is used by chat messages and delivery acknowledgements.
	MessageID string

	// ContactUpdated is set only for contact-update control messages.
	ContactUpdated *identity.Contact
	ContactChange  ContactUpdateChange
	ContactRequest *store.ContactRequest
	RoomUpdated    *store.RoomState
	RoomSync       *RoomSyncUpdate

	// TypingState and TypingExpiresAt are set only for typing control messages.
	TypingState     string
	TypingExpiresAt time.Time
}

type ContactUpdateChange string

const (
	ContactUpdateUnchanged     ContactUpdateChange = ""
	ContactUpdateDeviceAdded   ContactUpdateChange = "device-added"
	ContactUpdateDeviceRevoked ContactUpdateChange = "device-revoked"
	ContactUpdateDeviceRotated ContactUpdateChange = "device-rotated"
	ContactUpdateDeviceChanged ContactUpdateChange = "device-changed"
)

type OutgoingBatch struct {
	MessageID  string
	Envelopes  []protocol.Envelope
	Attachment *store.AttachmentRecord
}

type RoomSyncUpdate struct {
	RequestID string
	Added     int
	Complete  bool
}

type Service struct {
	store               *store.ClientStore
	identity            *identity.Identity
	incomingAttachments *incomingAttachmentAssembler
	directory           DirectoryClient
}

func New(store *store.ClientStore, mailbox string) (*Service, bool, error) {
	id, created, err := store.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return nil, false, err
	}
	if err := id.Validate(); err != nil {
		return nil, false, err
	}
	return &Service{
		store:               store,
		identity:            id,
		incomingAttachments: newIncomingAttachmentAssembler(store, id),
	}, created, nil
}

// Store facade used by the UI and command layer.
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

func (s *Service) SetDirectoryClient(client DirectoryClient) {
	s.directory = client
}

func (s *Service) ContactRequests() ([]store.ContactRequest, error) {
	return s.store.ListContactRequests()
}

func (s *Service) History(peerMailbox string) ([]store.MessageRecord, error) {
	return s.store.LoadHistory(s.identity, peerMailbox)
}

func (s *Service) SaveSent(peerMailbox, messageID, body string, attachment *store.AttachmentRecord) error {
	return s.store.AppendHistory(s.identity, store.MessageRecord{
		MessageID:   messageID,
		PeerMailbox: peerMailbox,
		Direction:   "outbound",
		Body:        body,
		Attachment:  attachment,
		Timestamp:   time.Now().UTC(),
	})
}

func (s *Service) SaveReceived(peerMailbox, body string, timestamp time.Time, attachment *store.AttachmentRecord) error {
	return s.store.AppendHistory(s.identity, store.MessageRecord{
		PeerMailbox: peerMailbox,
		Direction:   "inbound",
		Body:        body,
		Attachment:  attachment,
		Timestamp:   timestamp,
	})
}

func (s *Service) MarkDelivered(peerMailbox, messageID string, deliveredAt time.Time) error {
	return s.store.MarkHistoryDelivered(s.identity, peerMailbox, messageID, deliveredAt)
}

func (s *Service) AttachmentBytes(path string) ([]byte, error) {
	return s.store.ReadAttachment(s.identity, path)
}
