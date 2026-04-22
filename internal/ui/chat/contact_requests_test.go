package chat

import (
	"context"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/transport"
)

type failingClient struct{}

func (failingClient) Connect(context.Context) error { return nil }
func (failingClient) Events() <-chan transport.Event {
	ch := make(chan transport.Event)
	return ch
}
func (failingClient) Send(protocol.Envelope) error {
	return fmt.Errorf("network down")
}
func (failingClient) Close() error { return nil }

func signDirectoryEntry(t *testing.T, id *identity.Identity) *relayapi.SignedDirectoryEntry {
	t.Helper()
	signed, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{
		Mailbox:      id.AccountID,
		Bundle:       id.InviteBundle(),
		Discoverable: true,
		PublishedAt:  time.Now().UTC(),
		Version:      time.Now().UTC().UnixNano(),
	}, id.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	return signed
}

type fakeDirectoryClient struct {
	entries map[string]relayapi.SignedDirectoryEntry
	device  map[string]relayapi.SignedDirectoryEntry
}

func newFakeDirectoryClient(entries ...*relayapi.SignedDirectoryEntry) *fakeDirectoryClient {
	client := &fakeDirectoryClient{entries: make(map[string]relayapi.SignedDirectoryEntry), device: make(map[string]relayapi.SignedDirectoryEntry)}
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		client.entries[entry.Entry.Mailbox] = *entry
		for _, device := range entry.Entry.Bundle.Devices {
			client.device[device.Mailbox] = *entry
		}
	}
	return client
}

func (f *fakeDirectoryClient) LookupDirectoryEntry(mailbox string) (*relayapi.SignedDirectoryEntry, error) {
	entry, ok := f.entries[mailbox]
	if !ok {
		return nil, fmt.Errorf("entry %q not found", mailbox)
	}
	copyEntry := entry
	return &copyEntry, nil
}

func (f *fakeDirectoryClient) LookupDirectoryEntryByDeviceMailbox(mailbox string) (*relayapi.SignedDirectoryEntry, error) {
	entry, ok := f.device[mailbox]
	if !ok {
		return nil, fmt.Errorf("device %q not found", mailbox)
	}
	copyEntry := entry
	return &copyEntry, nil
}

