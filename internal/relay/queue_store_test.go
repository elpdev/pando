package relay

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
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
	if err := store.AuthorizeMailbox("bob", []byte("pubkey-1")); err != nil {
		t.Fatalf("authorize mailbox: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := NewBoltQueueStore(path)
	if err != nil {
		t.Fatalf("reopen bolt queue store: %v", err)
	}
	defer reopened.Close()
	if err := reopened.AuthorizeMailbox("bob", []byte("pubkey-1")); err != nil {
		t.Fatalf("expected persisted owner to reauthorize, got %v", err)
	}
	if err := reopened.AuthorizeMailbox("bob", []byte("pubkey-2")); err != ErrMailboxClaimConflict {
		t.Fatalf("expected mailbox claim conflict, got %v", err)
	}
}

func TestMemoryQueueStoreAuthorizeMailboxIsAtomic(t *testing.T) {
	store := NewMemoryQueueStore()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	keys := [][]byte{[]byte("pubkey-1"), []byte("pubkey-2")}
	for _, key := range keys {
		wg.Add(1)
		go func(key []byte) {
			defer wg.Done()
			errs <- store.AuthorizeMailbox("bob", key)
		}(key)
	}
	wg.Wait()
	close(errs)
	allowed := 0
	conflicts := 0
	for err := range errs {
		switch err {
		case nil:
			allowed++
		case ErrMailboxClaimConflict:
			conflicts++
		default:
			t.Fatalf("unexpected authorization result: %v", err)
		}
	}
	if allowed != 1 || conflicts != 1 {
		t.Fatalf("expected one winner and one conflict, got allowed=%d conflicts=%d", allowed, conflicts)
	}
}

func TestBoltQueueStorePublishesMailboxOwnershipFromDirectoryEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.db")
	store, err := NewBoltQueueStore(path)
	if err != nil {
		t.Fatalf("new bolt queue store: %v", err)
	}
	defer store.Close()
	id, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	signed, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{Mailbox: id.AccountID, Bundle: id.InviteBundle(), Discoverable: true, PublishedAt: time.Now().UTC(), Version: 1}, id.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	if err := store.PutDirectoryEntry(*signed); err != nil {
		t.Fatalf("put directory entry: %v", err)
	}
	device, err := id.CurrentDevice()
	if err != nil {
		t.Fatalf("current device: %v", err)
	}
	accountMailbox, err := store.LookupMailboxAccount(device.Mailbox)
	if err != nil {
		t.Fatalf("lookup mailbox account: %v", err)
	}
	if accountMailbox != id.AccountID {
		t.Fatalf("expected mailbox owner %q, got %q", id.AccountID, accountMailbox)
	}
	if err := store.AuthorizeMailbox(device.Mailbox, device.SigningPublic); err != nil {
		t.Fatalf("expected published owner to authorize, got %v", err)
	}
	entries, err := store.ListDiscoverableEntries()
	if err != nil {
		t.Fatalf("list discoverable entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Entry.Mailbox != id.AccountID {
		t.Fatalf("unexpected discoverable entries: %+v", entries)
	}
	loaded, err := store.LookupDirectoryEntryByDeviceMailbox(device.Mailbox)
	if err != nil {
		t.Fatalf("lookup directory entry by device mailbox: %v", err)
	}
	if loaded.Entry.Mailbox != id.AccountID {
		t.Fatalf("unexpected device mailbox lookup entry: %+v", loaded)
	}
}
