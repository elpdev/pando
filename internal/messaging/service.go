package messaging

import (
	"fmt"

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
	return &Service{store: store, identity: id}, created, nil
}

func (s *Service) Identity() *identity.Identity {
	return s.identity
}

func (s *Service) EncryptOutgoing(recipientMailbox, body string) (protocol.Envelope, error) {
	contact, err := s.store.LoadContact(recipientMailbox)
	if err != nil {
		if err == store.ErrNotFound {
			return protocol.Envelope{}, fmt.Errorf("no contact for mailbox %q; import an invite first", recipientMailbox)
		}
		return protocol.Envelope{}, err
	}
	return session.Encrypt(s.identity, contact, body)
}

func (s *Service) DecryptIncoming(envelope protocol.Envelope) (string, error) {
	contact, err := s.store.LoadContact(envelope.SenderMailbox)
	if err != nil {
		if err == store.ErrNotFound {
			return "", fmt.Errorf("no contact for sender mailbox %q", envelope.SenderMailbox)
		}
		return "", err
	}
	return session.Decrypt(s.identity, contact, envelope)
}
