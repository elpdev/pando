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
	allowedSince := time.Now().UTC().Add(-roomHistoryMaxAge)
	if state.JoinedAt.After(allowedSince) {
		allowedSince = state.JoinedAt
	}
	allowedUntil := time.Now().UTC()
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

func roomHistoryMessages(records []store.RoomMessageRecord) []roomHistoryMessage {
	messages := make([]roomHistoryMessage, 0, len(records))
	for _, record := range records {
		messages = append(messages, roomHistoryMessage{
			MessageID:       record.MessageID,
			SenderAccountID: record.SenderAccountID,
			SenderMailbox:   record.SenderMailbox,
			Body:            record.Body,
			Timestamp:       record.Timestamp,
		})
	}
	return messages
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

func roomMemberJoinedAt(members []store.RoomMember, accountID string) (time.Time, bool) {
	for _, member := range members {
		if member.AccountID == accountID {
			return member.JoinedAt, true
		}
	}
	return time.Time{}, false
}

func DefaultRoomLabel() string {
	return "#" + DefaultRoomName
}
