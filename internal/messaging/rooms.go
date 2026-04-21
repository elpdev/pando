package messaging

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/session"
	"github.com/elpdev/pando/internal/store"
	"github.com/google/uuid"
)

const (
	DefaultRoomID   = "default"
	DefaultRoomName = "general"
	DefaultRoomCap  = 16
)

func (s *Service) DefaultRoomState() (*store.RoomState, error) {
	state, err := s.store.LoadRoomState(s.identity, DefaultRoomID)
	if err == store.ErrNotFound {
		return s.defaultRoomState(), nil
	}
	if err != nil {
		return nil, err
	}
	if state.ID == "" {
		state.ID = DefaultRoomID
	}
	if state.Name == "" {
		state.Name = DefaultRoomName
	}
	return state, nil
}

func (s *Service) DefaultRoomHistory() ([]store.RoomMessageRecord, error) {
	return s.store.LoadRoomHistory(s.identity, DefaultRoomID)
}

func (s *Service) JoinDefaultRoom() (*store.RoomState, *OutgoingBatch, error) {
	state, err := s.DefaultRoomState()
	if err != nil {
		return nil, nil, err
	}
	if state.Joined {
		batch, err := s.roomMembershipBatch(state)
		return state, batch, err
	}
	now := time.Now().UTC()
	if len(state.Members) >= DefaultRoomCap {
		return nil, nil, fmt.Errorf("%s is full (%d members)", DefaultRoomLabel(), DefaultRoomCap)
	}
	state.Joined = true
	state.JoinedAt = now
	state.UpdatedAt = now
	state.Members = mergeRoomMembers(state.Members, store.RoomMember{AccountID: s.identity.AccountID, JoinedAt: now})
	if err := s.store.SaveRoomState(s.identity, state); err != nil {
		return nil, nil, err
	}
	batch, err := s.roomMembershipBatch(state)
	if err != nil {
		return nil, nil, err
	}
	return state, batch, nil
}

