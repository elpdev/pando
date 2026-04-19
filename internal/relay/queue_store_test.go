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

func TestMemoryQueueStoreRejectsMailboxWhenQueueLimitExceeded(t *testing.T) {
	store := NewMemoryQueueStore()
	store.SetLimits(QueueLimits{MaxMessages: 1, MaxBytes: 1024})
	if err := store.Enqueue(protocol.Envelope{RecipientMailbox: "bob", SenderMailbox: "alice", Body: "first"}); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	if err := store.Enqueue(protocol.Envelope{RecipientMailbox: "bob", SenderMailbox: "alice", Body: "second"}); err != ErrQueueFull {
		t.Fatalf("expected queue full error, got %v", err)
	}
}

func TestBoltQueueStorePersistsMailboxClaims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.db")
	store, err := NewBoltQueueStore(path)
	if err != nil {
		t.Fatalf("new bolt queue store: %v", err)
	}
	if err := store.ClaimMailbox("bob", []byte("pubkey-1")); err != nil {
		t.Fatalf("claim mailbox: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := NewBoltQueueStore(path)
	if err != nil {
		t.Fatalf("reopen bolt queue store: %v", err)
	}
	defer reopened.Close()
	owner, err := reopened.MailboxOwner("bob")
	if err != nil {
		t.Fatalf("mailbox owner: %v", err)
	}
	if string(owner) != "pubkey-1" {
		t.Fatalf("expected persisted owner, got %q", string(owner))
	}
	if err := reopened.ClaimMailbox("bob", []byte("pubkey-2")); err != ErrMailboxClaimConflict {
		t.Fatalf("expected mailbox claim conflict, got %v", err)
	}
}
