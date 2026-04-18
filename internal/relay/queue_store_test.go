package relay

import (
	"path/filepath"
	"testing"

	"github.com/elpdev/pando/internal/protocol"
)

func TestBoltQueueStorePersistsBacklog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.db")
	store, err := NewBoltQueueStore(path)
	if err != nil {
		t.Fatalf("new bolt queue store: %v", err)
	}
	if err := store.Enqueue(protocol.Envelope{RecipientMailbox: "bob", SenderMailbox: "alice", Body: "persisted"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := NewBoltQueueStore(path)
	if err != nil {
		t.Fatalf("reopen bolt queue store: %v", err)
	}
	defer reopened.Close()

	backlog, err := reopened.Drain("bob")
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(backlog) != 1 || backlog[0].Body != "persisted" {
		t.Fatalf("unexpected backlog: %+v", backlog)
	}

	backlog, err = reopened.Drain("bob")
	if err != nil {
		t.Fatalf("drain empty: %v", err)
	}
	if len(backlog) != 0 {
		t.Fatalf("expected queue to be empty, got %+v", backlog)
	}
}
