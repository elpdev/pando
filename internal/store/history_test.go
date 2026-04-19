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
