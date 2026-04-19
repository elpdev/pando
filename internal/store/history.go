package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
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
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read history: %w", err)
	}
	if len(bytes) < 12 {
		return nil, fmt.Errorf("history file is too short")
	}
	plaintext, err := decryptStorePayload(id, bytes)
	if err != nil {
		return nil, fmt.Errorf("decrypt history: %w", err)
	}
	var records []MessageRecord
	if err := json.Unmarshal(plaintext, &records); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
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
	plaintext, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	sealed, err := encryptStorePayload(id, plaintext)
	if err != nil {
		return fmt.Errorf("encrypt history: %w", err)
	}
	path, err := s.historyPath(record.PeerMailbox)
	if err != nil {
		return err
	}
	return os.WriteFile(path, sealed, 0o600)
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
	plaintext, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	sealed, err := encryptStorePayload(id, plaintext)
	if err != nil {
		return fmt.Errorf("encrypt history: %w", err)
	}
	path, err := s.historyPath(peerMailbox)
	if err != nil {
		return err
	}
	return os.WriteFile(path, sealed, 0o600)
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

func encryptStorePayload(id *identity.Identity, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(historyKey(id))
	if err != nil {
		return nil, fmt.Errorf("create store cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create store AEAD: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate store nonce: %w", err)
	}
	return append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...), nil
}

func decryptStorePayload(id *identity.Identity, bytes []byte) ([]byte, error) {
	block, err := aes.NewCipher(historyKey(id))
	if err != nil {
		return nil, fmt.Errorf("create store cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create store AEAD: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(bytes) < nonceSize {
		return nil, fmt.Errorf("store payload is missing nonce")
	}
	plaintext, err := gcm.Open(nil, bytes[:nonceSize], bytes[nonceSize:], nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}
