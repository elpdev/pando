package ws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relay"
	"github.com/elpdev/pando/internal/transport"
)

func TestConnectReturnsUnauthorizedErrorForBadHandshake(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "relay auth token is required", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient("ws"+server.URL[len("http"):], "wrong-token", "alice")
	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if !errors.Is(err, transport.ErrUnauthorized) {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestClientReceivesLargeBurstWhileSendingResponses(t *testing.T) {
	server := httptest.NewServer(relay.NewServer(testLogger(), relay.NewMemoryQueueStore(), relay.Options{}).Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	alice := NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", "alice")
	defer alice.Close()
	if err := alice.Connect(ctx); err != nil {
		t.Fatalf("connect alice: %v", err)
	}
	awaitAck(t, alice.Events())

	bob := NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", "bob")
	defer bob.Close()
	if err := bob.Connect(ctx); err != nil {
		t.Fatalf("connect bob: %v", err)
	}
	awaitAck(t, bob.Events())

	const total = 60
	for i := 0; i < total; i++ {
		if err := bob.Send(protocol.Envelope{SenderMailbox: "bob", RecipientMailbox: "alice", Body: fmt.Sprintf("chunk-%d", i)}); err != nil {
			t.Fatalf("send burst message %d: %v", i, err)
		}
	}

	received := 0
	acked := 0
	deadline := time.After(10 * time.Second)
	for received < total || acked < total {
		select {
		case event := <-bob.Events():
			if event.Err != nil {
				t.Fatalf("bob event error: %v", event.Err)
			}
			if event.Message != nil && event.Message.Type == protocol.MessageTypeAck {
				acked++
			}
		case event := <-alice.Events():
			if event.Err != nil {
				t.Fatalf("alice event error: %v", event.Err)
			}
			if event.Message == nil || event.Message.Type != protocol.MessageTypeIncoming || event.Message.Incoming == nil {
				continue
			}
			received++
			if err := alice.Send(protocol.Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Body: "ack"}); err != nil {
				t.Fatalf("alice response send %d: %v", received, err)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for messages: received=%d acked=%d", received, acked)
		}
	}
	if received != total || acked != total {
		t.Fatalf("expected %d messages and acks, got received=%d acked=%d", total, received, acked)
	}
}

func awaitAck(t *testing.T, events <-chan transport.Event) {
	t.Helper()
	select {
	case event := <-events:
		if event.Err != nil {
			t.Fatalf("unexpected event error: %v", event.Err)
		}
		if event.Message == nil || event.Message.Type != protocol.MessageTypeAck {
			t.Fatalf("expected subscribe ack, got %+v", event.Message)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for subscribe ack")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(testWriter{}, nil))
}

type testWriter struct{}

func (testWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}
