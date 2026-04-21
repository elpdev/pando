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

const typingIndicatorTTL = 5 * time.Second

func (s *Service) EncryptOutgoing(recipientAccountID, body string) (*OutgoingBatch, error) {
	contact, err := s.loadOutgoingContact(recipientAccountID)
	if err != nil {
		return nil, err
	}
	chatEnvelopes, err := session.Encrypt(s.identity, contact, body)
	if err != nil {
		return nil, err
	}
	stampExpiresAt(chatEnvelopes, s.outgoingExpiresAt(time.Now().UTC()))
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

// stampExpiresAt sets the outer envelope expiry on every envelope in the slice.
// No-op when expiresAt is the zero time (self-destruct disabled).
func stampExpiresAt(envelopes []protocol.Envelope, expiresAt time.Time) {
	if expiresAt.IsZero() {
		return
	}
	for i := range envelopes {
		envelopes[i].ExpiresAt = expiresAt
	}
}

func (s *Service) TypingEnvelopes(recipientAccountID, state string) ([]protocol.Envelope, error) {
	switch state {
	case TypingStateActive, TypingStateIdle:
	default:
		return nil, fmt.Errorf("invalid typing state %q", state)
	}
	contact, err := s.loadOutgoingContact(recipientAccountID)
	if err != nil {
		return nil, err
	}
	payload := contentPayload{Kind: contentKindTyping, Typing: &typingIndicator{State: state}}
	if state == TypingStateActive {
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

func (s *Service) contactUpdateEnvelopes(contact *identity.Contact) ([]protocol.Envelope, error) {
	payload, err := json.Marshal(contentPayload{
		Kind: contentKindContactUpdate,
		ContactUpdate: &identity.InviteBundle{
			AccountID:            s.identity.AccountID,
			AccountSigningPublic: append([]byte(nil), s.identity.AccountSigningPublic...),
			Devices:              s.identity.DeviceBundles(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode contact update payload: %w", err)
	}
	if len(contact.ActiveDevices()) == 0 {
		return nil, nil
	}
	return session.Encrypt(s.identity, contact, string(payload))
}

func (s *Service) loadOutgoingContact(recipientAccountID string) (*identity.Contact, error) {
	contact, err := s.store.LoadContact(recipientAccountID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, missingContactError(recipientAccountID)
		}
		return nil, err
	}
	return contact, nil
}
