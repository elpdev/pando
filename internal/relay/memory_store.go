package relay

import (
	"bytes"
	"sort"
	"sync"
	"time"

	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
)

type MemoryQueueStore struct {
	mu         sync.Mutex
	mailboxes  map[string][]protocol.Envelope
	claims     map[string][]byte
	accounts   map[string]string
	directory  map[string]relayapi.SignedDirectoryEntry
	rendezvous map[string][]relayapi.RendezvousPayload
	limits     QueueLimits
}

func NewMemoryQueueStore() *MemoryQueueStore {
	return &MemoryQueueStore{
		mailboxes:  make(map[string][]protocol.Envelope),
		claims:     make(map[string][]byte),
		accounts:   make(map[string]string),
		directory:  make(map[string]relayapi.SignedDirectoryEntry),
		rendezvous: make(map[string][]relayapi.RendezvousPayload),
	}
}

func (s *MemoryQueueStore) SetLimits(limits QueueLimits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = limits
}

func (s *MemoryQueueStore) Enqueue(envelope protocol.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	queue := filterExpired(s.mailboxes[envelope.RecipientMailbox], time.Now().UTC())
	if err := validateQueueLimits(queue, envelope, s.limits); err != nil {
		return err
	}
	s.mailboxes[envelope.RecipientMailbox] = append(queue, envelope)
	return nil
}

func (s *MemoryQueueStore) Drain(mailbox string) ([]protocol.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	backlog := append([]protocol.Envelope(nil), filterExpired(s.mailboxes[mailbox], time.Now().UTC())...)
	delete(s.mailboxes, mailbox)
	return backlog, nil
}

func (s *MemoryQueueStore) Close() error {
	return nil
}

func (s *MemoryQueueStore) AuthorizeMailbox(mailbox string, signingPublic []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.claims[mailbox]
	if len(existing) == 0 {
		s.claims[mailbox] = append([]byte(nil), signingPublic...)
		return nil
	}
	if bytes.Equal(existing, signingPublic) {
		return nil
	}
	return ErrMailboxClaimConflict
}

func (s *MemoryQueueStore) PutDirectoryEntry(entry relayapi.SignedDirectoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.directory[entry.Entry.Mailbox]
	if ok && !bytes.Equal(current.Entry.Bundle.AccountSigningPublic, entry.Entry.Bundle.AccountSigningPublic) {
		return ErrDirectoryConflict
	}
	if err := memoryMailboxOwnerSync(s).sync(current, entry); err != nil {
		return err
	}
	s.directory[entry.Entry.Mailbox] = entry
	return nil
}

func (s *MemoryQueueStore) GetDirectoryEntry(mailbox string) (*relayapi.SignedDirectoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.directory[mailbox]
	if !ok {
		return nil, ErrNotFound
	}
	copyEntry := entry
	return &copyEntry, nil
}

func (s *MemoryQueueStore) LookupMailboxAccount(mailbox string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	account, ok := s.accounts[mailbox]
	if !ok {
		return "", ErrNotFound
	}
	return account, nil
}

func (s *MemoryQueueStore) LookupDirectoryEntryByDeviceMailbox(mailbox string) (*relayapi.SignedDirectoryEntry, error) {
	account, err := s.LookupMailboxAccount(mailbox)
	if err != nil {
		return nil, err
	}
	return s.GetDirectoryEntry(account)
}

func (s *MemoryQueueStore) ListDiscoverableEntries() ([]relayapi.SignedDirectoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := make([]relayapi.SignedDirectoryEntry, 0, len(s.directory))
	for _, entry := range s.directory {
		if !entry.Entry.Discoverable {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Entry.Mailbox < entries[j].Entry.Mailbox
	})
	return entries, nil
}

func (s *MemoryQueueStore) PutRendezvousPayload(id string, payload relayapi.RendezvousPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.rendezvous[id] = append(filterExpiredRendezvous(s.rendezvous[id], time.Now().UTC()), payload)
	return nil
}

func (s *MemoryQueueStore) GetRendezvousPayloads(id string, now time.Time) ([]relayapi.RendezvousPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := filterExpiredRendezvous(s.rendezvous[id], now)
	if len(filtered) == 0 {
		delete(s.rendezvous, id)
		return nil, nil
	}
	s.rendezvous[id] = filtered
	return append([]relayapi.RendezvousPayload(nil), filtered...), nil
}

func (s *MemoryQueueStore) DeleteRendezvous(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.rendezvous, id)
	return nil
}
