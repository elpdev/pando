package relay

import (
	"testing"
	"time"

	"github.com/elpdev/chatui/internal/protocol"
)

func TestValidateEnvelopeLimitsRejectsOversizedPayload(t *testing.T) {
	err := validateEnvelopeLimits(protocol.Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Body: "123456"}, Options{MaxMessageBytes: 5})
	if err == nil {
		t.Fatalf("expected oversized payload to be rejected")
	}
}

func TestRateLimiterBlocksBurstWithinWindow(t *testing.T) {
	limiter := newRateLimiter(2)
	now := time.Now().UTC()
	if !limiter.Allow("alice", now) {
		t.Fatalf("first message should be allowed")
	}
	if !limiter.Allow("alice", now.Add(10*time.Second)) {
		t.Fatalf("second message should be allowed")
	}
	if limiter.Allow("alice", now.Add(20*time.Second)) {
		t.Fatalf("third message in same minute should be rejected")
	}
	if !limiter.Allow("alice", now.Add(61*time.Second)) {
		t.Fatalf("message after window reset should be allowed")
	}
}

func TestFilterExpiredDropsExpiredEnvelopes(t *testing.T) {
	now := time.Now().UTC()
	filtered := filterExpired([]protocol.Envelope{
		{ID: "expired", ExpiresAt: now.Add(-time.Minute)},
		{ID: "fresh", ExpiresAt: now.Add(time.Minute)},
	}, now)
	if len(filtered) != 1 || filtered[0].ID != "fresh" {
		t.Fatalf("unexpected filtered queue: %+v", filtered)
	}
}
