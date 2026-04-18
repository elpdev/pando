package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/elpdev/chatui/internal/identity"
)

func (s *ClientStore) HasSeenEnvelope(id *identity.Identity, envelopeID string) (bool, error) {
	if envelopeID == "" {
		return false, nil
	}
	seen, err := s.loadSeenEnvelopeIDs(id)
	if err != nil {
		return false, err
	}
	_, ok := seen[envelopeID]
	return ok, nil
}

func (s *ClientStore) MarkEnvelopeSeen(id *identity.Identity, envelopeID string) error {
	if envelopeID == "" {
		return nil
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	seen, err := s.loadSeenEnvelopeIDs(id)
	if err != nil {
		return err
	}
	seen[envelopeID] = struct{}{}
	return s.writeSeenEnvelopeIDs(id, seen)
}

func (s *ClientStore) loadSeenEnvelopeIDs(id *identity.Identity) (map[string]struct{}, error) {
	bytes, err := os.ReadFile(s.seenEnvelopesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, fmt.Errorf("read seen envelopes: %w", err)
	}
	decoded, err := decryptStorePayload(id, bytes)
	if err != nil {
		return nil, fmt.Errorf("decrypt seen envelopes: %w", err)
	}
	var ids []string
	if err := json.Unmarshal(decoded, &ids); err != nil {
		return nil, fmt.Errorf("decode seen envelopes: %w", err)
	}
	seen := make(map[string]struct{}, len(ids))
	for _, envelopeID := range ids {
		seen[envelopeID] = struct{}{}
	}
	return seen, nil
}

func (s *ClientStore) writeSeenEnvelopeIDs(id *identity.Identity, seen map[string]struct{}) error {
	ids := make([]string, 0, len(seen))
	for envelopeID := range seen {
		ids = append(ids, envelopeID)
	}
	plaintext, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("encode seen envelopes: %w", err)
	}
	sealed, err := encryptStorePayload(id, plaintext)
	if err != nil {
		return fmt.Errorf("encrypt seen envelopes: %w", err)
	}
	if err := os.WriteFile(s.seenEnvelopesPath(), sealed, 0o600); err != nil {
		return fmt.Errorf("write seen envelopes: %w", err)
	}
	return nil
}

func (s *ClientStore) seenEnvelopesPath() string {
	return filepath.Join(s.dir, "seen-envelopes.enc")
}
