package relay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/elpdev/chatui/internal/protocol"
	bbolt "go.etcd.io/bbolt"
)

var queueBucket = []byte("mailboxes")

type QueueStore interface {
	Enqueue(protocol.Envelope) error
	Drain(mailbox string) ([]protocol.Envelope, error)
	Close() error
}

type MemoryQueueStore struct {
	mu        sync.Mutex
	mailboxes map[string][]protocol.Envelope
}

func NewMemoryQueueStore() *MemoryQueueStore {
	return &MemoryQueueStore{mailboxes: make(map[string][]protocol.Envelope)}
}

func (s *MemoryQueueStore) Enqueue(envelope protocol.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mailboxes[envelope.RecipientMailbox] = append(s.mailboxes[envelope.RecipientMailbox], envelope)
	return nil
}

func (s *MemoryQueueStore) Drain(mailbox string) ([]protocol.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	backlog := filterExpired(append([]protocol.Envelope(nil), s.mailboxes[mailbox]...), time.Now().UTC())
	delete(s.mailboxes, mailbox)
	return backlog, nil
}

func (s *MemoryQueueStore) Close() error {
	return nil
}

type BoltQueueStore struct {
	db *bbolt.DB
}

func NewBoltQueueStore(path string) (*BoltQueueStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create relay store directory: %w", err)
	}
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open relay store: %w", err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(queueBucket)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize relay store: %w", err)
	}
	return &BoltQueueStore{db: db}, nil
}

func (s *BoltQueueStore) Enqueue(envelope protocol.Envelope) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		key := []byte(envelope.RecipientMailbox)
		var queue []protocol.Envelope
		if current := bucket.Get(key); len(current) != 0 {
			if err := json.Unmarshal(current, &queue); err != nil {
				return fmt.Errorf("decode queue: %w", err)
			}
		}
		queue = filterExpired(queue, time.Now().UTC())
		queue = append(queue, envelope)
		bytes, err := json.Marshal(queue)
		if err != nil {
			return fmt.Errorf("encode queue: %w", err)
		}
		return bucket.Put(key, bytes)
	})
}

func (s *BoltQueueStore) Drain(mailbox string) ([]protocol.Envelope, error) {
	var backlog []protocol.Envelope
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		key := []byte(mailbox)
		current := bucket.Get(key)
		if len(current) == 0 {
			return nil
		}
		if err := json.Unmarshal(current, &backlog); err != nil {
			return fmt.Errorf("decode queue: %w", err)
		}
		backlog = filterExpired(backlog, time.Now().UTC())
		if len(backlog) == 0 {
			return bucket.Delete(key)
		}
		return bucket.Delete(key)
	})
	if err != nil {
		return nil, err
	}
	return backlog, nil
}

func (s *BoltQueueStore) Close() error {
	return s.db.Close()
}

func filterExpired(queue []protocol.Envelope, now time.Time) []protocol.Envelope {
	filtered := queue[:0]
	for _, envelope := range queue {
		if !envelope.ExpiresAt.IsZero() && !envelope.ExpiresAt.After(now) {
			continue
		}
		filtered = append(filtered, envelope)
	}
	return filtered
}
