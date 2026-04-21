package rendezvous

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/relayapi"
)

// DefaultPollInterval is how often Exchange polls the relay when PollEvery
// is zero. It matches the prior ctlcmd cadence so existing timing expectations
// (roughly one round-trip per second) are preserved.
const DefaultPollInterval = 750 * time.Millisecond

// ErrTimedOut is returned when the provided context deadline elapses before a
// peer payload is received. Separate from ctx.Err() so callers can distinguish
// "the user cancelled" from "the wait window elapsed without a match".
var ErrTimedOut = errors.New("timed out waiting for the other person to complete the invite exchange")

// RelayClient is the minimum surface Exchange needs from a relay client.
// Kept small on purpose so tests can supply an in-memory fake without pulling
// in the HTTP client.
type RelayClient interface {
	PutRendezvousPayload(id string, p relayapi.RendezvousPayload) error
	GetRendezvousPayloads(id string) ([]relayapi.RendezvousPayload, error)
}

// PollConfig parameterises a single invite-code exchange.
type PollConfig struct {
	Client        RelayClient
	Code          string
	Self          identity.InviteBundle
	SelfAccountID string
	PollEvery     time.Duration
}

// Exchange uploads the local invite bundle to the rendezvous slot derived
// from the code and polls for a peer payload. It returns as soon as it
// decrypts a payload authored by a different account, or when ctx is
// cancelled / times out.
//
// Cancellation only takes effect between polls — if a GetRendezvousPayloads
// request is in flight, ctx.Done() won't interrupt it (relayapi.Client lacks
// ctx plumbing). Worst-case latency on Esc is the HTTP client timeout (~15s).
func Exchange(ctx context.Context, cfg PollConfig) (*identity.InviteBundle, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("rendezvous: relay client is required")
	}
	if cfg.Code == "" {
		return nil, fmt.Errorf("rendezvous: code is required")
	}
	pollEvery := cfg.PollEvery
	if pollEvery <= 0 {
		pollEvery = DefaultPollInterval
	}
	id := DeriveID(cfg.Code)
	payload, err := EncryptBundle(cfg.Code, cfg.Self)
	if err != nil {
		return nil, err
	}
	if err := cfg.Client.PutRendezvousPayload(id, payload); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, ErrTimedOut
			}
			return nil, err
		}
		payloads, err := cfg.Client.GetRendezvousPayloads(id)
		if err != nil {
			return nil, err
		}
		for _, candidate := range payloads {
			bundle, err := DecryptBundle(cfg.Code, candidate)
			if err != nil {
				continue
			}
			if bundle.AccountID == cfg.SelfAccountID {
				continue
			}
			return bundle, nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, ErrTimedOut
			}
			return nil, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}
