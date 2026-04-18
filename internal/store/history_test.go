package store

import (
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
