package chat

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/rendezvous"
	"github.com/elpdev/pando/internal/store"
)

// fakeRelayClient lets tests drive the lookup and rendezvous flows
// deterministically without touching the network.
type fakeRelayClient struct {
	mu sync.Mutex

	// Directory state.
	directory    map[string]*relayapi.SignedDirectoryEntry
	lookupErr    error
	lookupCalled int

	// Rendezvous state.
	puts         []relayapi.RendezvousPayload
	putErr       error
	payloads     map[string][]relayapi.RendezvousPayload
	getErr       error
	getGate      chan struct{} // blocks every GetRendezvousPayloads until closed
	onFirstGet   func()        // fired on the first Get call
	onFirstGetCh chan struct{}
}

func newFakeRelay() *fakeRelayClient {
	return &fakeRelayClient{
		directory: map[string]*relayapi.SignedDirectoryEntry{},
		payloads:  map[string][]relayapi.RendezvousPayload{},
	}
}

func (f *fakeRelayClient) LookupDirectoryEntry(mailbox string) (*relayapi.SignedDirectoryEntry, error) {
	f.mu.Lock()
	f.lookupCalled++
	err := f.lookupErr
	entry, ok := f.directory[mailbox]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("directory: mailbox %q not found", mailbox)
	}
	return entry, nil
}

func (f *fakeRelayClient) LookupDirectoryEntryByDeviceMailbox(mailbox string) (*relayapi.SignedDirectoryEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, entry := range f.directory {
		for _, device := range entry.Entry.Bundle.Devices {
			if device.Mailbox == mailbox {
				return entry, nil
			}
		}
	}
	return nil, fmt.Errorf("directory: device mailbox %q not found", mailbox)
}

func (f *fakeRelayClient) ListDiscoverableEntries() ([]relayapi.SignedDirectoryEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries := make([]relayapi.SignedDirectoryEntry, 0, len(f.directory))
	for _, entry := range f.directory {
		if !entry.Entry.Discoverable {
			continue
		}
		entries = append(entries, *entry)
	}
	return entries, nil
}

func (f *fakeRelayClient) PutRendezvousPayload(id string, p relayapi.RendezvousPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return f.putErr
	}
	f.puts = append(f.puts, p)
	f.payloads[id] = append(f.payloads[id], p)
	return nil
}

func (f *fakeRelayClient) GetRendezvousPayloads(id string) ([]relayapi.RendezvousPayload, error) {
	if f.onFirstGet != nil {
		fn := f.onFirstGet
		f.onFirstGet = nil
		fn()
	}
	if f.getGate != nil {
		<-f.getGate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	return append([]relayapi.RendezvousPayload(nil), f.payloads[id]...), nil
}

func (f *fakeRelayClient) seedDirectory(t *testing.T, peer *messaging.Service) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	id := peer.Identity()
	entry := relayapi.DirectoryEntry{
		Mailbox:     id.AccountID,
		Bundle:      id.InviteBundle(),
		PublishedAt: time.Now().UTC(),
		Version:     time.Now().UTC().UnixNano(),
	}
	signed, err := relayapi.SignDirectoryEntry(entry, id.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	f.directory[id.AccountID] = signed
}

func (f *fakeRelayClient) seedRendezvous(t *testing.T, code string, peer *messaging.Service) {
	t.Helper()
	payload, err := rendezvous.EncryptBundle(code, peer.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("encrypt peer payload: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payloads[rendezvous.DeriveID(code)] = append(f.payloads[rendezvous.DeriveID(code)], payload)
}

func newChatModel(t *testing.T, mailbox string, relay *fakeRelayClient, relayURL string) (*Model, *messaging.Service) {
	t.Helper()
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, mailbox)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	deps := Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   mailbox,
		RelayURL:  relayURL,
	}
	if relay != nil {
		deps.RelayClientFactory = func(url, token string) (RelayClient, error) { return relay, nil }
	}
	model := New(deps)
	model.SetSize(100, 24)
	return model, service
}

// drainMsg invokes the returned cmd if non-nil, then keeps feeding resulting
// messages back into Update until the command chain settles.
func drainMsg(t *testing.T, model *Model, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return
		}
		_, cmd = model.Update(msg)
	}
}

func openAddContactViaPalette(t *testing.T, model *Model) {
	t.Helper()
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	drainMsg(t, model, cmd)
	// "add contact" uniquely matches `Contacts › Add contact` in the palette's
	// cross-level search, so Enter activates the leaf directly.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("add contact")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainMsg(t, model, cmd)
	if !model.addContact.open {
		t.Fatal("expected add contact modal to open from command palette")
	}
}