func (s *Service) EncryptDefaultRoomOutgoing(body string) (*OutgoingBatch, error) {
	state, err := s.DefaultRoomState()
	if err != nil {
		return nil, err
	}
	if !state.Joined {
		return nil, fmt.Errorf("join %s before sending", DefaultRoomLabel())
	}
	messageID := uuid.NewString()
	payload, err := json.Marshal(contentPayload{
		Kind: contentKindRoomMessage,
		RoomMessage: &roomMessage{
			RoomID:          DefaultRoomID,
			MessageID:       messageID,
			SenderAccountID: s.identity.AccountID,
			Body:            body,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode room message: %w", err)
	}
	envelopes, err := s.encryptForRoomMembers(state, string(payload))
	if err != nil {
		return nil, err
	}
	return &OutgoingBatch{MessageID: messageID, Envelopes: envelopes}, nil
}

func (s *Service) SaveDefaultRoomSent(messageID, body string) error {
	return s.store.AppendRoomHistory(s.identity, DefaultRoomID, store.RoomMessageRecord{
		MessageID:       messageID,
		SenderAccountID: s.identity.AccountID,
		Body:            body,
		Timestamp:       time.Now().UTC(),
	})
}

func (s *Service) SaveDefaultRoomReceived(senderAccountID, senderMailbox, messageID, body string, timestamp time.Time) error {
	return s.store.AppendRoomHistory(s.identity, DefaultRoomID, store.RoomMessageRecord{
		MessageID:       messageID,
		SenderAccountID: senderAccountID,
		SenderMailbox:   senderMailbox,
		Body:            body,
		Timestamp:       timestamp,
	})
}

func (s *Service) encryptForRoomMembers(state *store.RoomState, plaintext string) ([]protocol.Envelope, error) {
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

func (s *Service) handleRoomMembership(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	if payload == nil || payload.RoomMembership == nil {
		return nil, fmt.Errorf("room membership payload is required")
	}
	if payload.RoomMembership.RoomID != DefaultRoomID {
		return &IncomingResult{Control: true}, nil
	}
	state, err := s.DefaultRoomState()
	if err != nil {
		return nil, err
	}
	members := roomMembersFromPayload(payload.RoomMembership.Members)
	members = mergeRoomMembers(state.Members, members...)
	if len(members) > DefaultRoomCap {
		return &IncomingResult{Control: true}, nil
	}
	state.Members = members
	if state.UpdatedAt.Before(payload.RoomMembership.UpdatedAt) {
		state.UpdatedAt = payload.RoomMembership.UpdatedAt
	}
	if state.Joined && state.JoinedAt.IsZero() {
		for _, member := range state.Members {
			if member.AccountID == s.identity.AccountID {
				state.JoinedAt = member.JoinedAt
				break
			}
		}
	}
	if err := s.store.SaveRoomState(s.identity, state); err != nil {
		return nil, err
	}
	return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, RoomID: DefaultRoomID, RoomUpdated: state}, nil
}

func (s *Service) handleRoomMessage(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	if payload == nil || payload.RoomMessage == nil {
		return nil, fmt.Errorf("room message payload is required")
	}
	message := payload.RoomMessage
	if message.RoomID != DefaultRoomID {
		return nil, nil
	}
	if message.MessageID == "" {
		return nil, fmt.Errorf("room message id is required")
	}
	if message.SenderAccountID == "" {
		return nil, fmt.Errorf("room sender account is required")
	}
	if message.SenderAccountID != contact.AccountID {
		return nil, fmt.Errorf("room sender account does not match envelope sender")
	}
	state, err := s.DefaultRoomState()
	if err != nil {
		return nil, err
	}
	if !roomHasMember(state.Members, contact.AccountID) {
		members := mergeRoomMembers(state.Members, store.RoomMember{AccountID: contact.AccountID, JoinedAt: time.Now().UTC()})
		if len(members) > DefaultRoomCap {
			return &IncomingResult{Control: true}, nil
		}
		state.Members = members
		state.UpdatedAt = time.Now().UTC()
		if err := s.store.SaveRoomState(s.identity, state); err != nil {
			return nil, err
		}
	}
	return &IncomingResult{
		PeerAccountID: contact.AccountID,
		RoomID:        DefaultRoomID,
		Body:          message.Body,
		MessageID:     message.MessageID,
	}, nil
}

func (s *Service) defaultRoomState() *store.RoomState {
	return &store.RoomState{ID: DefaultRoomID, Name: DefaultRoomName}
}

func roomMembershipMembers(members []store.RoomMember) []roomMember {
	result := make([]roomMember, 0, len(members))
	for _, member := range members {
		result = append(result, roomMember{AccountID: member.AccountID, JoinedAt: member.JoinedAt})
	}
	return result
}

func roomMembersFromPayload(members []roomMember) []store.RoomMember {
	result := make([]store.RoomMember, 0, len(members))
	for _, member := range members {
		result = append(result, store.RoomMember{AccountID: member.AccountID, JoinedAt: member.JoinedAt})
	}
	return result
}

func mergeRoomMembers(existing []store.RoomMember, incoming ...store.RoomMember) []store.RoomMember {
	merged := append([]store.RoomMember(nil), existing...)
	index := make(map[string]int, len(merged))
	for i, member := range merged {
		index[member.AccountID] = i
	}
	for _, member := range incoming {
		if member.AccountID == "" {
			continue
		}
		if idx, ok := index[member.AccountID]; ok {
			if merged[idx].JoinedAt.IsZero() || (!member.JoinedAt.IsZero() && member.JoinedAt.Before(merged[idx].JoinedAt)) {
				merged[idx].JoinedAt = member.JoinedAt
			}
			continue
		}
		index[member.AccountID] = len(merged)
		merged = append(merged, member)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].JoinedAt.Equal(merged[j].JoinedAt) {
			return merged[i].AccountID < merged[j].AccountID
		}
		if merged[i].JoinedAt.IsZero() {
			return false
		}
		if merged[j].JoinedAt.IsZero() {
			return true
		}
		return merged[i].JoinedAt.Before(merged[j].JoinedAt)
	})
	return merged
}

func roomHasMember(members []store.RoomMember, accountID string) bool {
	for _, member := range members {
		if member.AccountID == accountID {
			return true
		}
	}
	return false
}

func DefaultRoomLabel() string {
	return "#" + DefaultRoomName
}
