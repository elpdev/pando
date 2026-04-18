package chat

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/transport"
)

type stubClient struct{}

func (stubClient) Connect(context.Context) error { return nil }
func (stubClient) Events() <-chan transport.Event {
	ch := make(chan transport.Event)
	return ch
}
func (stubClient) Send(protocol.Envelope) error { return nil }
func (stubClient) Close() error                 { return nil }

type recordingClient struct {
	sent []protocol.Envelope
}

func (c *recordingClient) Connect(context.Context) error { return nil }
func (c *recordingClient) Events() <-chan transport.Event {
	ch := make(chan transport.Event)
	return ch
}
func (c *recordingClient) Send(envelope protocol.Envelope) error {
	c.sent = append(c.sent, envelope)
	return nil
}
func (c *recordingClient) Close() error { return nil }

func TestAuthFailureKeepsHistoryVisibleAndStopsReconnect(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := service.SaveReceived("bobn", "hello from history", time.Now().UTC()); err != nil {
		t.Fatalf("save received history: %v", err)
	}

	model := New(Deps{
		Client:           stubClient{},
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bobn",
		RelayURL:         "wss://pandorelay.network/ws",
	})

	initCmd := model.Init()
	if initCmd == nil {
		t.Fatal("expected init command")
	}
	if len(model.messages) < 4 {
		t.Fatalf("expected local history to be loaded, got %d messages", len(model.messages))
	}

	updated, cmd := model.Update(clientEventMsg(transport.Event{Err: fmt.Errorf("%w: check relay token", transport.ErrUnauthorized)}))
	if updated != model {
		t.Fatal("expected update to mutate the existing model")
	}
	if cmd != nil {
		t.Fatal("expected no reconnect command after auth failure")
	}
	if !model.authFailed {
		t.Fatal("expected auth failure state")
	}
	if model.connected {
		t.Fatal("expected model to remain disconnected")
	}
	if model.input.Placeholder != "Relay auth failed. Restart with --relay-token" {
		t.Fatalf("unexpected placeholder: %q", model.input.Placeholder)
	}
	if model.status != "relay auth failed: relay unauthorized: check relay token" {
		t.Fatalf("unexpected status: %q", model.status)
	}

	model.input.SetValue("hi")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.status != "cannot send: relay auth failed; restart with --relay-token" {
		t.Fatalf("unexpected send status: %q", model.status)
	}
	if model.input.Value() != "hi" {
		t.Fatalf("expected input to remain unchanged, got %q", model.input.Value())
	}
	if len(model.messages) < 4 {
		t.Fatalf("expected local history to remain visible, got %d messages", len(model.messages))
	}
}

func TestSendPhotoCommandQueuesAttachmentBatch(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := messaging.New(bobStore, "bob")
	if err != nil {
		t.Fatalf("new bob service: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bobService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("bob invite to contact: %v", err)
	}
	if err := clientStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob contact: %v", err)
	}

	photoPath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(photoPath, mustPhotoBytes(t), 0o600); err != nil {
		t.Fatalf("write photo: %v", err)
	}

	client := &recordingClient{}
	model := New(Deps{
		Client:           client,
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.connected = true
	model.connecting = false
	model.input.SetValue("/send-photo " + photoPath)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd == nil {
		t.Fatal("expected send command")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected send result message")
	}
	_, _ = model.Update(msg)
	if len(client.sent) == 0 {
		t.Fatal("expected photo send to produce envelopes")
	}
	if model.input.Value() != "" {
		t.Fatalf("expected input to clear after send, got %q", model.input.Value())
	}
	found := false
	for _, message := range model.messages {
		if message == "you -> bob: photo sent: photo.png" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sent photo message in history: %+v", model.messages)
	}
}

func mustPhotoBytes(t *testing.T) []byte {
	t.Helper()
	bytes, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7Zl9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode photo bytes: %v", err)
	}
	return bytes
}