func TestAddContactLookupAppliesRelayDirectoryTrust(t *testing.T) {
	peer, _, err := messaging.New(store.NewClientStore(t.TempDir()), "carol")
	if err != nil {
		t.Fatalf("new peer: %v", err)
	}
	relay := newFakeRelay()
	relay.seedDirectory(t, peer)

	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")

	openAddContactViaPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if model.addContact.mode != addContactModeLookup {
		t.Fatalf("expected lookup mode, got %v", model.addContact.mode)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("carol")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected lookup command")
	}
	drainMsg(t, model, cmd)

	if model.addContact.open {
		t.Fatal("expected modal to close after successful lookup")
	}
	contact, err := model.messaging.Contact("carol")
	if err != nil {
		t.Fatalf("load carol: %v", err)
	}
	if contact.TrustSource != identity.TrustSourceRelayDirectory {
		t.Fatalf("expected TrustSourceRelayDirectory, got %q", contact.TrustSource)
	}
	if !contact.Verified {
		t.Fatal("expected contact to be verified")
	}
	if toast, _ := model.Toast(); !strings.Contains(toast, "relay-directory contact carol") {
		t.Fatalf("unexpected toast: %q", toast)
	}
	if relay.lookupCalled != 1 {
		t.Fatalf("expected 1 directory lookup, got %d", relay.lookupCalled)
	}
}

func TestAddContactLookupValidationRequiresMailbox(t *testing.T) {
	relay := newFakeRelay()
	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")

	openAddContactViaPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected no command when mailbox is empty")
	}
	if model.addContact.error != "mailbox is required" {
		t.Fatalf("unexpected error: %q", model.addContact.error)
	}
	if relay.lookupCalled != 0 {
		t.Fatalf("did not expect lookup call, got %d", relay.lookupCalled)
	}
}

func TestAddContactInviteAcceptAppliesInviteCodeTrust(t *testing.T) {
	peer, _, err := messaging.New(store.NewClientStore(t.TempDir()), "dave")
	if err != nil {
		t.Fatalf("new peer: %v", err)
	}
	relay := newFakeRelay()
	code := "42424-24242"
	relay.seedRendezvous(t, code, peer)

	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")

	openAddContactViaPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if model.addContact.mode != addContactModeInviteAccept {
		t.Fatalf("expected invite-accept mode, got %v", model.addContact.mode)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(code)})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected invite-exchange command")
	}
	drainMsg(t, model, cmd)

	if model.addContact.open {
		t.Fatal("expected modal to close after successful invite exchange")
	}
	contact, err := model.messaging.Contact("dave")
	if err != nil {
		t.Fatalf("load dave: %v", err)
	}
	if contact.TrustSource != identity.TrustSourceInviteCode {
		t.Fatalf("expected TrustSourceInviteCode, got %q", contact.TrustSource)
	}
	if !contact.Verified {
		t.Fatal("expected contact to be verified")
	}
	if toast, _ := model.Toast(); !strings.Contains(toast, "invite-code contact dave") {
		t.Fatalf("unexpected toast: %q", toast)
	}
}

func TestAddContactInviteStartGeneratesCodeAndUploadsPayload(t *testing.T) {
	relay := newFakeRelay()
	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")

	// Gate the rendezvous poll so the exchange cmd stays busy — we only want
	// to observe the start-side state, not wait for completion.
	relay.getGate = make(chan struct{})

	openAddContactViaPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	_, startCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if startCmd == nil {
		t.Fatal("expected invite-start command")
	}

	// startCmd is tea.Batch(invite-started publisher, blocking exchange).
	// Run the batch in a goroutine — the exchange call blocks on the gate.
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		batch := startCmd()
		if typed, ok := batch.(tea.BatchMsg); ok {
			for _, sub := range typed {
				if sub != nil {
					_ = sub()
				}
			}
		}
	}()

	// Drain the invite-started message synchronously: pop the first branch of the
	// batch (which returns immediately) by replaying the message generator.
	// We re-call the start-cmd closure by extracting what we need from the
	// gated relay instead.
	_, _ = model.Update(addContactInviteStartedMsg{code: model.addContact.code})

	// Wait for the Put to land so we know Exchange reached the poll loop.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		relay.mu.Lock()
		n := len(relay.puts)
		relay.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if model.addContact.code == "" {
		t.Fatal("expected generated code to be stored on the model")
	}
	view := model.View()
	if !strings.Contains(view, model.addContact.code) {
		t.Fatalf("expected generated code in view: %q", view)
	}
	relay.mu.Lock()
	puts := len(relay.puts)
	relay.mu.Unlock()
	if puts == 0 {
		t.Fatal("expected at least one rendezvous put")
	}
	// Cancel the in-flight exchange cleanly so the goroutine can exit.
	if m := model; m.addContact.cancel != nil {
		m.addContact.cancel()
	}
	close(relay.getGate)
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("exchange goroutine did not exit after cancel + gate release")
	}
}

