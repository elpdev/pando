package chat

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/invite"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/transport"
	"github.com/elpdev/pando/internal/ui/style"
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
	if err := service.SaveReceived("bobn", "hello from history", time.Now().UTC(), nil); err != nil {
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
	if len(model.msgs.rendered) < 1 {
		t.Fatalf("expected local history to be loaded, got %d messages", len(model.msgs.rendered))
	}

	updated, cmd := model.Update(clientEventMsg(transport.Event{Err: fmt.Errorf("%w: check relay token", transport.ErrUnauthorized)}))
	if updated != model {
		t.Fatal("expected update to mutate the existing model")
	}
	if cmd != nil {
		t.Fatal("expected no reconnect command after auth failure")
	}
	if !model.conn.authFailed {
		t.Fatal("expected auth failure state")
	}
	if model.conn.connected {
		t.Fatal("expected model to remain disconnected")
	}
	if model.input.Placeholder != "Relay auth failed. Restart with --relay-token" {
		t.Fatalf("unexpected placeholder: %q", model.input.Placeholder)
	}
	if model.conn.status != "relay auth failed: relay unauthorized: check relay token" {
		t.Fatalf("unexpected status: %q", model.conn.status)
	}

	model.input.SetValue("hi")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if toast, _ := model.Toast(); toast != "cannot send: relay auth failed; restart with --relay-token" {
		t.Fatalf("unexpected send toast: %q", toast)
	}
	if model.input.Value() != "hi" {
		t.Fatalf("expected input to remain unchanged, got %q", model.input.Value())
	}
	if len(model.msgs.rendered) < 1 {
		t.Fatalf("expected local history to remain visible, got %d messages", len(model.msgs.rendered))
	}
}

func TestLoadHistoryKeepsStructuredAttachmentMetadata(t *testing.T) {
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
	attachment := messaging.NewAttachmentRecord(messaging.AttachmentTypePhoto, "photo.png", "image/png", photoPath, 42)
	if err := service.SaveReceived("bob", messaging.AttachmentReceivedBody(messaging.AttachmentTypePhoto, "photo.png", photoPath), time.Now().UTC(), attachment); err != nil {
		t.Fatalf("save photo history: %v", err)
	}

	model := New(Deps{Client: stubClient{}, Messaging: service, Mailbox: "alice", RecipientMailbox: "bob", RelayURL: "ws://localhost:8080/ws"})
	model.Init()
	if len(model.msgs.items) != 1 {
		t.Fatalf("expected one history item, got %d", len(model.msgs.items))
	}
	if model.msgs.items[0].attachment == nil || model.msgs.items[0].attachment.LocalPath != photoPath {
		t.Fatalf("expected structured attachment metadata on loaded history item, got %+v", model.msgs.items[0].attachment)
	}
}

func TestLoadHistoryParsesLegacyAttachmentBody(t *testing.T) {
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
	photoPath := filepath.Join(t.TempDir(), "legacy-photo.png")
	if err := os.WriteFile(photoPath, mustPhotoBytes(t), 0o600); err != nil {
		t.Fatalf("write photo: %v", err)
	}
	if err := clientStore.AppendHistory(service.Identity(), store.MessageRecord{
		PeerMailbox: "bob",
		Direction:   "inbound",
		Body:        messaging.AttachmentReceivedBody(messaging.AttachmentTypePhoto, "legacy-photo.png", photoPath),
		Timestamp:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append legacy history: %v", err)
	}

	model := New(Deps{Client: stubClient{}, Messaging: service, Mailbox: "alice", RecipientMailbox: "bob", RelayURL: "ws://localhost:8080/ws"})
	model.Init()
	if len(model.msgs.items) != 1 {
		t.Fatalf("expected one history item, got %d", len(model.msgs.items))
	}
	if model.msgs.items[0].attachment == nil || model.msgs.items[0].attachment.LocalPath != photoPath {
		t.Fatalf("expected legacy attachment body to be parsed, got %+v", model.msgs.items[0].attachment)
	}
}

func TestUnchangedContactUpdateForActivePeerDoesNotToast(t *testing.T) {
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
	aliceContact, err := identity.ContactFromInvite(aliceService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("alice invite to contact: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bobService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("bob invite to contact: %v", err)
	}
	if err := aliceStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob contact: %v", err)
	}
	if err := bobStore.SaveContact(aliceContact); err != nil {
		t.Fatalf("save alice contact: %v", err)
	}

	model := New(Deps{
		Client:           stubClient{},
		Messaging:        bobService,
		Mailbox:          "bob",
		RecipientMailbox: "alice",
		RelayURL:         "ws://localhost:8080/ws",
	})

	batch, err := aliceService.EncryptOutgoing("bob", "hello bob")
	if err != nil {
		t.Fatalf("encrypt outgoing: %v", err)
	}
	if batch == nil || len(batch.Envelopes) == 0 {
		t.Fatal("expected outgoing envelopes")
	}

	model.handleProtocolMessage(protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &batch.Envelopes[0]})
	if toast, _ := model.Toast(); toast != "" {
		t.Fatalf("expected no toast for unchanged contact update, got %q", toast)
	}
	if model.peer.mailbox != "alice" {
		t.Fatalf("expected active peer to remain alice, got %q", model.peer.mailbox)
	}
}

