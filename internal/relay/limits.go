package relay

import (
	"fmt"
	"sync"
	"time"

	"github.com/elpdev/pando/internal/protocol"
)

type Options struct {
	QueueTTL           time.Duration
	MaxMessageBytes    int
	MaxQueuedMessages  int
	MaxQueuedBytes     int
	RateLimitPerMinute int
	AuthToken          string
	AllowedOrigins     []string
	LandingPage        bool
}

type QueueLimits struct {
	MaxMessages int
	MaxBytes    int
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
	payloadBytes := envelopeSize(envelope)
	if payloadBytes > options.MaxMessageBytes {
		return fmt.Errorf("message exceeds relay size limit of %d bytes", options.MaxMessageBytes)
	}
	return nil
}

func envelopeSize(envelope protocol.Envelope) int {
	return len(envelope.Body) + len(envelope.Ciphertext) + len(envelope.Nonce) + len(envelope.Signature)
}
