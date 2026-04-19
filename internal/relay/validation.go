package relay

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/google/uuid"
)

var ErrMailboxNotPublished = errors.New("publish your signed relay directory entry before connecting")
var ErrMailboxNotAuthorized = errors.New("device is not authorized for this mailbox")

const rendezvousTTL = 10 * time.Minute

func newSubscribeChallenge(now time.Time) *protocol.SubscribeChallenge {
	return &protocol.SubscribeChallenge{Nonce: uuid.NewString(), ExpiresAt: now.Add(subscribeChallengeTTL)}
}

func subscribeErrorMessage(err error) string {
	if err == nil {
		return genericClientError
	}
	if errors.Is(err, ErrMailboxNotPublished) {
		return ErrMailboxNotPublished.Error()
	}
	if errors.Is(err, ErrMailboxNotAuthorized) {
		return ErrMailboxNotAuthorized.Error()
	}
	return genericClientError
}

func validateRendezvousPayload(payload relayapi.RendezvousPayload, now time.Time, maxBytes int) error {
	if strings.TrimSpace(payload.Ciphertext) == "" {
		return fmt.Errorf("rendezvous ciphertext is required")
	}
	if strings.TrimSpace(payload.Nonce) == "" {
		return fmt.Errorf("rendezvous nonce is required")
	}
	if payload.CreatedAt.IsZero() {
		return fmt.Errorf("rendezvous created_at is required")
	}
	if payload.ExpiresAt.IsZero() {
		return fmt.Errorf("rendezvous expires_at is required")
	}
	if payload.ExpiresAt.After(now.Add(rendezvousTTL)) {
		return fmt.Errorf("rendezvous expiry exceeds maximum TTL")
	}
	if !payload.ExpiresAt.After(now) {
		return fmt.Errorf("rendezvous payload is already expired")
	}
	if len(payload.Ciphertext)+len(payload.Nonce) > maxBytes {
		return fmt.Errorf("rendezvous payload exceeds relay size limit")
	}
	return nil
}
