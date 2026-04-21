package messaging

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/elpdev/pando/internal/store"
	"github.com/google/uuid"
)

const (
	DefaultRoomID        = "default"
	DefaultRoomName      = "general"
	DefaultRoomCap       = 16
	roomHistoryMaxAge    = 7 * 24 * time.Hour
	roomHistoryChunkSize = 64
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

func (s *Service) RequestDefaultRoomHistory() (*OutgoingBatch, string, error) {
	state, err := s.DefaultRoomState()
	if err != nil {
		return nil, "", err
	}
	if !state.Joined {
		return nil, "", fmt.Errorf("join %s before syncing history", DefaultRoomLabel())
	}
	recipients, err := s.roomSyncRecipients(state)
	if err != nil {
		return nil, "", err
	}
	if len(recipients) == 0 {
		return nil, "", nil
	}
	now := time.Now().UTC()
	since := now.Add(-roomHistoryMaxAge)
	if state.JoinedAt.After(since) {
		since = state.JoinedAt
	}
	requestID := uuid.NewString()
	payload, err := json.Marshal(contentPayload{
		Kind: contentKindRoomHistoryRequest,
		RoomHistoryRequest: &roomHistoryRequest{
			RoomID:    DefaultRoomID,
			RequestID: requestID,
			Since:     since,
			Until:     now,
		},
	})
	if err != nil {
		return nil, "", fmt.Errorf("encode room history request: %w", err)
	}
	envelopes, err := s.encryptForContacts(recipients, string(payload))
	if err != nil {
		return nil, "", err
	}
	if len(envelopes) == 0 {
		return nil, "", nil
	}
	return &OutgoingBatch{Envelopes: envelopes}, requestID, nil
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
	stampExpiresAt(envelopes, s.outgoingExpiresAt(time.Now().UTC()))
	return &OutgoingBatch{MessageID: messageID, Envelopes: envelopes}, nil
}

func (s *Service) SaveDefaultRoomSent(messageID, body string) error {
	now := time.Now().UTC()
	return s.store.AppendRoomHistory(s.identity, DefaultRoomID, store.RoomMessageRecord{
		MessageID:       messageID,
		SenderAccountID: s.identity.AccountID,
		Body:            body,
		Timestamp:       now,
		ExpiresAt:       s.outgoingExpiresAt(now),
	})
}

func (s *Service) SaveDefaultRoomReceived(senderAccountID, senderMailbox, messageID, body string, timestamp, expiresAt time.Time) error {
	return s.store.AppendRoomHistory(s.identity, DefaultRoomID, store.RoomMessageRecord{
		MessageID:       messageID,
		SenderAccountID: senderAccountID,
		SenderMailbox:   senderMailbox,
		Body:            body,
		Timestamp:       timestamp,
		ExpiresAt:       expiresAt,
	})
}

func DefaultRoomLabel() string {
	return "#" + DefaultRoomName
}