func TestSidebarSelectionLoadsContactHistory(t *testing.T) {
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
	carolStore := store.NewClientStore(t.TempDir())
	carolService, _, err := messaging.New(carolStore, "carol")
	if err != nil {
		t.Fatalf("new carol service: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bobService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("bob invite to contact: %v", err)
	}
	bobContact.Verified = true
	if err := clientStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob contact: %v", err)
	}
	carolContact, err := identity.ContactFromInvite(carolService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("carol invite to contact: %v", err)
	}
	if err := clientStore.SaveContact(carolContact); err != nil {
		t.Fatalf("save carol contact: %v", err)
	}
	if err := service.SaveReceived("bob", "hello from bob", time.Now().UTC(), nil); err != nil {
		t.Fatalf("save received history: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(100, 20)
	if model.peer.mailbox != "" {
		t.Fatalf("expected no active chat, got %q", model.peer.mailbox)
	}
	if model.selectedIndex < 0 || model.selectedIndex >= len(model.contacts) {
		t.Fatalf("expected a contact to be selected, got index %d of %d", model.selectedIndex, len(model.contacts))
	}
	if selected := model.contacts[model.selectedIndex]; selected.IsRoom || selected.Mailbox != "bob" {
		t.Fatalf("expected bob (first direct contact) to be selected, got %+v", selected)
	}
	view := model.View()
	if !strings.Contains(view, "bob  verified") {
		t.Fatalf("expected verified contact in sidebar: %q", view)
	}
	if !strings.Contains(view, "carol  unverified") {
		t.Fatalf("expected unverified contact in sidebar: %q", view)
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd != nil {
		t.Fatal("expected no async command when opening selected chat")
	}
	if model.peer.mailbox != "bob" {
		t.Fatalf("expected bob to become active chat, got %q", model.peer.mailbox)
	}
	if !model.peer.verified {
		t.Fatal("expected active peer to be verified")
	}
	if len(model.msgs.items) != 1 || model.msgs.items[0].body != "hello from bob" {
		t.Fatalf("expected bob history to load, got %+v", model.msgs.items)
	}
	if !stringsContainsAny(model.msgs.rendered, "hello from bob") {
		t.Fatalf("expected rendered history to include body, got %+v", model.msgs.rendered)
	}
	view = model.View()
	if !strings.Contains(view, "bob") {
		t.Fatalf("expected peer name in conversation pane: %q", view)
	}
	if model.PeerFingerprint() != bobContact.Fingerprint() {
		t.Fatalf("expected peer fingerprint %q, got %q", bobContact.Fingerprint(), model.PeerFingerprint())
	}
	if !model.PeerVerified() {
		t.Fatalf("expected active peer to be verified")
	}
	if model.input.Placeholder != "Message bob" {
		t.Fatalf("unexpected input placeholder: %q", model.input.Placeholder)
	}
}

func TestEnterWithoutActiveChatPromptsForSidebarSelection(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.conn.connected = true
	model.conn.connecting = false
	model.input.SetValue("hi")

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if toast, _ := model.Toast(); toast != "select a contact from the sidebar first" {
		t.Fatalf("unexpected toast: %q", toast)
	}
	if model.input.Value() != "hi" {
		t.Fatalf("expected input to stay intact, got %q", model.input.Value())
	}
}

func TestCommandPaletteOpensAddContactModalAndEscClosesIt(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(100, 20)

	openPaletteCommand(t, model, "contact")
	if !model.addContact.open {
		t.Fatal("expected add contact modal to open")
	}
	view := model.View()
	if !strings.Contains(view, "Add Contact") {
		t.Fatalf("expected add contact modal in view: %q", view)
	}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	drainMsg(t, model, cmd)
	if model.addContact.open {
		t.Fatal("expected add contact modal to close")
	}
	if toast, _ := model.Toast(); toast != "add contact cancelled" {
		t.Fatalf("unexpected toast: %q", toast)
	}
}

func TestAddContactModalImportsRawInviteAndActivatesChat(t *testing.T) {
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
	code, err := invite.EncodeCode(bobService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: aliceService,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(100, 20)

	openPaletteCommand(t, model, "contact")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(code), Paste: true})
	// First ctrl+s parses the invite locally and shows a preview; no command.
	_, previewCmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if previewCmd != nil {
		t.Fatal("expected first ctrl+s to only show preview, no async command")
	}
	if model.addContact.preview == nil {
		t.Fatal("expected preview state to be populated after ctrl+s")
	}
	// Second ctrl+s commits the import.
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("expected import command")
	}
	drainMsg(t, model, cmd)

	if model.addContact.open {
		t.Fatal("expected add contact modal to close after import")
	}
	if model.peer.mailbox != "bob" {
		t.Fatalf("expected imported contact to become active chat, got %q", model.peer.mailbox)
	}
	if !model.peer.verified {
		t.Fatal("expected imported contact to be verified")
	}
	if toast, _ := model.Toast(); toast != "added verified contact bob" {
		t.Fatalf("unexpected toast: %q", toast)
	}
	view := model.View()
	if !strings.Contains(view, "bob  verified") {
		t.Fatalf("expected imported contact in sidebar: %q", view)
	}
	if model.PeerFingerprint() != bobService.Identity().Fingerprint() {
		t.Fatalf("expected peer fingerprint %q, got %q", bobService.Identity().Fingerprint(), model.PeerFingerprint())
	}
	if !model.PeerVerified() {
		t.Fatalf("expected peer to be verified")
	}
	contact, err := aliceStore.LoadContact("bob")
	if err != nil {
		t.Fatalf("load imported contact: %v", err)
	}
	if !contact.Verified {
		t.Fatal("expected imported contact to be saved as verified")
	}
}

func TestAddContactModalAcceptsVerboseInvitePaste(t *testing.T) {
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
	code, err := invite.EncodeCode(bobService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	pasted := "account: bob\nfingerprint: " + bobService.Identity().Fingerprint() + "\ninvite-code: " + code + "\n"

	model := New(Deps{
		Client:    stubClient{},
		Messaging: aliceService,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(100, 20)

	openPaletteCommand(t, model, "contact")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	// First ctrl+s parses; second ctrl+s commits.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if model.addContact.preview == nil {
		t.Fatal("expected verbose paste to parse into a preview")
	}
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("expected import command")
	}
	drainMsg(t, model, cmd)
	if model.peer.mailbox != "bob" {
		t.Fatalf("expected verbose invite paste to import bob, got %q", model.peer.mailbox)
	}
	if !model.peer.verified {
		t.Fatal("expected verbose invite import to verify contact")
	}
}

func TestAddContactModalShowsDecodeErrorsAndKeepsInput(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(100, 20)
	badInvite := "not a valid invite"

	openPaletteCommand(t, model, "contact")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(badInvite), Paste: true})
	// With the preview step, a bad paste surfaces the decode error synchronously
	// from the first ctrl+s — no async command, no preview state.
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil {
		t.Fatal("expected no command when invite fails to parse")
	}
	if model.addContact.preview != nil {
		t.Fatal("expected no preview when invite fails to parse")
	}

	if !model.addContact.open {
		t.Fatal("expected modal to stay open on error")
	}
	if model.addContact.value != badInvite {
		t.Fatalf("expected bad invite to remain for editing, got %q", model.addContact.value)
	}
	if !strings.Contains(model.addContact.error, "decode invite input") {
		t.Fatalf("expected decode error, got %q", model.addContact.error)
	}
	view := model.View()
	if !strings.Contains(view, "decode invite input") {
		t.Fatalf("expected inline error in modal: %q", view)
	}
}

