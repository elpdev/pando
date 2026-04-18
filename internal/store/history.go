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
	"strings"
	"time"

	"github.com/elpdev/chatui/internal/identity"
)

type MessageRecord struct {
	PeerMailbox string    `json:"peer_mailbox"`
	Direction   string    `json:"direction"`
	Body        string    `json:"body"`
	Timestamp   time.Time `json:"timestamp"`
}

func (s *ClientStore) LoadHistory(id *identity.Identity, peerMailbox string) ([]MessageRecord, error) {
	bytes, err := os.ReadFile(s.historyPath(peerMailbox))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read history: %w", err)
	}
	if len(bytes) < 12 {
		return nil, fmt.Errorf("history file is too short")
	}
	block, err := aes.NewCipher(historyKey(id))
	if err != nil {
		return nil, fmt.Errorf("create history cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create history AEAD: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(bytes) < nonceSize {
		return nil, fmt.Errorf("history file is missing nonce")
	}
	plaintext, err := gcm.Open(nil, bytes[:nonceSize], bytes[nonceSize:], nil)
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
	block, err := aes.NewCipher(historyKey(id))
	if err != nil {
		return fmt.Errorf("create history cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create history AEAD: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate history nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	return os.WriteFile(s.historyPath(record.PeerMailbox), append(nonce, sealed...), 0o600)
}

func (s *ClientStore) historyPath(peerMailbox string) string {
	sanitized := strings.ReplaceAll(peerMailbox, string(os.PathSeparator), "_")
	return filepath.Join(s.dir, "history-"+sanitized+".enc")
}

func historyKey(id *identity.Identity) []byte {
	sum := sha256.Sum256(append([]byte("chatui-history-v1"), id.AccountSigningPrivate...))
	return sum[:]
}
