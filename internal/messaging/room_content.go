package messaging

import "time"

const (
	contentKindRoomMessage        = "room-message"
	contentKindRoomMembership     = "room-membership"
	contentKindRoomHistoryRequest = "room-history-request"
	contentKindRoomHistoryChunk   = "room-history-chunk"
)

type roomMessage struct {
	RoomID          string `json:"room_id"`
	MessageID       string `json:"message_id"`
	SenderAccountID string `json:"sender_account_id"`
	Body            string `json:"body"`
}

type roomMember struct {
	AccountID string    `json:"account_id"`
	JoinedAt  time.Time `json:"joined_at"`
}

type roomMembership struct {
	RoomID    string       `json:"room_id"`
	UpdatedAt time.Time    `json:"updated_at,omitempty"`
	Members   []roomMember `json:"members,omitempty"`
}

type roomHistoryRequest struct {
	RoomID    string    `json:"room_id"`
	RequestID string    `json:"request_id"`
	Since     time.Time `json:"since,omitempty"`
	Until     time.Time `json:"until,omitempty"`
}

type roomHistoryMessage struct {
	MessageID       string    `json:"message_id"`
	SenderAccountID string    `json:"sender_account_id"`
	SenderMailbox   string    `json:"sender_mailbox,omitempty"`
	Body            string    `json:"body"`
	Timestamp       time.Time `json:"timestamp"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
}

type roomHistoryChunk struct {
	RoomID    string               `json:"room_id"`
	RequestID string               `json:"request_id"`
	Messages  []roomHistoryMessage `json:"messages,omitempty"`
	Last      bool                 `json:"last,omitempty"`
}
