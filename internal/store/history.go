package store

import (
	"crypto/sha256"
	"path/filepath"
	"time"

	"github.com/elpdev/pando/internal/identity"
)

type MessageRecord struct {
	MessageID   string    `json:"message_id,omitempty"`
	PeerMailbox string    `json:"peer_mailbox"`
	Direction   string    `json:"direction"`
	Body        string    `json:"body"`
	Delivered   bool      `json:"delivered,omitempty"`
	DeliveredAt time.Time `json:"delivered_at,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

func (s *ClientStore) LoadHistory(id *identity.Identity, peerMailbox string) ([]MessageRecord, error) {
	path, err := s.historyPath(peerMailbox)
	if err != nil {
		return nil, err
	}
	var records []MessageRecord
	if err := readEncryptedJSON(id, path, &records, "read history", "decrypt history", "decode history"); err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return records, nil
}

func (s *ClientStore) AppendHistory(id *identity.Identity, record MessageRecord) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	records, err := s.LoadHistory(id, record.PeerMailbox)
	if err != nil {
		return err
	}
	records = append(records, record)
	path, err := s.historyPath(record.PeerMailbox)
	if err != nil {
		return err
	}
	return writeEncryptedJSON(id, path, records, "encode history", "encrypt history", "write history", true)
}

func (s *ClientStore) MarkHistoryDelivered(id *identity.Identity, peerMailbox, messageID string, deliveredAt time.Time) error {
	if messageID == "" {
		return nil
	}
	records, err := s.LoadHistory(id, peerMailbox)
	if err != nil {
		return err
	}
	updated := false
	for idx := range records {
		if records[idx].Direction != "outbound" || records[idx].MessageID != messageID {
			continue
		}
		records[idx].Delivered = true
		records[idx].DeliveredAt = deliveredAt
		updated = true
	}
	if !updated {
		return nil
	}
	path, err := s.historyPath(peerMailbox)
	if err != nil {
		return err
	}
	return writeEncryptedJSON(id, path, records, "encode history", "encrypt history", "write history", true)
}

func (s *ClientStore) historyPath(peerMailbox string) (string, error) {
	sanitized, err := sanitizeStoreMailboxComponent(peerMailbox)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dir, "history-"+sanitized+".enc"), nil
}

func historyKey(id *identity.Identity) []byte {
	sum := sha256.Sum256(append([]byte("pando-history-v1"), id.AccountSigningPrivate...))
	return sum[:]
}
