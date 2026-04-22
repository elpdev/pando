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
	if model.commandPalette.activeViewID() != paletteViewContactVerify {
		t.Fatalf("expected palette at verify view, got id=%d path=%v", model.commandPalette.activeViewID(), model.commandPalette.path)
	}
	if !strings.Contains(model.View(), "Verify") {
		t.Fatalf("expected verify title in view: %q", model.View())
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
	if model.commandPalette.open {
		t.Fatal("expected palette to close after successful verify")
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
	openPaletteCommand(t, model, "peer detail")
	if model.commandPalette.activeViewID() != paletteViewPeerDetail {
		t.Fatalf("expected palette at peer-detail view, got id=%d path=%v", model.commandPalette.activeViewID(), model.commandPalette.path)
	}
	peerDetail := &peerDetailView{m: model}
	if peerDetail.Footer() != "v verify · esc close" {
		t.Fatalf("unexpected peer detail footer: %q", peerDetail.Footer())
	}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if cmd != nil {
		drainMsg(t, model, cmd)
	}
	if model.commandPalette.activeViewID() != paletteViewContactVerify {
		t.Fatalf("expected v shortcut to navigate to verify, got id=%d path=%v", model.commandPalette.activeViewID(), model.commandPalette.path)
	}
}
