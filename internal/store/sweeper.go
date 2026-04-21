package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/identity"
)

// PurgeExpired removes messages whose ExpiresAt is in the past from every
// history-*.enc and room-history-*.enc file in the client's store directory.
// Files with no expired records are left untouched on disk. Records whose
// ExpiresAt is the zero value (older messages written before self-destruct
// was introduced) are always kept.
func (s *ClientStore) PurgeExpired(id *identity.Identity, now time.Time) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read store dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(s.dir, name)
		switch {
		case strings.HasPrefix(name, "history-") && strings.HasSuffix(name, ".enc"):
			if err := s.purgeDirectHistory(id, path, now); err != nil {
				return err
			}
		case strings.HasPrefix(name, "room-history-") && strings.HasSuffix(name, ".enc"):
			if err := s.purgeRoomHistory(id, path, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ClientStore) purgeDirectHistory(id *identity.Identity, path string, now time.Time) error {
	var records []MessageRecord
	if err := readEncryptedJSON(id, path, &records, "read history", "decrypt history", "decode history"); err != nil {
		if err == ErrNotFound {
			return nil
		}
		return err
	}
	kept := filterExpiredMessages(records, now)
	if len(kept) == len(records) {
		return nil
	}
	return writeEncryptedJSON(id, path, kept, "encode history", "encrypt history", "write history", true)
}

func (s *ClientStore) purgeRoomHistory(id *identity.Identity, path string, now time.Time) error {
	var records []RoomMessageRecord
	if err := readEncryptedJSON(id, path, &records, "read room history", "decrypt room history", "decode room history"); err != nil {
		if err == ErrNotFound {
			return nil
		}
		return err
	}
	kept := filterExpiredRoomMessages(records, now)
	if len(kept) == len(records) {
		return nil
	}
	return writeEncryptedJSON(id, path, kept, "encode room history", "encrypt room history", "write room history", true)
}
