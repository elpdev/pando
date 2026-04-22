package ws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relay"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/relayclient"
	"github.com/elpdev/pando/internal/transport"
)

func TestConnectReturnsUnauthorizedErrorForBadHandshake(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "relay auth token is required", http.StatusUnauthorized)
	}))
	defer server.Close()

	aliceID, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	client := NewClient("ws"+server.URL[len("http"):], "wrong-token", aliceID, relayclient.ClientOptions{})
	err = client.Connect(context.Background())
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

	aliceID, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new alice identity: %v", err)
	}
	alice := NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", aliceID, relayclient.ClientOptions{})
	defer alice.Close()
	publishDirectoryEntry(t, server, aliceID)
	if err := alice.Connect(ctx); err != nil {
		t.Fatalf("connect alice: %v", err)
	}

	bobID, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new bob identity: %v", err)
	}
	bob := NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", bobID, relayclient.ClientOptions{})
	defer bob.Close()
	publishDirectoryEntry(t, server, bobID)
	if err := bob.Connect(ctx); err != nil {
		t.Fatalf("connect bob: %v", err)
	}

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

func TestConnectRequiresPublishedDirectoryEntryBeforeFirstSubscribe(t *testing.T) {
	server := httptest.NewServer(relay.NewServer(testLogger(), relay.NewMemoryQueueStore(), relay.Options{}).Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	aliceID, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new alice identity: %v", err)
	}
	client := NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", aliceID, relayclient.ClientOptions{})
	defer client.Close()

	err = client.Connect(ctx)
	if err == nil {
		t.Fatal("expected unpublished mailbox connect failure")
	}
	if !strings.Contains(err.Error(), "publish your signed relay directory entry before connecting") {
		t.Fatalf("expected unpublished mailbox guidance, got %v", err)
	}
	if !strings.Contains(err.Error(), "pando contact publish-directory --mailbox alice") {
		t.Fatalf("expected publish command guidance, got %v", err)
	}

	publishDirectoryEntry(t, server, aliceID)
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("expected connect to succeed after publish, got %v", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(testWriter{}, nil))
}

func TestConnectRejectsInvalidRelayCAForTLS(t *testing.T) {
	aliceID, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	client := NewClient("wss://relay.example/ws", "", aliceID, relayclient.ClientOptions{CAPath: filepath.Join(t.TempDir(), "missing.pem")})
	err = client.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read relay CA file") {
		t.Fatalf("expected relay CA read error, got %v", err)
	}
}

func TestDisconnectKeepsClientReusable(t *testing.T) {
	server := httptest.NewServer(relay.NewServer(testLogger(), relay.NewMemoryQueueStore(), relay.Options{}).Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	aliceID, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new alice identity: %v", err)
	}
	client := NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", aliceID, relayclient.ClientOptions{})
	defer client.Close()
	publishDirectoryEntry(t, server, aliceID)
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("connect client: %v", err)
	}
	if err := client.Disconnect(); err != nil {
		t.Fatalf("disconnect client: %v", err)
	}

	select {
	case event := <-client.Events():
		if event.Err != nil || event.Message != nil {
			t.Fatalf("expected no disconnect event, got %+v", event)
		}
	case <-time.After(200 * time.Millisecond):
	}

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("reconnect client: %v", err)
	}
	if err := client.Send(protocol.Envelope{SenderMailbox: "alice", RecipientMailbox: "alice", Body: "hello after reconnect"}); err != nil {
		t.Fatalf("send after reconnect: %v", err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-client.Events():
			if event.Err != nil {
				t.Fatalf("unexpected event error after reconnect: %v", event.Err)
			}
			if event.Message != nil && event.Message.Type == protocol.MessageTypeAck {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for ack after reconnect")
		}
	}
}

type testWriter struct{}

func (testWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func publishDirectoryEntry(t *testing.T, server *httptest.Server, id *identity.Identity) {
	t.Helper()
	client, err := relayapi.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", relayclient.ClientOptions{})
	if err != nil {
		t.Fatalf("new relay api client: %v", err)
	}
	signed, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{Mailbox: id.AccountID, Bundle: id.InviteBundle(), PublishedAt: time.Now().UTC(), Version: time.Now().UTC().UnixNano()}, id.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	if _, err := client.PublishDirectoryEntry(*signed); err != nil {
		t.Fatalf("publish directory entry: %v", err)
	}
}
