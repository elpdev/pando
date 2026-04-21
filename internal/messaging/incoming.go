package messaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/session"
	"github.com/elpdev/pando/internal/store"
)

func (s *Service) HandleIncoming(envelope protocol.Envelope) (*IncomingResult, error) {
	duplicate, err := s.markIncomingEnvelopeSeen(envelope.ID)
	if err != nil {
		return nil, err
	}
	if duplicate {
		return &IncomingResult{Duplicate: true}, nil
	}

	contact, err := s.store.LoadContactByDeviceMailbox(envelope.SenderMailbox)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		return s.handleIncomingUnknownSender(envelope)
	}

	body, err := s.decryptIncomingEnvelope(contact, envelope)
	if err != nil {
		return nil, err
	}

	result, handled, err := s.dispatchIncomingPayload(contact, body)
	if err != nil {
		return nil, err
	}
	if handled {
		if result != nil && result.ExpiresAt.IsZero() {
			result.ExpiresAt = envelope.ExpiresAt
		}
		return result, nil
	}

	return s.buildIncomingChatResult(contact, envelope, body)
}

func (s *Service) markIncomingEnvelopeSeen(envelopeID string) (bool, error) {
	seen, err := s.store.HasSeenEnvelope(s.identity, envelopeID)
	if err != nil {
		return false, err
	}
	if seen {
		return true, nil
	}
	if err := s.store.MarkEnvelopeSeen(s.identity, envelopeID); err != nil {
		return false, err
	}
	return false, nil
}

func (s *Service) decryptIncomingEnvelope(contact *identity.Contact, envelope protocol.Envelope) (string, error) {
	return session.Decrypt(s.identity, contact, envelope)
}

func (s *Service) handleIncomingUnknownSender(envelope protocol.Envelope) (*IncomingResult, error) {
	if s.directory == nil {
		return nil, fmt.Errorf("no contact device for sender mailbox %q", envelope.SenderMailbox)
	}
	entry, err := s.directory.LookupDirectoryEntryByDeviceMailbox(envelope.SenderMailbox)
	if err != nil {
		return nil, fmt.Errorf("no contact device for sender mailbox %q", envelope.SenderMailbox)
	}
	if err := relayapi.VerifySignedDirectoryEntry(*entry); err != nil {
		return nil, err
	}
	contact, err := identity.ContactFromInvite(entry.Entry.Bundle)
	if err != nil {
		return nil, err
	}
	body, err := s.decryptIncomingEnvelope(contact, envelope)
	if err != nil {
		return nil, err
	}
	payload, ok, err := decodeContentPayload(body)
	if err != nil {
		return nil, err
	}
	if !ok || (payload.Kind != contentKindContactRequest && payload.Kind != contentKindContactResponse && payload.Kind != contentKindRoomMembership) {
		return nil, fmt.Errorf("no contact device for sender mailbox %q", envelope.SenderMailbox)
	}
	result, err := s.handleIncomingPayload(contact, payload)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) buildIncomingChatResult(contact *identity.Contact, envelope protocol.Envelope, body string) (*IncomingResult, error) {
	ackEnvelopes, err := s.deliveryAckEnvelopes(envelope)
	if err != nil {
		return nil, err
	}
	return &IncomingResult{
		PeerAccountID: contact.AccountID,
		Body:          body,
		MessageID:     envelope.ClientMessageID,
		AckEnvelopes:  ackEnvelopes,
		ExpiresAt:     envelope.ExpiresAt,
	}, nil
}

func (s *Service) deliveryAckEnvelopes(envelope protocol.Envelope) ([]protocol.Envelope, error) {
	if envelope.ClientMessageID == "" {
		return nil, nil
	}
	contact, err := s.store.LoadContactByDeviceMailbox(envelope.SenderMailbox)
	if err != nil {
		return nil, err
	}
	ackBody, err := json.Marshal(contentPayload{
		Kind: contentKindDeliveryAck,
		DeliveryAck: &deliveryAck{
			MessageID:   envelope.ClientMessageID,
			DeliveredAt: time.Now().UTC(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode delivery ack: %w", err)
	}
	return session.Encrypt(s.identity, contact, string(ackBody))
}
