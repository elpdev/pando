package relay

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/elpdev/pando/internal/relayapi"
)

func (s *Server) verifyMailboxOwnership(mailbox string, signingPublic []byte) error {
	accountMailbox, err := s.queue.LookupMailboxAccount(mailbox)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w", ErrMailboxNotPublished)
		}
		return fmt.Errorf("lookup mailbox owner: %w", err)
	}
	entry, err := s.queue.GetDirectoryEntry(accountMailbox)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w", ErrMailboxNotPublished)
		}
		return fmt.Errorf("load directory entry: %w", err)
	}
	if err := relayapi.VerifySignedDirectoryEntry(*entry); err != nil {
		return fmt.Errorf("verify directory entry: %w", err)
	}
	for _, device := range entry.Entry.Bundle.Devices {
		if device.Revoked {
			continue
		}
		if device.Mailbox != mailbox {
			continue
		}
		if bytes.Equal(device.SigningPublic, signingPublic) {
			return nil
		}
	}
	return fmt.Errorf("%w", ErrMailboxNotAuthorized)
}

func (s *Server) allowRateLimit(key string, now time.Time) bool {
	return s.limiter.Allow(key, now)
}
