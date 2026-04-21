package store

import (
	"fmt"
	"sort"
	"time"

	"github.com/elpdev/pando/internal/identity"
)

type RoomMember struct {
	AccountID string    `json:"account_id"`
	JoinedAt  time.Time `json:"joined_at"`
}

type RoomState struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Joined    bool         `json:"joined"`
	JoinedAt  time.Time    `json:"joined_at,omitempty"`
	Members   []RoomMember `json:"members,omitempty"`
	UpdatedAt time.Time    `json:"updated_at,omitempty"`
}

type RoomMessageRecord struct {
	MessageID       string    `json:"message_id,omitempty"`
	SenderAccountID string    `json:"sender_account_id"`
	SenderMailbox   string    `json:"sender_mailbox,omitempty"`
	Body            string    `json:"body"`
	Timestamp       time.Time `json:"timestamp"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
}

func (s *ClientStore) LoadRoomState(id *identity.Identity, roomID string) (*RoomState, error) {
	path, err := s.roomStatePath(roomID)
	if err != nil {
		return nil, err
	}
	var state RoomState
	if err := readEncryptedJSON(id, path, &state, "read room state", "decrypt room state", "decode room state"); err != nil {
		if err == ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	state.normalizeMembers()
	copyState := state
	copyState.Members = append([]RoomMember(nil), state.Members...)
	return &copyState, nil
}

func (s *ClientStore) SaveRoomState(id *identity.Identity, state *RoomState) error {
	if state == nil {
		return fmt.Errorf("room state is required")
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	copyState := *state
	copyState.normalizeMembers()
	path, err := s.roomStatePath(copyState.ID)
	if err != nil {
		return err
	}
	return writeEncryptedJSON(id, path, copyState, "encode room state", "encrypt room state", "write room state", true)
}

func (s *ClientStore) LoadRoomHistory(id *identity.Identity, roomID string) ([]RoomMessageRecord, error) {
	path, err := s.roomHistoryPath(roomID)
	if err != nil {
		return nil, err
	}
	var records []RoomMessageRecord
	if err := readEncryptedJSON(id, path, &records, "read room history", "decrypt room history", "decode room history"); err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return filterExpiredRoomMessages(records, time.Now()), nil
}

func filterExpiredRoomMessages(records []RoomMessageRecord, now time.Time) []RoomMessageRecord {
	kept := records[:0]
	for _, record := range records {
		if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now) {
			continue
		}
		kept = append(kept, record)
	}
	return kept
}

func (s *ClientStore) AppendRoomHistory(id *identity.Identity, roomID string, record RoomMessageRecord) error {
	_, err := s.MergeRoomHistory(id, roomID, []RoomMessageRecord{record})
	return err
}

func (s *ClientStore) MergeRoomHistory(id *identity.Identity, roomID string, incoming []RoomMessageRecord) (int, error) {
	if err := s.Ensure(); err != nil {
		return 0, err
	}
	records, err := s.LoadRoomHistory(id, roomID)
	if err != nil {
		return 0, err
	}
	seen := make(map[string]struct{}, len(records))
	for _, existing := range records {
		if existing.MessageID == "" {
			continue
		}
		seen[existing.MessageID] = struct{}{}
	}
	added := 0
	for _, record := range incoming {
		if record.MessageID != "" {
			if _, ok := seen[record.MessageID]; ok {
				continue
			}
			seen[record.MessageID] = struct{}{}
		}
		records = append(records, record)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Timestamp.Equal(records[j].Timestamp) {
			return records[i].MessageID < records[j].MessageID
		}
		return records[i].Timestamp.Before(records[j].Timestamp)
	})
	path, err := s.roomHistoryPath(roomID)
	if err != nil {
		return 0, err
	}
	if err := writeEncryptedJSON(id, path, records, "encode room history", "encrypt room history", "write room history", true); err != nil {
		return 0, err
	}
	return added, nil
}

func (s *ClientStore) LoadRoomHistoryWindow(id *identity.Identity, roomID string, since, until time.Time) ([]RoomMessageRecord, error) {
	records, err := s.LoadRoomHistory(id, roomID)
	if err != nil {
		return nil, err
	}
	window := make([]RoomMessageRecord, 0, len(records))
	for _, record := range records {
		if !since.IsZero() && record.Timestamp.Before(since) {
			continue
		}
		if !until.IsZero() && record.Timestamp.After(until) {
			continue
		}
		window = append(window, record)
	}
	return window, nil
}

func (s *ClientStore) roomStatePath(roomID string) (string, error) {
	sanitized, err := sanitizeStoreRoomID(roomID)
	if err != nil {
		return "", err
	}
	return joinStorePath(s.dir, "room-state-"+sanitized+".enc"), nil
}

func (s *ClientStore) roomHistoryPath(roomID string) (string, error) {
	sanitized, err := sanitizeStoreRoomID(roomID)
	if err != nil {
		return "", err
	}
	return joinStorePath(s.dir, "room-history-"+sanitized+".enc"), nil
}

func (r *RoomState) normalizeMembers() {
	if r == nil {
		return
	}
	seen := make(map[string]RoomMember, len(r.Members))
	for _, member := range r.Members {
		if member.AccountID == "" {
			continue
		}
		existing, ok := seen[member.AccountID]
		if !ok || existing.JoinedAt.IsZero() || (!member.JoinedAt.IsZero() && member.JoinedAt.Before(existing.JoinedAt)) {
			seen[member.AccountID] = member
		}
	}
	r.Members = r.Members[:0]
	for _, member := range seen {
		r.Members = append(r.Members, member)
	}
	sort.Slice(r.Members, func(i, j int) bool {
		if r.Members[i].JoinedAt.Equal(r.Members[j].JoinedAt) {
			return r.Members[i].AccountID < r.Members[j].AccountID
		}
		if r.Members[i].JoinedAt.IsZero() {
			return false
		}
		if r.Members[j].JoinedAt.IsZero() {
			return true
		}
		return r.Members[i].JoinedAt.Before(r.Members[j].JoinedAt)
	})
}