func TestSuccessfulConnectMarksModelConnectedWithoutAckEvent(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})

	cmd := model.connectCmd()
	if cmd == nil {
		t.Fatal("expected connect command")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected connect result message")
	}
	updated, followup := model.Update(msg)
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if followup == nil {
		t.Fatal("expected wait-for-event follow-up command")
	}
	if !model.conn.connected {
		t.Fatal("expected model to be connected after successful connect")
	}
	if model.conn.connecting {
		t.Fatal("expected connecting state to clear")
	}
	if model.conn.status != "connected as alice" {
		t.Fatalf("unexpected status: %q", model.conn.status)
	}
	if model.input.Placeholder != "Select a contact to start chatting" {
		t.Fatalf("unexpected placeholder: %q", model.input.Placeholder)
	}
}

func TestBootstrapConnectFailureStopsReconnectLoop(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})

	err = fmt.Errorf("publish your signed relay directory entry before connecting: run `pando contact publish-directory --mailbox alice`")
	updated, cmd := model.Update(connectResultMsg{err: err})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd != nil {
		t.Fatal("expected no reconnect command for bootstrap failure")
	}
	if model.conn.connecting {
		t.Fatal("expected connecting state to clear")
	}
	if model.conn.connected {
		t.Fatal("expected model to remain disconnected")
	}
	if !model.conn.disconnected {
		t.Fatal("expected disconnected state")
	}
	if model.conn.status != err.Error() {
		t.Fatalf("unexpected status: %q", model.conn.status)
	}
	if model.conn.reconnectDelay != 0 {
		t.Fatalf("expected reconnect delay reset, got %s", model.conn.reconnectDelay)
	}
}

