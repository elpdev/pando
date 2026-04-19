package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	bbolt "go.etcd.io/bbolt"
)

type BoltQueueStore struct {
	db     *bbolt.DB
	limits QueueLimits
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
		for _, bucket := range [][]byte{queueBucket, mailboxClaimBucket, mailboxDirectoryBucket, directoryBucket, rendezvousBucket} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize relay store: %w", err)
	}
	return &BoltQueueStore{db: db}, nil
}

func (s *BoltQueueStore) SetLimits(limits QueueLimits) {
	s.limits = limits
}

func (s *BoltQueueStore) Enqueue(envelope protocol.Envelope) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		key := []byte(envelope.RecipientMailbox)

		var queue []protocol.Envelope
		_, err := boltGet(bucket, key, &queue)
		if err != nil {
			return fmt.Errorf("decode queue: %w", err)
		}
		queue = filterExpired(queue, time.Now().UTC())
		if err := validateQueueLimits(queue, envelope, s.limits); err != nil {
			return err
		}
		return boltPut(bucket, key, append(queue, envelope))
	})
}

func (s *BoltQueueStore) Drain(mailbox string) ([]protocol.Envelope, error) {
	var backlog []protocol.Envelope
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		key := []byte(mailbox)

		found, err := boltGet(bucket, key, &backlog)
		if err != nil {
			return fmt.Errorf("decode queue: %w", err)
		}
		if !found {
			return nil
		}
		backlog = filterExpired(backlog, time.Now().UTC())
		return bucket.Delete(key)
	})
	if err != nil {
		return nil, err
	}
	return backlog, nil
}

func (s *BoltQueueStore) AuthorizeMailbox(mailbox string, signingPublic []byte) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(mailboxClaimBucket)
		key := []byte(mailbox)

		existing := bucket.Get(key)
		if len(existing) == 0 {
			return bucket.Put(key, append([]byte(nil), signingPublic...))
		}
		if bytes.Equal(existing, signingPublic) {
			return nil
		}
		return ErrMailboxClaimConflict
	})
}

func (s *BoltQueueStore) Close() error {
	return s.db.Close()
}

func (s *BoltQueueStore) PutDirectoryEntry(entry relayapi.SignedDirectoryEntry) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(directoryBucket)
		key := []byte(entry.Entry.Mailbox)

		var current relayapi.SignedDirectoryEntry
		found, err := boltGet(bucket, key, &current)
		if err != nil {
			return fmt.Errorf("decode directory entry: %w", err)
		}
		if found && !bytes.Equal(current.Entry.Bundle.AccountSigningPublic, entry.Entry.Bundle.AccountSigningPublic) {
			return ErrDirectoryConflict
		}
		if err := boltMailboxOwnerSync(tx).sync(current, entry); err != nil {
			return err
		}
		return boltPut(bucket, key, entry)
	})
}

func (s *BoltQueueStore) GetDirectoryEntry(mailbox string) (*relayapi.SignedDirectoryEntry, error) {
	var entry relayapi.SignedDirectoryEntry
	err := s.db.View(func(tx *bbolt.Tx) error {
		found, err := boltGet(tx.Bucket(directoryBucket), []byte(mailbox), &entry)
		if err != nil {
			return fmt.Errorf("decode directory entry: %w", err)
		}
		if !found {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (s *BoltQueueStore) LookupMailboxAccount(mailbox string) (string, error) {
	var account string
	err := s.db.View(func(tx *bbolt.Tx) error {
		value := tx.Bucket(mailboxDirectoryBucket).Get([]byte(mailbox))
		if len(value) == 0 {
			return ErrNotFound
		}
		account = string(value)
		return nil
	})
	if err != nil {
		return "", err
	}
	return account, nil
}

func (s *BoltQueueStore) LookupDirectoryEntryByDeviceMailbox(mailbox string) (*relayapi.SignedDirectoryEntry, error) {
	account, err := s.LookupMailboxAccount(mailbox)
	if err != nil {
		return nil, err
	}
	return s.GetDirectoryEntry(account)
}

func (s *BoltQueueStore) ListDiscoverableEntries() ([]relayapi.SignedDirectoryEntry, error) {
	entries := make([]relayapi.SignedDirectoryEntry, 0)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(directoryBucket).ForEach(func(_, value []byte) error {
			var entry relayapi.SignedDirectoryEntry
			if err := json.Unmarshal(value, &entry); err != nil {
				return fmt.Errorf("decode directory entry: %w", err)
			}
			if !entry.Entry.Discoverable {
				return nil
			}
			entries = append(entries, entry)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Entry.Mailbox < entries[j].Entry.Mailbox
	})
	return entries, nil
}

func (s *BoltQueueStore) PutRendezvousPayload(id string, payload relayapi.RendezvousPayload) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(rendezvousBucket)
		key := []byte(id)

		var payloads []relayapi.RendezvousPayload
		_, err := boltGet(bucket, key, &payloads)
		if err != nil {
			return fmt.Errorf("decode rendezvous payloads: %w", err)
		}
		payloads = append(filterExpiredRendezvous(payloads, time.Now().UTC()), payload)
		return boltPut(bucket, key, payloads)
	})
}

func (s *BoltQueueStore) GetRendezvousPayloads(id string, now time.Time) ([]relayapi.RendezvousPayload, error) {
	var payloads []relayapi.RendezvousPayload
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(rendezvousBucket)
		key := []byte(id)

		found, err := boltGet(bucket, key, &payloads)
		if err != nil {
			return fmt.Errorf("decode rendezvous payloads: %w", err)
		}
		if !found {
			return nil
		}
		// Keep eager TTL cleanup on reads so stale payloads don't occupy the slot until the next write.
		payloads = filterExpiredRendezvous(payloads, now)
		if len(payloads) == 0 {
			return bucket.Delete(key)
		}
		return boltPut(bucket, key, payloads)
	})
	if err != nil {
		return nil, err
	}
	return payloads, nil
}

func (s *BoltQueueStore) DeleteRendezvous(id string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(rendezvousBucket).Delete([]byte(id))
	})
}