func (f *fakeDirectoryClient) ListDiscoverableEntries() ([]relayapi.SignedDirectoryEntry, error) {
	entries := make([]relayapi.SignedDirectoryEntry, 0, len(f.entries))
	for _, entry := range f.entries {
		if !entry.Entry.Discoverable {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// wirePair sets up a fake directory so that sender/recipient can resolve
// each other for contact-request lookups during HandleIncoming.
func wirePair(sender, recipient *messaging.Service, extra ...*messaging.Service) {
	entries := []*relayapi.SignedDirectoryEntry{signForService(sender), signForService(recipient)}
	for _, svc := range extra {
		entries = append(entries, signForService(svc))
	}
	directory := newFakeDirectoryClient(entries...)
	sender.SetDirectoryClient(directory)
	recipient.SetDirectoryClient(directory)
	for _, svc := range extra {
		svc.SetDirectoryClient(directory)
	}
}

// signForService signs a directory entry for the service's identity without
// requiring *testing.T in callers that already have error handling.
func signForService(s *messaging.Service) *relayapi.SignedDirectoryEntry {
	id := s.Identity()
	signed, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{
		Mailbox:      id.AccountID,
		Bundle:       id.InviteBundle(),
		Discoverable: true,
		PublishedAt:  time.Now().UTC(),
		Version:      time.Now().UTC().UnixNano(),
	}, id.AccountSigningPrivate)
	if err != nil {
		panic(fmt.Sprintf("sign directory entry for %s: %v", id.AccountID, err))
	}
	return signed
}

// seedPendingIncomingRequest saves a fresh pending-incoming request on
// recipient's store by invoking recipient.HandleIncoming on request
// envelopes produced from sender's service.
func seedPendingIncomingRequest(t *testing.T, sender, recipient *messaging.Service, recipientStore *store.ClientStore, note string) store.ContactRequest {
	t.Helper()
	wirePair(sender, recipient)
	recipientEntry := signDirectoryEntry(t, recipient.Identity())
	envelopes, _, err := sender.ContactRequestEnvelopes(recipientEntry, note)
	if err != nil {
		t.Fatalf("create request envelopes: %v", err)
	}
	for idx, envelope := range envelopes {
		envelope.ID = fmt.Sprintf("seed-request-%d-%s", idx, sender.Identity().AccountID)
		if _, err := recipient.HandleIncoming(envelope); err != nil {
			t.Fatalf("recipient handle incoming: %v", err)
		}
	}
	stored, err := recipientStore.LoadContactRequest(sender.Identity().AccountID)
	if err != nil {
		t.Fatalf("load seeded request: %v", err)
	}
	return *stored
}

func TestIncomingContactRequestUpdatesBadgeAndToasts(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := messaging.New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := messaging.New(bobStore, "bob")
	if err != nil {
		t.Fatalf("new bob service: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: aliceService,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})

	request := seedPendingIncomingRequest(t, bobService, aliceService, aliceStore, "hello from bob")
	model.handleContactRequestUpdate(&request)

	if model.pendingRequestsCount != 1 {
		t.Fatalf("expected pendingRequestsCount 1, got %d", model.pendingRequestsCount)
	}
	if len(model.contactRequests.items) != 1 {
		t.Fatalf("expected one cached request, got %d", len(model.contactRequests.items))
	}
	if got := model.contactRequests.items[0].AccountID; got != "bob" {
		t.Fatalf("expected cached request from bob, got %q", got)
	}
	toast, _ := model.Toast()
	if toast != "contact request from bob" {
		t.Fatalf("unexpected toast: %q", toast)
	}
}

func TestContactRequestsModalAcceptPath(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := messaging.New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := messaging.New(bobStore, "bob")
	if err != nil {
		t.Fatalf("new bob service: %v", err)
	}

	client := &recordingClient{}
	model := New(Deps{
		Client:    client,
		Messaging: aliceService,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(120, 40)

	request := seedPendingIncomingRequest(t, bobService, aliceService, aliceStore, "hi alice")
	model.handleContactRequestUpdate(&request)
	openPaletteCommand(t, model, "contact requests")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd == nil {
		t.Fatal("expected decision command")
	}
	msg := cmd()
	result, ok := msg.(contactRequestDecisionResultMsg)
	if !ok {
		t.Fatalf("expected contactRequestDecisionResultMsg, got %T", msg)
	}
	if result.err != nil {
		t.Fatalf("accept failed: %v", result.err)
	}
	if !result.accepted {
		t.Fatal("expected accepted=true in result")
	}
	if result.contact == nil || result.contact.AccountID != "bob" {
		t.Fatalf("expected imported bob contact, got %+v", result.contact)
	}
	if len(client.sent) == 0 {
		t.Fatal("expected accept envelopes to be sent")
	}

	model.Update(result)
	updatedRequest, err := aliceStore.LoadContactRequest("bob")
	if err != nil {
		t.Fatalf("load updated request: %v", err)
	}
	if updatedRequest.Status != store.ContactRequestStatusAccepted {
		t.Fatalf("expected accepted status, got %q", updatedRequest.Status)
	}
	if model.pendingRequestsCount != 0 {
		t.Fatalf("expected pendingRequestsCount to drop to 0, got %d", model.pendingRequestsCount)
	}
	if _, err := aliceStore.LoadContact("bob"); err != nil {
		t.Fatalf("expected bob contact after accept, got %v", err)
	}
	foundContact := false
	for _, c := range model.contacts {
		if c.Mailbox == "bob" {
			foundContact = true
			break
		}
	}
	if !foundContact {
		t.Fatal("expected bob to be added to sidebar contacts")
	}
}

func TestContactRequestsModalRejectPath(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := messaging.New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := messaging.New(bobStore, "bob")
	if err != nil {
		t.Fatalf("new bob service: %v", err)
	}

	client := &recordingClient{}
	model := New(Deps{
		Client:    client,
		Messaging: aliceService,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(120, 40)

	request := seedPendingIncomingRequest(t, bobService, aliceService, aliceStore, "")
	model.handleContactRequestUpdate(&request)
	openPaletteCommand(t, model, "contact requests")

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("expected reject command")
	}
	msg := cmd()
	result, ok := msg.(contactRequestDecisionResultMsg)
	if !ok {
		t.Fatalf("expected decision result, got %T", msg)
	}
	if result.err != nil {
		t.Fatalf("reject failed: %v", result.err)
	}
	if result.accepted {
		t.Fatal("expected accepted=false")
	}
	if result.contact != nil {
		t.Fatal("expected no contact import on reject")
	}

	model.Update(result)
	updatedRequest, err := aliceStore.LoadContactRequest("bob")
	if err != nil {
		t.Fatalf("load updated request: %v", err)
	}
	if updatedRequest.Status != store.ContactRequestStatusRejected {
		t.Fatalf("expected rejected status, got %q", updatedRequest.Status)
	}
	if _, err := aliceStore.LoadContact("bob"); err == nil {
		t.Fatal("expected no bob contact after reject")
	}
	if model.pendingRequestsCount != 0 {
		t.Fatalf("expected pendingRequestsCount 0, got %d", model.pendingRequestsCount)
	}
}

func TestContactRequestsModalHandlesSendError(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := messaging.New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := messaging.New(bobStore, "bob")
	if err != nil {
		t.Fatalf("new bob service: %v", err)
	}

	model := New(Deps{
		Client:    failingClient{},
		Messaging: aliceService,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(120, 40)

	request := seedPendingIncomingRequest(t, bobService, aliceService, aliceStore, "")
	model.handleContactRequestUpdate(&request)
	openPaletteCommand(t, model, "contact requests")

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("expected decision command")
	}
	result, ok := cmd().(contactRequestDecisionResultMsg)
	if !ok {
		t.Fatal("expected decision result msg")
	}
	if result.err == nil {
		t.Fatal("expected send error")
	}

	model.Update(result)
	updatedRequest, err := aliceStore.LoadContactRequest("bob")
	if err != nil {
		t.Fatalf("load request after send failure: %v", err)
	}
	if updatedRequest.Status != store.ContactRequestStatusPending {
		t.Fatalf("expected status to remain pending after send failure, got %q", updatedRequest.Status)
	}
	if model.pendingRequestsCount != 1 {
		t.Fatalf("expected pending count to remain 1, got %d", model.pendingRequestsCount)
	}
	toast, level := model.Toast()
	if toast == "" || level != ToastBad {
		t.Fatalf("expected bad toast on send failure, got %q level=%v", toast, level)
	}
}

func TestStartupLoadsPersistedRequests(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := messaging.New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := messaging.New(bobStore, "bob")
	if err != nil {
		t.Fatalf("new bob service: %v", err)
	}
	carolStore := store.NewClientStore(t.TempDir())
	carolService, _, err := messaging.New(carolStore, "carol")
	if err != nil {
		t.Fatalf("new carol service: %v", err)
	}
	seedPendingIncomingRequest(t, bobService, aliceService, aliceStore, "")
	seedPendingIncomingRequest(t, carolService, aliceService, aliceStore, "")

	model := New(Deps{
		Client:    stubClient{},
		Messaging: aliceService,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})

	if model.pendingRequestsCount != 2 {
		t.Fatalf("expected pendingRequestsCount 2 after startup, got %d", model.pendingRequestsCount)
	}
	if len(model.contactRequests.items) != 2 {
		t.Fatalf("expected 2 cached requests, got %d", len(model.contactRequests.items))
	}
}

func TestDuplicateIncomingContactRequestDedupes(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := messaging.New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := messaging.New(bobStore, "bob")
	if err != nil {
		t.Fatalf("new bob service: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: aliceService,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})

	request := seedPendingIncomingRequest(t, bobService, aliceService, aliceStore, "first")
	model.handleContactRequestUpdate(&request)
	request2 := request
	request2.Note = "second"
	request2.UpdatedAt = time.Now().UTC()
	model.handleContactRequestUpdate(&request2)

	if model.pendingRequestsCount != 1 {
		t.Fatalf("expected deduped pendingRequestsCount 1, got %d", model.pendingRequestsCount)
	}
	if len(model.contactRequests.items) != 1 {
		t.Fatalf("expected one cached entry after dedupe, got %d", len(model.contactRequests.items))
	}
	if got := model.contactRequests.items[0].Note; got != "second" {
		t.Fatalf("expected deduped entry to keep latest note, got %q", got)
	}
}
