package store

import (
	"testing"

	"github.com/elpdev/chatui/internal/identity"
)

func TestSeenEnvelopeRoundTrip(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	seen, err := clientStore.HasSeenEnvelope(id, "msg-1")
	if err != nil {
		t.Fatalf("has seen before mark: %v", err)
	}
	if seen {
		t.Fatalf("expected message to be unseen initially")
	}
	if err := clientStore.MarkEnvelopeSeen(id, "msg-1"); err != nil {
		t.Fatalf("mark seen: %v", err)
	}
	seen, err = clientStore.HasSeenEnvelope(id, "msg-1")
	if err != nil {
		t.Fatalf("has seen after mark: %v", err)
	}
	if !seen {
		t.Fatalf("expected message to be seen after mark")
	}
}