func TestUnauthorizedDeviceConnectFailureStopsReconnectLoop(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})

	err = fmt.Errorf("device is not authorized for this mailbox")
	_, cmd := model.Update(reconnectResultMsg{err: err})
	if cmd != nil {
		t.Fatal("expected no reconnect command for unauthorized device")
	}
	if model.conn.status != err.Error() {
		t.Fatalf("unexpected status: %q", model.conn.status)
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
	model.conn.connected = true
	model.conn.connecting = false
	model.input.SetValue("/send-photo " + photoPath)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd != nil {
		t.Fatal("expected attachment to queue without sending")
	}
	if !model.HasPendingAttachment() {
		t.Fatal("expected pending photo attachment")
	}
	if !strings.Contains(model.PendingAttachmentLabel(), "photo.png") {
		t.Fatalf("unexpected pending attachment label: %q", model.PendingAttachmentLabel())
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected queued photo to send on enter")
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
	for _, message := range model.msgs.rendered {
		if strings.Contains(message, "photo sent: photo.png") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sent photo message in history: %+v", model.msgs.rendered)
	}
}

func TestSendPhotoCommandAcceptsQuotedPath(t *testing.T) {
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

	photoPath := filepath.Join(t.TempDir(), "photo with spaces.png")
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
	model.conn.connected = true
	model.conn.connecting = false
	model.input.SetValue(`/send-photo "` + photoPath + `"`)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd != nil {
		t.Fatal("expected attachment to queue without sending")
	}
	if !model.HasPendingAttachment() {
		t.Fatal("expected pending photo attachment")
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected queued photo to send on enter")
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
}

func TestSendVoiceCommandQueuesAttachmentBatch(t *testing.T) {
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

	voicePath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(voicePath, mustVoiceBytes(), 0o600); err != nil {
		t.Fatalf("write voice note: %v", err)
	}

	client := &recordingClient{}
	model := New(Deps{
		Client:           client,
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.conn.connected = true
	model.conn.connecting = false
	model.input.SetValue("/send-voice " + voicePath)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd != nil {
		t.Fatal("expected attachment to queue without sending")
	}
	if !model.HasPendingAttachment() {
		t.Fatal("expected pending voice attachment")
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected queued voice to send on enter")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected send result message")
	}
	_, _ = model.Update(msg)
	if len(client.sent) == 0 {
		t.Fatal("expected voice send to produce envelopes")
	}
	if model.input.Value() != "" {
		t.Fatalf("expected input to clear after send, got %q", model.input.Value())
	}
	found := false
	for _, message := range model.msgs.rendered {
		if strings.Contains(message, "voice note sent: clip.wav") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sent voice message in history: %+v", model.msgs.rendered)
	}
}

func TestSendVoiceCommandAcceptsEscapedSpacesInPath(t *testing.T) {
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

	voicePath := filepath.Join(t.TempDir(), "voice memo.wav")
	if err := os.WriteFile(voicePath, mustVoiceBytes(), 0o600); err != nil {
		t.Fatalf("write voice note: %v", err)
	}

	client := &recordingClient{}
	model := New(Deps{
		Client:           client,
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.conn.connected = true
	model.conn.connecting = false
	model.input.SetValue("/send-voice " + strings.ReplaceAll(voicePath, " ", `\ `))

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd != nil {
		t.Fatal("expected attachment to queue without sending")
	}
	if !model.HasPendingAttachment() {
		t.Fatal("expected pending voice attachment")
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected queued voice to send on enter")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected send result message")
	}
	_, _ = model.Update(msg)
	if len(client.sent) == 0 {
		t.Fatal("expected voice send to produce envelopes")
	}
	if model.input.Value() != "" {
		t.Fatalf("expected input to clear after send, got %q", model.input.Value())
	}
}

func TestSendFileCommandQueuesAttachmentBatch(t *testing.T) {
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

	filePath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(filePath, []byte("hello from file"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	client := &recordingClient{}
	model := New(Deps{
		Client:           client,
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.conn.connected = true
	model.conn.connecting = false
	model.input.SetValue("/send-file " + filePath)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd != nil {
		t.Fatal("expected attachment to queue without sending")
	}
	if !model.HasPendingAttachment() {
		t.Fatal("expected pending file attachment")
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected queued file to send on enter")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected send result message")
	}
	_, _ = model.Update(msg)
	if len(client.sent) == 0 {
		t.Fatal("expected file send to produce envelopes")
	}
	if model.input.Value() != "" {
		t.Fatalf("expected input to clear after send, got %q", model.input.Value())
	}
	found := false
	for _, message := range model.msgs.rendered {
		if strings.Contains(message, "file sent: notes.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sent file message in history: %+v", model.msgs.rendered)
	}
}

func TestCtrlOOpensFilePickerAndSelectsFile(t *testing.T) {
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

	pickerDir := t.TempDir()
	filePath := filepath.Join(pickerDir, "draft.txt")
	if err := os.WriteFile(filePath, []byte("draft body"), 0o600); err != nil {
		t.Fatalf("write picker file: %v", err)
	}

	client := &recordingClient{}
	model := New(Deps{
		Client:           client,
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.conn.connected = true
	model.conn.connecting = false
	model.SetSize(100, 20)
	model.filePicker.dir = pickerDir

	updated, cmd := openPaletteCommand(t, model, "attach")
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd != nil {
		t.Fatal("expected modal open without async command")
	}
	if !model.filePicker.open {
		t.Fatal("expected file picker to open")
	}
	if !strings.Contains(model.View(), "Attach File") {
		t.Fatalf("expected modal title in view: %q", model.View())
	}

	// The first entry is the synthetic ".." — step down to reach draft.txt.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected picker selection to queue attachment")
	}
	if model.filePicker.open {
		t.Fatal("expected file picker to close after selecting a file")
	}
	if !model.HasPendingAttachment() {
		t.Fatal("expected picker selection to create pending attachment")
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected queued picker attachment to send on enter")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected send result message")
	}
	_, _ = model.Update(msg)
	if len(client.sent) == 0 {
		t.Fatal("expected picker send to produce envelopes")
	}
	if model.filePicker.open {
		t.Fatal("expected picker to close after selecting a file")
	}
	found := false
	for _, message := range model.msgs.rendered {
		if strings.Contains(message, "file sent: draft.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected picker file message in history: %+v", model.msgs.rendered)
	}
}

func TestFilePickerNavigatesDirectoriesAndCancels(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	pickerRoot := t.TempDir()
	childDir := filepath.Join(pickerRoot, "photos")
	if err := os.Mkdir(childDir, 0o700); err != nil {
		t.Fatalf("mkdir child dir: %v", err)
	}

	model := New(Deps{
		Client:           stubClient{},
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.conn.connected = true
	model.conn.connecting = false
	model.SetSize(100, 20)
	model.filePicker.dir = pickerRoot

	openPaletteCommand(t, model, "attach")
	if !model.filePicker.open {
		t.Fatal("expected file picker to open")
	}
	// First entry is the synthetic ".." parent pointer.
	if first := model.selectedFilePickerEntry(); first == nil || !first.IsParent {
		t.Fatalf("expected first picker entry to be the parent pointer, got %+v", first)
	}
	// Move past ".." to reach the photos directory.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.selectedFilePickerEntry() == nil || !model.selectedFilePickerEntry().IsDir {
		t.Fatalf("expected second picker entry to be the child directory, got %+v", model.selectedFilePickerEntry())
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.filePicker.dir != childDir {
		t.Fatalf("expected picker to enter child dir, got %q", model.filePicker.dir)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if model.filePicker.dir != pickerRoot {
		t.Fatalf("expected picker to return to root dir, got %q", model.filePicker.dir)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.filePicker.open {
		t.Fatal("expected file picker to close on escape")
	}
}

func TestFilePickerVisibleEntriesStayWithinWindow(t *testing.T) {
	model := &Model{}
	for i := 0; i < 20; i++ {
		model.filePicker.entries = append(model.filePicker.entries, filePickerEntry{Name: fmt.Sprintf("entry-%02d", i)})
	}
	model.filePicker.selected = 10

	visible, hiddenAbove, hiddenBelow := model.filePickerVisibleEntries(5)
	if !hiddenAbove {
		t.Fatal("expected hidden entries above the visible window")
	}
	if !hiddenBelow {
		t.Fatal("expected hidden entries below the visible window")
	}
	if len(visible) != 5 {
		t.Fatalf("expected 5 visible entries, got %d", len(visible))
	}
	if visible[0].entry.Name != "entry-08" {
		t.Fatalf("expected window to start near selection, got %q", visible[0].entry.Name)
	}
	if visible[len(visible)-1].entry.Name != "entry-12" {
		t.Fatalf("expected window to end at final entry, got %q", visible[len(visible)-1].entry.Name)
	}
	foundSelected := false
	for _, entry := range visible {
		if entry.index == model.filePicker.selected {
			foundSelected = true
			break
		}
	}
	if !foundSelected {
		t.Fatal("expected selected entry to stay visible")
	}
}

func TestFilePickerFiltersEntriesFromTypedQuery(t *testing.T) {
	model := &Model{}
	model.filePicker = newFilePickerModel()
	model.filePicker.open = true
	_ = model.filePicker.filter.Focus()
	model.filePicker.SetSize(80, 20)
	model.filePicker.entries = []filePickerEntry{{Name: "..", IsDir: true, IsParent: true}, {Name: "draft.txt"}, {Name: "photo.png"}}

	updatedPicker, _ := model.filePicker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dr")})
	model.filePicker = updatedPicker

	filtered := model.filePicker.filteredEntries()
	if len(filtered) != 1 || filtered[0].Name != "draft.txt" {
		t.Fatalf("expected only draft.txt after filtering, got %+v", filtered)
	}
	view := model.filePicker.View()
	if !strings.Contains(view, "draft.txt") {
		t.Fatalf("expected filtered view to show draft.txt: %q", view)
	}
	if strings.Contains(view, "photo.png") {
		t.Fatalf("expected filtered view to hide photo.png: %q", view)
	}
}

func TestFilePickerBackspaceEditsFilterBeforeNavigatingUp(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "docs")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatalf("mkdir child dir: %v", err)
	}

	model := &Model{}
	model.filePicker = newFilePickerModel()
	if err := model.filePicker.openAt(child); err != nil {
		t.Fatalf("open picker at child dir: %v", err)
	}

	updatedPicker, _ := model.filePicker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("doc")})
	model.filePicker = updatedPicker
	updatedPicker, _ = model.filePicker.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model.filePicker = updatedPicker
	if model.filePicker.dir != child {
		t.Fatalf("expected picker to stay in child dir while editing filter, got %q", model.filePicker.dir)
	}
	if model.filePicker.filter.Value() != "do" {
		t.Fatalf("expected backspace to edit filter first, got %q", model.filePicker.filter.Value())
	}

	updatedPicker, _ = model.filePicker.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model.filePicker = updatedPicker
	updatedPicker, _ = model.filePicker.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model.filePicker = updatedPicker
	if model.filePicker.filter.Value() != "" {
		t.Fatalf("expected filter to be cleared, got %q", model.filePicker.filter.Value())
	}
	updatedPicker, _ = model.filePicker.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model.filePicker = updatedPicker
	if model.filePicker.dir != root {
		t.Fatalf("expected final backspace to navigate to parent dir, got %q", model.filePicker.dir)
	}
}

func TestTypingIndicatorRendersAnimatesAndExpires(t *testing.T) {
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
	bobContact, err := identity.ContactFromInvite(bobService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("bob invite to contact: %v", err)
	}
	aliceContact, err := identity.ContactFromInvite(aliceService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("alice invite to contact: %v", err)
	}
	if err := aliceStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob contact: %v", err)
	}
	if err := bobStore.SaveContact(aliceContact); err != nil {
		t.Fatalf("save alice contact: %v", err)
	}

	model := New(Deps{
		Client:           stubClient{},
		Messaging:        aliceService,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.SetSize(100, 20)
	envelopes, err := bobService.TypingEnvelopes("alice", messaging.TypingStateActive)
	if err != nil {
		t.Fatalf("typing envelopes: %v", err)
	}
	model.handleProtocolMessage(protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &envelopes[0]})
	footer := strings.Join(model.FooterSegments(), "    ")
	if !strings.Contains(footer, "bob is typing") {
		t.Fatalf("expected typing indicator in footer: %q", footer)
	}
	if strings.Contains(footer, "enter send") {
		t.Fatalf("expected typing indicator to take priority over key hints: %q", footer)
	}

	_, _ = model.Update(typingTickMsg(time.Now().UTC().Add(typingAnimationInterval)))
	footer = strings.Join(model.FooterSegments(), "    ")
	if !strings.Contains(footer, "bob is typing") {
		t.Fatalf("expected animated typing indicator in footer: %q", footer)
	}
	// Verify spinner actually advanced (frame changed)
	footer2 := strings.Join(model.FooterSegments(), "    ")
	_, _ = model.Update(typingTickMsg(time.Now().UTC().Add(2 * typingAnimationInterval)))
	footer3 := strings.Join(model.FooterSegments(), "    ")
	if footer2 == footer3 {
		t.Fatalf("expected typing indicator to animate between frames, got same footer: %q", footer3)
	}

	model.typing.peerExpiresAt = time.Now().UTC().Add(-time.Second)
	_, _ = model.Update(typingTickMsg(time.Now().UTC()))
	footer = strings.Join(model.FooterSegments(), "    ")
	if strings.Contains(footer, "bob is typing") {
		t.Fatalf("expected typing indicator to expire: %q", footer)
	}
}

func TestTypingIndicatorDoesNotChangeViewportHeight(t *testing.T) {
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
	bobContact, err := identity.ContactFromInvite(bobService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("bob invite to contact: %v", err)
	}
	aliceContact, err := identity.ContactFromInvite(aliceService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("alice invite to contact: %v", err)
	}
	if err := aliceStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob contact: %v", err)
	}
	if err := bobStore.SaveContact(aliceContact); err != nil {
		t.Fatalf("save alice contact: %v", err)
	}

	model := New(Deps{
		Client:           stubClient{},
		Messaging:        aliceService,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.SetSize(100, 20)
	heightBefore := model.viewport.Height

	envelopes, err := bobService.TypingEnvelopes("alice", messaging.TypingStateActive)
	if err != nil {
		t.Fatalf("typing envelopes: %v", err)
	}
	model.handleProtocolMessage(protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &envelopes[0]})
	if model.viewport.Height != heightBefore {
		t.Fatalf("expected typing indicator to keep viewport height stable, before=%d after=%d", heightBefore, model.viewport.Height)
	}

	model.typing.peerExpiresAt = time.Now().UTC().Add(-time.Second)
	_, _ = model.Update(typingTickMsg(time.Now().UTC()))
	if model.viewport.Height != heightBefore {
		t.Fatalf("expected typing expiry to keep viewport height stable, before=%d after=%d", heightBefore, model.viewport.Height)
	}
}

func TestHandleInputActivitySendsTypingWithoutSavingHistory(t *testing.T) {
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

	client := &recordingClient{}
	model := New(Deps{
		Client:           client,
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.conn.connected = true
	model.conn.connecting = false

	cmd := model.handleInputActivity("", "h")
	if cmd == nil {
		t.Fatal("expected typing start command")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected typing result message")
	}
	_, _ = model.Update(msg)
	if len(client.sent) == 0 {
		t.Fatal("expected typing indicator envelope to be sent")
	}
	history, err := service.History("bob")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected no saved history for typing indicator: %+v", history)
	}

	cmd = model.handleInputActivity("h", "")
	if cmd == nil {
		t.Fatal("expected typing stop command")
	}
	msg = cmd()
	if msg == nil {
		t.Fatal("expected typing stop result message")
	}
	_, _ = model.Update(msg)
	if len(client.sent) < 2 {
		t.Fatalf("expected idle typing envelope after clearing input, got %d sends", len(client.sent))
	}
	if model.typing.localSent {
		t.Fatal("expected local typing state to reset")
	}
}

func TestPushToastExpiresOnNextTypingTick(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})

	model.pushToast("hello", ToastInfo)
	if txt, level := model.Toast(); txt != "hello" || level != ToastInfo {
		t.Fatalf("unexpected toast: %q level=%d", txt, level)
	}

	// A tick that arrives before the toast expires must preserve it.
	model.Update(typingTickMsg(model.ui.toast.expiresAt.Add(-1 * time.Millisecond)))
	if txt, _ := model.Toast(); txt != "hello" {
		t.Fatalf("expected toast to remain before expiry, got %q", txt)
	}
	// A tick at or after the expiry clears it.
	model.Update(typingTickMsg(model.ui.toast.expiresAt.Add(1 * time.Millisecond)))
	if txt, _ := model.Toast(); txt != "" {
		t.Fatalf("expected toast cleared after expiry, got %q", txt)
	}
}

func TestConnectionStateReflectsFlags(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})

	if got := model.ConnectionState(); got != ConnConnecting {
		t.Fatalf("expected ConnConnecting, got %v", got)
	}
	model.markConnected("connected as alice")
	if got := model.ConnectionState(); got != ConnConnected {
		t.Fatalf("expected ConnConnected, got %v", got)
	}
	model.conn.disconnected = true
	model.conn.connected = false
	if got := model.ConnectionState(); got != ConnDisconnected {
		t.Fatalf("expected ConnDisconnected, got %v", got)
	}
	model.conn.authFailed = true
	if got := model.ConnectionState(); got != ConnAuthFailed {
		t.Fatalf("expected ConnAuthFailed, got %v", got)
	}
}

func TestUnreadCountTracksOffChatMessagesAndClearsOnOpen(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
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
	bobContact.Verified = true
	if err := clientStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob contact: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: aliceService,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(100, 20)

	// Two unread arrivals while bob isn't the active chat.
	model.markUnread("bob")
	model.markUnread("bob")
	if got := model.Unread("bob"); got != 2 {
		t.Fatalf("expected 2 unread from bob, got %d", got)
	}
	view := model.View()
	if !strings.Contains(view, "●2") {
		t.Fatalf("expected unread badge ●2 in sidebar: %q", view)
	}

	// Opening bob's chat clears the badge.
	model.selectContact("bob")
	if !model.activateSelectedContact() {
		t.Fatal("expected bob chat to activate")
	}
	if got := model.Unread("bob"); got != 0 {
		t.Fatalf("expected unread cleared after opening chat, got %d", got)
	}
	if strings.Contains(model.View(), "●2") {
		t.Fatalf("expected unread badge cleared from sidebar: %q", model.View())
	}

	// markUnread is a no-op for the active chat.
	model.markUnread("bob")
	if got := model.Unread("bob"); got != 0 {
		t.Fatalf("expected markUnread on active chat to be no-op, got %d", got)
	}
}

func TestRenderMessagesGroupsByConsecutiveSender(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	model := New(Deps{
		Client:           stubClient{},
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.SetSize(120, 20)
	model.peer.fingerprint = "abcd1234abcd1234"

	t0 := time.Date(2026, 4, 19, 12, 34, 0, 0, time.UTC)
	model.msgs.items = []messageItem{
		{direction: "outbound", sender: "alice", body: "hi", timestamp: t0, messageID: "m1", status: statusDelivered},
		{direction: "outbound", sender: "alice", body: "one more thing", timestamp: t0.Add(20 * time.Second), messageID: "m2", status: statusSent},
		{direction: "inbound", sender: "bob", body: "got it", timestamp: t0.Add(time.Minute)},
	}
	model.renderMessages()

	// Expected shape: "you · HH:MM" header once, two body lines; then "bob · HH:MM" header, one body line.
	joined := strings.Join(model.msgs.rendered, "\n")
	youHeaders := strings.Count(joined, "you")
	bobHeaders := strings.Count(joined, "bob")
	if youHeaders != 1 {
		t.Fatalf("expected exactly one 'you' group header, got %d in %q", youHeaders, joined)
	}
	if bobHeaders != 1 {
		t.Fatalf("expected exactly one 'bob' group header, got %d in %q", bobHeaders, joined)
	}
	if !strings.Contains(joined, "hi") || !strings.Contains(joined, "one more thing") || !strings.Contains(joined, "got it") {
		t.Fatalf("expected all message bodies in rendered output, got %q", joined)
	}
	if !strings.Contains(joined, style.GlyphDeliveryDelivered) {
		t.Fatalf("expected delivered glyph %q in output: %q", style.GlyphDeliveryDelivered, joined)
	}
}

func TestRenderMessagesFlipsTickOnDeliveryAck(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	model := New(Deps{
		Client:           stubClient{},
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.SetSize(120, 20)

	model.msgs.items = []messageItem{
		{direction: "outbound", sender: "alice", body: "hi", timestamp: time.Now(), messageID: "m1", status: statusSent},
	}
	model.renderMessages()
	before := strings.Join(model.msgs.rendered, "\n")
	if !strings.Contains(before, style.GlyphDeliverySent) {
		t.Fatalf("expected sent glyph before ack: %q", before)
	}

	if ok := model.updateMessageStatus("m1", statusDelivered); !ok {
		t.Fatal("updateMessageStatus returned false for known messageID")
	}
	after := strings.Join(model.msgs.rendered, "\n")
	if !strings.Contains(after, style.GlyphDeliveryDelivered) {
		t.Fatalf("expected delivered glyph after ack: %q", after)
	}
}

func TestRenderMessagesUsesLocalTimezoneForHeaders(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC-7", -7*60*60)
	defer func() {
		time.Local = oldLocal
	}()

	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	model := New(Deps{
		Client:           stubClient{},
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.SetSize(120, 20)

	model.msgs.items = []messageItem{{
		direction: "inbound",
		sender:    "bob",
		body:      "hello",
		timestamp: time.Date(2026, 4, 19, 12, 34, 0, 0, time.UTC),
	}}
	model.renderMessages()

	joined := strings.Join(model.msgs.rendered, "\n")
	if !strings.Contains(joined, "5:34AM") {
		t.Fatalf("expected local timezone timestamp in rendered output, got %q", joined)
	}
	if strings.Contains(joined, "12:34PM") {
		t.Fatalf("expected rendered output to avoid raw UTC timestamp, got %q", joined)
	}
}

func TestHelpOverlayTogglesWithQuestionMark(t *testing.T) {
	model := newHelpTestModel(t)

	// "?" with empty input opens the overlay.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if !model.helpOpen {
		t.Fatal("expected ? to open the help overlay")
	}
	if !strings.Contains(model.View(), "Help") {
		t.Fatalf("expected help title in view: %q", model.View())
	}

	// Esc closes it.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.helpOpen {
		t.Fatal("expected esc to close the help overlay")
	}

	// Typing "?" in the input while editing a message must not open help.
	model.input.SetValue("draft")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if model.helpOpen {
		t.Fatal("expected ? to be ignored while typing a message")
	}
}

func TestCtrlPOpensCommandPalette(t *testing.T) {
	model := newHelpTestModel(t)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if cmd != nil {
		drainMsg(t, model, cmd)
	}
	if !model.commandPalette.open {
		t.Fatal("expected ctrl+p to open command palette")
	}
	if !strings.Contains(model.View(), "Command Palette") {
		t.Fatalf("expected command palette in view: %q", model.View())
	}
}

func TestCommandPaletteFiltersByAlias(t *testing.T) {
	model := newHelpTestModel(t)
	openPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("appearance")})
	items := model.commandPalette.visibleItems(false)
	if len(items) != 1 {
		t.Fatalf("expected one visible command after alias filter, got %d", len(items))
	}
	if items[0].item.id != string(commandPaletteCommandThemes) {
		t.Fatalf("expected themes command after alias filter, got %q", items[0].item.id)
	}
}

func TestCommandPaletteAppliesAndPersistsTheme(t *testing.T) {
	prev := style.Current()
	t.Cleanup(func() { style.Apply(prev) })

	savedTheme := ""
	model := newHelpTestModel(t)
	model.commandPalette.deps.saveTheme = func(name string) error {
		savedTheme = name
		return nil
	}

	if style.Current().Name != style.DefaultThemeName {
		style.Apply(style.Themes[style.DefaultThemeName])
	}
	openPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("theme")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.commandPalette.open || model.commandPalette.mode != commandPaletteModeThemes {
		t.Fatal("expected enter on themes command to open theme submenu")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("classic")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if style.Current().Name != "classic" {
		t.Fatalf("expected active theme classic, got %q", style.Current().Name)
	}
	if savedTheme != "classic" {
		t.Fatalf("expected theme save callback for classic, got %q", savedTheme)
	}
	if model.commandPalette.open {
		t.Fatal("expected theme selection to close command palette")
	}
}

func TestCommandPaletteEscReturnsFromThemeSubmenu(t *testing.T) {
	model := newHelpTestModel(t)
	openPalette(t, model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("theme")})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.commandPalette.mode != commandPaletteModeThemes {
		t.Fatal("expected theme submenu to open")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !model.commandPalette.open {
		t.Fatal("expected esc in theme submenu to keep palette open")
	}
	if model.commandPalette.mode != commandPaletteModeRoot {
		t.Fatal("expected esc in theme submenu to return to root palette")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.commandPalette.open {
		t.Fatal("expected esc in root palette to close it")
	}
}

func TestTabTogglesFocus(t *testing.T) {
	model := newHelpTestModel(t)

	if model.ui.focus != focusSidebar {
		t.Fatalf("expected initial focus on sidebar when no chat is open, got %v", model.ui.focus)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if model.ui.focus != focusChat {
		t.Fatalf("expected tab to move focus to chat, got %v", model.ui.focus)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if model.ui.focus != focusSidebar {
		t.Fatalf("expected tab to move focus back to sidebar, got %v", model.ui.focus)
	}
}

func TestPendingIncomingRendersJumpPillAndClearsOnEnd(t *testing.T) {
	model := newHelpTestModel(t)
	model.peer.mailbox = "bob"
	model.peer.fingerprint = "abcd1234abcd1234"
	model.SetSize(100, 20)

	// Seed a long conversation so there is room to scroll up.
	for i := 0; i < 40; i++ {
		model.msgs.items = append(model.msgs.items, messageItem{
			direction: "inbound",
			sender:    "bob",
			body:      fmt.Sprintf("line %d", i),
			timestamp: time.Now(),
		})
	}
	model.renderMessages()
	model.syncViewportToBottom()

	// Scroll up so subsequent arrivals don't auto-snap to bottom.
	model.viewport.SetYOffset(0)
	if model.viewport.AtBottom() {
		t.Fatal("precondition: viewport should be scrolled up")
	}

	model.appendMessageItem(messageItem{
		direction: "inbound", sender: "bob", body: "new 1", timestamp: time.Now(),
	})
	model.appendMessageItem(messageItem{
		direction: "inbound", sender: "bob", body: "new 2", timestamp: time.Now(),
	})
	if model.msgs.pendingIncoming != 2 {
		t.Fatalf("expected pending=2 after two inbound while scrolled up, got %d", model.msgs.pendingIncoming)
	}
	if !strings.Contains(model.View(), "2 new") {
		t.Fatalf("expected '2 new' pill in view: %q", model.View())
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if model.msgs.pendingIncoming != 0 {
		t.Fatalf("expected end-key to clear pending, got %d", model.msgs.pendingIncoming)
	}
	if !model.viewport.AtBottom() {
		t.Fatal("expected end-key to scroll viewport to bottom")
	}
}

func TestWelcomeCardShownWhenNoContacts(t *testing.T) {
	model := newHelpTestModel(t)
	view := model.View()
	if !strings.Contains(view, "Welcome to Pando") {
		t.Fatalf("expected welcome card on empty contact list: %q", view)
	}
	if !strings.Contains(view, "share your code") {
		t.Fatalf("expected welcome card step 1 in view: %q", view)
	}
}

func TestManifestoCardShownWhenNoChatSelected(t *testing.T) {
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

	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(120, 20)

	view := model.View()
	if !strings.Contains(view, "Why this exists") {
		t.Fatalf("expected manifesto card title in view: %q", view)
	}
	if !strings.Contains(view, "the right to a private conversation is not a feature") {
		t.Fatalf("expected manifesto quote in view: %q", view)
	}
	if !strings.Contains(view, "pando exists because your conversations are nobody's business but yours") {
		t.Fatalf("expected updated manifesto line in view: %q", view)
	}
	if !strings.Contains(view, "Pick a contact from the sidebar, or press ctrl+p to add one.") {
		t.Fatalf("expected action hint in view: %q", view)
	}
}

func TestPeerDetailDrawerToggle(t *testing.T) {
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
	bobContact.Verified = true
	if err := clientStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob: %v", err)
	}
	model := New(Deps{
		Client:           stubClient{},
		Messaging:        aliceService,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.SetSize(120, 24)

	openPaletteCommand(t, model, "detail")
	if !model.peerDetailOpen {
		t.Fatal("expected palette command to open peer detail")
	}
	view := model.View()
	if !strings.Contains(view, "Peer detail") {
		t.Fatalf("expected peer detail title in view: %q", view)
	}
	// Full formatted fingerprint should appear.
	if !strings.Contains(view, style.FormatFingerprint(bobContact.Fingerprint())) {
		t.Fatalf("expected formatted fingerprint in drawer: %q", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.peerDetailOpen {
		t.Fatal("expected esc to close the drawer")
	}
}

func TestFilePickerRendersSizesAndParentEntry(t *testing.T) {
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(file, []byte("hello, world\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	model := New(Deps{
		Client:           stubClient{},
		Messaging:        service,
		Mailbox:          "alice",
		RecipientMailbox: "bob",
		RelayURL:         "ws://localhost:8080/ws",
	})
	model.conn.connected = true
	model.conn.connecting = false
	model.SetSize(100, 20)
	model.filePicker.dir = dir

	openPaletteCommand(t, model, "attach")
	if !model.filePicker.open {
		t.Fatal("expected picker to open")
	}

	// First entry must be the parent pointer; note.txt must follow with its size.
	entries := model.filePicker.entries
	if len(entries) < 2 {
		t.Fatalf("expected picker to include .. + note.txt, got %+v", entries)
	}
	if !entries[0].IsParent || entries[0].Name != ".." {
		t.Fatalf("expected first entry to be the '..' parent pointer, got %+v", entries[0])
	}
	if entries[1].Name != "note.txt" || entries[1].Size == 0 {
		t.Fatalf("expected note.txt with non-zero size, got %+v", entries[1])
	}

	view := model.View()
	if !strings.Contains(view, "..") || !strings.Contains(view, "note.txt") {
		t.Fatalf("expected picker view to show .. and note.txt: %q", view)
	}
	if !strings.Contains(view, "B") && !strings.Contains(view, "KB") {
		t.Fatalf("expected a file size label in view: %q", view)
	}
}

func newHelpTestModel(t *testing.T) *Model {
	t.Helper()
	clientStore := store.NewClientStore(t.TempDir())
	service, _, err := messaging.New(clientStore, "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(120, 20)
	return model
}

func openPalette(t *testing.T, model *Model) {
	t.Helper()
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	drainMsg(t, model, cmd)
	if !model.commandPalette.open {
		t.Fatal("expected command palette to be open")
	}
}

func openPaletteCommand(t *testing.T, model *Model, query string) (*Model, tea.Cmd) {
	t.Helper()
	openPalette(t, model)
	if query != "" {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(query)})
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return updated, cmd
}

func stringsContainsAny(lines []string, needle string) bool {
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

func mustPhotoBytes(t *testing.T) []byte {
	t.Helper()
	bytes, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7Zl9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode photo bytes: %v", err)
	}
	return bytes
}

func mustVoiceBytes() []byte {
	return []byte{
		'R', 'I', 'F', 'F', 0x24, 0x08, 0x00, 0x00,
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ', 0x10, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x01, 0x00,
		0x40, 0x1f, 0x00, 0x00,
		0x80, 0x3e, 0x00, 0x00,
		0x02, 0x00, 0x10, 0x00,
		'd', 'a', 't', 'a', 0x00, 0x08, 0x00, 0x00,
	}
}