func TestAddContactInviteEscCancelsPolling(t *testing.T) {
	relay := newFakeRelay()
	relay.getGate = make(chan struct{}) // block until we say otherwise

	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")
	openAddContactViaPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("12345-67890")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected invite-exchange command")
	}

	resultCh := make(chan tea.Msg, 1)
	go func() { resultCh <- cmd() }()

	// Wait for the Put to land so we know Exchange has reached the poll loop.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		relay.mu.Lock()
		n := len(relay.puts)
		relay.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Cancel via Esc — the cancel func runs context.CancelFunc, releasing
	// Exchange's select. Unblock the gate so the in-flight Get returns and
	// the ctx.Done() arm fires.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	close(relay.getGate)

	var msg tea.Msg
	select {
	case msg = <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("invite-exchange cmd never completed after cancel")
	}
	result, ok := msg.(addContactInviteExchangeResultMsg)
	if !ok {
		t.Fatalf("expected addContactInviteExchangeResultMsg, got %T", msg)
	}
	if !result.cancelled {
		t.Fatalf("expected cancelled=true, got err=%v", result.err)
	}
	_, _ = model.Update(result)
	if !model.addContact.open {
		t.Fatal("modal should remain open after cancel")
	}
	if model.addContact.busy {
		t.Fatal("busy should clear after cancel result")
	}
	if model.addContact.error != "cancelled" {
		t.Fatalf("expected cancelled error label, got %q", model.addContact.error)
	}
}

func TestAddContactChooserDisablesRelayPathsWithoutRelay(t *testing.T) {
	model, _ := newChatModel(t, "alice", nil, "")

	openAddContactViaPalette(t, model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if cmd != nil {
		t.Fatal("expected no lookup command without relay")
	}
	if model.addContact.mode != addContactModeChooser {
		t.Fatalf("expected chooser mode to persist, got %v", model.addContact.mode)
	}
	if model.addContact.error != "no relay configured" {
		t.Fatalf("unexpected error: %q", model.addContact.error)
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if cmd != nil {
		t.Fatal("expected no invite command without relay")
	}
	view := model.View()
	if !strings.Contains(view, "no relay configured") {
		t.Fatalf("expected chooser to mention no relay: %q", view)
	}
}

func TestAddContactChooserArrowNavigationUsesSelectedAction(t *testing.T) {
	relay := newFakeRelay()
	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")

	openAddContactViaPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if model.addContact.mode != addContactModeLookup {
		t.Fatalf("expected lookup mode after selecting second row, got %v", model.addContact.mode)
	}
}

func TestAddContactInviteChoiceArrowNavigationUsesSelectedAction(t *testing.T) {
	relay := newFakeRelay()
	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")

	openAddContactViaPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if model.addContact.mode != addContactModeInviteAccept {
		t.Fatalf("expected invite-accept mode after selecting second row, got %v", model.addContact.mode)
	}
}

func TestAddContactLookupSurfacesRelayError(t *testing.T) {
	relay := newFakeRelay()
	relay.lookupErr = errors.New("relay 500")
	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")

	openAddContactViaPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("carol")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainMsg(t, model, cmd)

	if !model.addContact.open {
		t.Fatal("modal should remain open on relay error")
	}
	if model.addContact.error != "relay 500" {
		t.Fatalf("expected relay error surfaced verbatim, got %q", model.addContact.error)
	}
}

func TestAddContactInviteAcceptValidationRejectsEmptyCode(t *testing.T) {
	relay := newFakeRelay()
	model, _ := newChatModel(t, "alice", relay, "ws://relay/ws")

	openAddContactViaPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected no command for empty code")
	}
	if model.addContact.error != "invite code is required" {
		t.Fatalf("unexpected error: %q", model.addContact.error)
	}
}
