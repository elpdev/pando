package messaging

import (
	"sort"
	"time"

	"github.com/elpdev/pando/internal/store"
)

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
			ExpiresAt:       record.ExpiresAt,
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
