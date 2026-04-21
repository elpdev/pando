package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
)

func TestSendContactRequestModalOpensFromPalette(t *testing.T) {
	model := newHelpTestModel(t)
	openPaletteCommand(t, model, "send contact request")
	if !model.contactRequestSend.open {
		t.Fatal("expected send contact request modal to open")
	}
	if !strings.Contains(model.View(), "Send Contact Request") {
		t.Fatalf("expected send contact request modal in view: %q", model.View())
	}
}

func TestSendContactRequestPersistsOutgoingRequest(t *testing.T) {
	relay := newFakeRelay()
	_, peerService := newChatModel(t, "bob", nil, "ws://relay/ws")
	seedDiscoverableDirectory(t, relay, peerService)
	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")
	model.contactRequestSend.deps.publishEnvelopes = func(_ context.Context, relayURL, relayToken string, envelopes []protocol.Envelope) error {
		if relayURL != "ws://relay/ws" {
			t.Fatalf("expected relay url ws://relay/ws, got %q", relayURL)
		}
		if len(envelopes) == 0 {
			t.Fatal("expected request envelopes to publish")
		}
		return nil
	}

	openPaletteCommand(t, model, "send contact request")
	if !model.contactRequestSend.open {
		t.Fatal("expected send contact request modal to open")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bob")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi bob")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected send contact request command")
	}
	msg := cmd()
	result, ok := msg.(contactRequestSendResultMsg)
	if !ok {
		t.Fatalf("expected contactRequestSendResultMsg, got %T", msg)
	}
	if result.err != nil {
		t.Fatalf("send contact request failed: %v", result.err)
	}
	_, _ = model.Update(result)
	if model.contactRequestSend.open {
		t.Fatal("expected modal to close after send")
	}
	req, err := model.messaging.LoadContactRequest("bob")
	if err != nil {
		t.Fatalf("load contact request: %v", err)
	}
	if req.Direction != store.ContactRequestDirectionOutgoing || req.Status != store.ContactRequestStatusPending {
		t.Fatalf("unexpected request state: %+v", req)
	}
	if req.Note != "hi bob" {
		t.Fatalf("expected note hi bob, got %q", req.Note)
	}
	if model.pendingRequestsCount != 0 {
		t.Fatalf("expected pending incoming count unchanged, got %d", model.pendingRequestsCount)
	}
	toast, _ := model.Toast()
	if toast != "sent contact request to bob" {
		t.Fatalf("unexpected toast: %q", toast)
	}
}

func TestSendContactRequestRequiresMailbox(t *testing.T) {
	model := newHelpTestModel(t)
	openPaletteCommand(t, model, "send contact request")
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected no command when mailbox is missing")
	}
	if model.contactRequestSend.error != "mailbox is required" {
		t.Fatalf("expected mailbox validation error, got %q", model.contactRequestSend.error)
	}
}

func TestSendContactRequestShowsPublishError(t *testing.T) {
	relay := newFakeRelay()
	_, peerService := newChatModel(t, "bob", nil, "ws://relay/ws")
	seedDiscoverableDirectory(t, relay, peerService)
	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")
	model.contactRequestSend.deps.publishEnvelopes = func(context.Context, string, string, []protocol.Envelope) error {
		return errors.New("relay down")
	}
	openPaletteCommand(t, model, "send contact request")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bob")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected send command")
	}
	msg := cmd()
	result, ok := msg.(contactRequestSendResultMsg)
	if !ok {
		t.Fatalf("expected contactRequestSendResultMsg, got %T", msg)
	}
	if result.err == nil {
		t.Fatal("expected publish error")
	}
	_, _ = model.Update(result)
	if model.contactRequestSend.error != "relay down" {
		t.Fatalf("expected relay error to surface, got %q", model.contactRequestSend.error)
	}
	if !model.contactRequestSend.open {
		t.Fatal("expected modal to remain open on error")
	}
}

func seedDiscoverableDirectory(t *testing.T, relay *fakeRelayClient, peerService *messaging.Service) {
	t.Helper()
	id := peerService.Identity()
	signed, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{
		Mailbox:      id.AccountID,
		Bundle:       id.InviteBundle(),
		Discoverable: true,
		PublishedAt:  time.Now().UTC(),
		Version:      time.Now().UTC().UnixNano(),
	}, id.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign discoverable entry: %v", err)
	}
	relay.directory[id.AccountID] = signed
}
