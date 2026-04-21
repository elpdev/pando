package messaging

import (
	"encoding/json"
	"fmt"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/session"
	"github.com/elpdev/pando/internal/store"
)

func (s *Service) encryptForRoomMembers(state *store.RoomState, plaintext string) ([]protocol.Envelope, error) {
	if state == nil {
		return nil, fmt.Errorf("room state is required")
	}
	targets, err := s.roomSyncRecipients(state)
	if err != nil {
		return nil, err
	}
	return s.encryptForContacts(targets, plaintext)
}

func (s *Service) encryptForContacts(targets []*identity.Contact, plaintext string) ([]protocol.Envelope, error) {
	envelopes := make([]protocol.Envelope, 0, len(targets))
	for _, contact := range targets {
		memberEnvelopes, err := session.Encrypt(s.identity, contact, plaintext)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, memberEnvelopes...)
	}
	return envelopes, nil
}

func (s *Service) roomSyncRecipients(state *store.RoomState) ([]*identity.Contact, error) {
	if state == nil {
		return nil, fmt.Errorf("room state is required")
	}
	targets := make([]*identity.Contact, 0, len(state.Members))
	for _, member := range state.Members {
		if member.AccountID == "" || member.AccountID == s.identity.AccountID {
			continue
		}
		contact, err := s.loadOutgoingContact(member.AccountID)
		if err != nil {
			continue
		}
		targets = append(targets, contact)
	}
	return targets, nil
}

func (s *Service) roomMembershipBatch(state *store.RoomState) (*OutgoingBatch, error) {
	payload, err := json.Marshal(contentPayload{
		Kind: contentKindRoomMembership,
		RoomMembership: &roomMembership{
			RoomID:    DefaultRoomID,
			UpdatedAt: state.UpdatedAt,
			Members:   roomMembershipMembers(state.Members),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode room membership: %w", err)
	}
	contacts, err := s.store.ListContacts()
	if err != nil {
		return nil, err
	}
	envelopes := make([]protocol.Envelope, 0, len(contacts))
	for _, contact := range contacts {
		contact := contact
		memberEnvelopes, err := session.Encrypt(s.identity, &contact, string(payload))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, memberEnvelopes...)
	}
	return &OutgoingBatch{Envelopes: envelopes}, nil
}
