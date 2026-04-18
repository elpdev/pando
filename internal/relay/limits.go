package relay

import (
	"fmt"
	"sync"
	"time"

	"github.com/elpdev/chatui/internal/protocol"
)

type Options struct {
	QueueTTL           time.Duration
	MaxMessageBytes    int
	RateLimitPerMinute int
}

type rateLimiter struct {
	mu      sync.Mutex
	windows map[string]rateWindow
	limit   int
}

type rateWindow struct {
	startedAt time.Time
	count     int
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{windows: make(map[string]rateWindow), limit: limit}
}

func (l *rateLimiter) Allow(mailbox string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	window := l.windows[mailbox]
	if window.startedAt.IsZero() || now.Sub(window.startedAt) >= time.Minute {
		window = rateWindow{startedAt: now, count: 0}
	}
	if window.count >= l.limit {
		l.windows[mailbox] = window
		return false
	}
	window.count++
	l.windows[mailbox] = window
	return true
}

func validateEnvelopeLimits(envelope protocol.Envelope, options Options) error {
	payloadBytes := len(envelope.Body) + len(envelope.Ciphertext) + len(envelope.Nonce) + len(envelope.Signature)
	if payloadBytes > options.MaxMessageBytes {
		return fmt.Errorf("message exceeds relay size limit of %d bytes", options.MaxMessageBytes)
	}
	return nil
}
