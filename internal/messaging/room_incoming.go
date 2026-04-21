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
		now := time.Now().UTC()
		members := mergeRoomMembers(state.Members, store.RoomMember{AccountID: contact.AccountID, JoinedAt: now})
		if len(members) > DefaultRoomCap {
			return &IncomingResult{Control: true}, nil
		}
		state.Members = members
		state.UpdatedAt = now
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

func (s *Service) handleRoomHistoryRequest(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	if payload == nil || payload.RoomHistoryRequest == nil {
		return nil, fmt.Errorf("room history request payload is required")
	}
	request := payload.RoomHistoryRequest
	if request.RoomID != DefaultRoomID {
		return &IncomingResult{Control: true}, nil
	}
	if request.RequestID == "" {
		return nil, fmt.Errorf("room history request id is required")
	}
	state, err := s.DefaultRoomState()
	if err != nil {
		return nil, err
	}
	if !state.Joined {
		return &IncomingResult{Control: true}, nil
	}
	requesterJoinAt, ok := roomMemberJoinedAt(state.Members, contact.AccountID)
	if !ok {
		return &IncomingResult{Control: true}, nil
	}
	now := time.Now().UTC()
	since := now.Add(-roomHistoryMaxAge)
	if requesterJoinAt.After(since) {
		since = requesterJoinAt
	}
	if request.Since.After(since) {
		since = request.Since
	}
	until := now
	if !request.Until.IsZero() && request.Until.Before(until) {
		until = request.Until
	}
	if until.Before(since) {
		until = since
	}
	records, err := s.store.LoadRoomHistoryWindow(s.identity, DefaultRoomID, since, until)
	if err != nil {
		return nil, err
	}
	envelopes := make([]protocol.Envelope, 0)
	if len(records) == 0 {
		chunkPayload, err := json.Marshal(contentPayload{
			Kind:             contentKindRoomHistoryChunk,
			RoomHistoryChunk: &roomHistoryChunk{RoomID: DefaultRoomID, RequestID: request.RequestID, Last: true},
		})
		if err != nil {
			return nil, fmt.Errorf("encode room history chunk: %w", err)
		}
		envelopes, err = session.Encrypt(s.identity, contact, string(chunkPayload))
		if err != nil {
			return nil, err
		}
		return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, AckEnvelopes: envelopes}, nil
	}
	for start := 0; start < len(records); start += roomHistoryChunkSize {
		end := start + roomHistoryChunkSize
		if end > len(records) {
			end = len(records)
		}
		chunkPayload, err := json.Marshal(contentPayload{
			Kind: contentKindRoomHistoryChunk,
			RoomHistoryChunk: &roomHistoryChunk{
				RoomID:    DefaultRoomID,
				RequestID: request.RequestID,
				Messages:  roomHistoryMessages(records[start:end]),
				Last:      end == len(records),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("encode room history chunk: %w", err)
		}
		chunkEnvelopes, err := session.Encrypt(s.identity, contact, string(chunkPayload))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, chunkEnvelopes...)
	}
	return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, AckEnvelopes: envelopes}, nil
}

func (s *Service) handleRoomHistoryChunk(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	if payload == nil || payload.RoomHistoryChunk == nil {
		return nil, fmt.Errorf("room history chunk payload is required")
	}
	chunk := payload.RoomHistoryChunk
	if chunk.RoomID != DefaultRoomID {
		return &IncomingResult{Control: true}, nil
	}
	if chunk.RequestID == "" {
		return nil, fmt.Errorf("room history chunk request id is required")
	}
	state, err := s.DefaultRoomState()
	if err != nil {
		return nil, err
	}
	if !state.Joined || !roomHasMember(state.Members, contact.AccountID) {
		return &IncomingResult{Control: true}, nil
	}
	now := time.Now().UTC()
	allowedSince := now.Add(-roomHistoryMaxAge)
	if state.JoinedAt.After(allowedSince) {
		allowedSince = state.JoinedAt
	}
	allowedUntil := now
	records := make([]store.RoomMessageRecord, 0, len(chunk.Messages))
	for _, message := range chunk.Messages {
		if message.MessageID == "" || message.SenderAccountID == "" || message.Timestamp.IsZero() {
			continue
		}
		if message.Timestamp.Before(allowedSince) || message.Timestamp.After(allowedUntil) {
			continue
		}
		if !roomHasMember(state.Members, message.SenderAccountID) {
			continue
		}
		records = append(records, store.RoomMessageRecord{
			MessageID:       message.MessageID,
			SenderAccountID: message.SenderAccountID,
			SenderMailbox:   message.SenderMailbox,
			Body:            message.Body,
			Timestamp:       message.Timestamp,
		})
	}
	added := 0
	if len(records) != 0 {
		added, err = s.store.MergeRoomHistory(s.identity, DefaultRoomID, records)
		if err != nil {
			return nil, err
		}
	}
	return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, RoomID: DefaultRoomID, RoomSync: &RoomSyncUpdate{RequestID: chunk.RequestID, Added: added, Complete: chunk.Last}}, nil
}
