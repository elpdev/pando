package messaging

import (
	"fmt"
	"time"

	"github.com/elpdev/chatui/internal/identity"
	"github.com/elpdev/chatui/internal/protocol"
	"github.com/elpdev/chatui/internal/session"
	"github.com/elpdev/chatui/internal/store"
)

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

func (s *Service) SaveSent(peerMailbox, body string) error {
	return s.store.AppendHistory(s.identity, store.MessageRecord{
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

func (s *Service) EncryptOutgoing(recipientAccountID, body string) ([]protocol.Envelope, error) {
	contact, err := s.store.LoadContact(recipientAccountID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, fmt.Errorf("no contact for account %q; import an updated invite first", recipientAccountID)
		}
		return nil, err
	}
	return session.Encrypt(s.identity, contact, body)
}

func (s *Service) DecryptIncoming(envelope protocol.Envelope) (string, error) {
	contact, err := s.store.LoadContactByDeviceMailbox(envelope.SenderMailbox)
	if err != nil {
		if err == store.ErrNotFound {
			return "", fmt.Errorf("no contact device for sender mailbox %q", envelope.SenderMailbox)
		}
		return "", err
	}
	return session.Decrypt(s.identity, contact, envelope)
}
