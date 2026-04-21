package store

import (
	"strings"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
)

func TestEncryptedHistoryRoundTrip(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	record := MessageRecord{PeerMailbox: "bob", Direction: "outbound", Body: "hello bob", Timestamp: time.Now().UTC().Round(time.Second)}
	if err := clientStore.AppendHistory(id, record); err != nil {
		t.Fatalf("append history: %v", err)
	}

	records, err := clientStore.LoadHistory(id, "bob")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	if records[0].Body != record.Body || records[0].Direction != record.Direction {
		t.Fatalf("unexpected record: %+v", records[0])
	}
}

func TestAppendHistoryRejectsPathTraversalMailbox(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	err = clientStore.AppendHistory(id, MessageRecord{PeerMailbox: "../bob", Direction: "outbound", Body: "hello", Timestamp: time.Now().UTC()})
	if err == nil {
		t.Fatal("expected traversal mailbox to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid mailbox") {
		t.Fatalf("expected invalid mailbox error, got %v", err)
	}
}

func TestEncryptedRoomRoundTrip(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	state := &RoomState{
		ID:       "default",
		Name:     "general",
		Joined:   true,
		JoinedAt: time.Now().UTC().Round(time.Second),
		Members:  []RoomMember{{AccountID: "alice", JoinedAt: time.Now().UTC().Round(time.Second)}},
	}
	if err := clientStore.SaveRoomState(id, state); err != nil {
		t.Fatalf("save room state: %v", err)
	}
	if err := clientStore.AppendRoomHistory(id, "default", RoomMessageRecord{MessageID: "msg-1", SenderAccountID: "alice", Body: "hello room", Timestamp: time.Now().UTC().Round(time.Second)}); err != nil {
		t.Fatalf("append room history: %v", err)
	}

	loadedState, err := clientStore.LoadRoomState(id, "default")
	if err != nil {
		t.Fatalf("load room state: %v", err)
	}
	if !loadedState.Joined || len(loadedState.Members) != 1 || loadedState.Members[0].AccountID != "alice" {
		t.Fatalf("unexpected room state: %+v", loadedState)
	}
	records, err := clientStore.LoadRoomHistory(id, "default")
	if err != nil {
		t.Fatalf("load room history: %v", err)
	}
	if len(records) != 1 || records[0].Body != "hello room" {
		t.Fatalf("unexpected room history: %+v", records)
	}
}
