package store

import (
	"os"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
)

func TestPurgeExpiredDropsExpiredMessages(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}

	now := time.Now().UTC().Round(time.Second)
	records := []MessageRecord{
		{MessageID: "keep-no-expiry", PeerMailbox: "bob", Direction: "outbound", Body: "legacy", Timestamp: now.Add(-3 * time.Hour)},
		{MessageID: "keep-future", PeerMailbox: "bob", Direction: "outbound", Body: "fresh", Timestamp: now.Add(-1 * time.Hour), ExpiresAt: now.Add(1 * time.Hour)},
		{MessageID: "drop-expired", PeerMailbox: "bob", Direction: "outbound", Body: "stale", Timestamp: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-1 * time.Hour)},
	}
	for _, record := range records {
		if err := clientStore.AppendHistory(id, record); err != nil {
			t.Fatalf("append history: %v", err)
		}
	}

	if err := clientStore.PurgeExpired(id, now); err != nil {
		t.Fatalf("purge: %v", err)
	}

	loaded, err := clientStore.LoadHistory(id, "bob")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 surviving records, got %d: %+v", len(loaded), loaded)
	}
	for _, rec := range loaded {
		if rec.MessageID == "drop-expired" {
			t.Fatalf("expired record not removed: %+v", rec)
		}
	}
}

func TestPurgeExpiredSkipsUnchangedFiles(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	now := time.Now().UTC().Round(time.Second)

	if err := clientStore.AppendHistory(id, MessageRecord{PeerMailbox: "bob", Direction: "outbound", Body: "hi", Timestamp: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("append history: %v", err)
	}

	path, err := clientStore.historyPath("bob")
	if err != nil {
		t.Fatalf("history path: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// Sleep long enough that an inadvertent rewrite would bump mtime past
	// filesystem granularity.
	time.Sleep(10 * time.Millisecond)

	if err := clientStore.PurgeExpired(id, now); err != nil {
		t.Fatalf("purge: %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("expected no rewrite when nothing expired; before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

func TestPurgeExpiredHandlesRoomHistory(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	now := time.Now().UTC().Round(time.Second)

	records := []RoomMessageRecord{
		{MessageID: "msg-a", SenderAccountID: "alice", Body: "fresh", Timestamp: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		{MessageID: "msg-b", SenderAccountID: "bob", Body: "stale", Timestamp: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)},
	}
	if _, err := clientStore.MergeRoomHistory(id, "general", records); err != nil {
		t.Fatalf("merge room history: %v", err)
	}

	if err := clientStore.PurgeExpired(id, now); err != nil {
		t.Fatalf("purge: %v", err)
	}

	loaded, err := clientStore.LoadRoomHistory(id, "general")
	if err != nil {
		t.Fatalf("load room history: %v", err)
	}
	if len(loaded) != 1 || loaded[0].MessageID != "msg-a" {
		t.Fatalf("expected only msg-a to survive, got %+v", loaded)
	}
}

func TestLoadHistoryHidesExpiredWithoutRewrite(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	now := time.Now().UTC().Round(time.Second)

	if err := clientStore.AppendHistory(id, MessageRecord{PeerMailbox: "bob", Direction: "outbound", Body: "stale", Timestamp: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}); err != nil {
		t.Fatalf("append history: %v", err)
	}
	if err := clientStore.AppendHistory(id, MessageRecord{PeerMailbox: "bob", Direction: "outbound", Body: "fresh", Timestamp: now.Add(-30 * time.Minute), ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("append history: %v", err)
	}

	loaded, err := clientStore.LoadHistory(id, "bob")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Body != "fresh" {
		t.Fatalf("expected only the fresh record, got %+v", loaded)
	}
}
