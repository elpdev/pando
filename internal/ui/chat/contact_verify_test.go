package chat

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/store"
)

func TestVerifyContactOpensFromPaletteAndMarksVerified(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new alice: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := messaging.New(bobStore, "bob")
	if err != nil {
		t.Fatalf("new bob: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bobService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("contact from invite: %v", err)
	}
	if err := clientStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob: %v", err)
	}
	model := New(Deps{Client: stubClient{}, Messaging: aliceService, Mailbox: "alice", RecipientMailbox: "bob", RelayURL: "ws://localhost:8080/ws"})
	model.SetSize(120, 24)

	openPaletteCommand(t, model, "verify contact")
	if !model.contactVerify.open {
		t.Fatal("expected verify contact modal to open")
	}
	if !strings.Contains(model.View(), "Verify Contact") {
		t.Fatalf("expected verify contact modal in view: %q", model.View())
	}
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected verify confirmation command")
	}
	msg := cmd()
	result, ok := msg.(contactVerifyConfirmedMsg)
	if !ok {
		t.Fatalf("expected contactVerifyConfirmedMsg, got %T", msg)
	}
	_, _ = model.Update(result)
	if model.contactVerify.open {
		t.Fatal("expected verify modal to close")
	}
	stored, err := clientStore.LoadContact("bob")
	if err != nil {
		t.Fatalf("load verified contact: %v", err)
	}
	if !stored.Verified {
		t.Fatal("expected bob to be marked verified")
	}
	if !model.PeerVerified() {
		t.Fatal("expected active peer state to refresh to verified")
	}
	toast, _ := model.Toast()
	if toast != "verified contact bob" {
		t.Fatalf("unexpected toast: %q", toast)
	}
}

func TestPeerDetailVerifyShortcutOpensModal(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new alice: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := messaging.New(bobStore, "bob")
	if err != nil {
		t.Fatalf("new bob: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bobService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("contact from invite: %v", err)
	}
	if err := clientStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob: %v", err)
	}
	model := New(Deps{Client: stubClient{}, Messaging: aliceService, Mailbox: "alice", RecipientMailbox: "bob", RelayURL: "ws://localhost:8080/ws"})
	model.SetSize(120, 24)
	model.peerDetailOpen = true

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if cmd != nil {
		drainMsg(t, model, cmd)
	}
	if !model.contactVerify.open {
		t.Fatal("expected verify modal from peer detail shortcut")
	}
	if model.peerDetailFooter() != "v verify · esc close" {
		t.Fatalf("unexpected peer detail footer: %q", model.peerDetailFooter())
	}
}
