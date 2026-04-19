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
	if len(model.messages) < 1 {
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
	if len(model.messages) < 1 {
		t.Fatalf("expected local history to remain visible, got %d messages", len(model.messages))
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
	if err := service.SaveReceived("bob", "hello from bob", time.Now().UTC()); err != nil {
		t.Fatalf("save received history: %v", err)
	}

	model := New(Deps{
		Client:    stubClient{},
		Messaging: service,
		Mailbox:   "alice",
		RelayURL:  "ws://localhost:8080/ws",
	})
	model.SetSize(100, 20)
	if model.recipientMailbox != "" {
		t.Fatalf("expected no active chat, got %q", model.recipientMailbox)
	}
	if model.selectedIndex != 0 {
		t.Fatalf("expected first contact to be selected, got %d", model.selectedIndex)
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
	if model.recipientMailbox != "bob" {
		t.Fatalf("expected bob to become active chat, got %q", model.recipientMailbox)
	}
	if !model.peerVerified {
		t.Fatal("expected active peer to be verified")
	}
	if len(model.messages) != 1 || !strings.Contains(model.messages[0], "hello from bob") {
		t.Fatalf("expected bob history to load, got %+v", model.messages)
	}
	view = model.View()
	if !strings.Contains(view, "fingerprint "+bobContact.Fingerprint()+"  verified") {
		t.Fatalf("expected active fingerprint header: %q", view)
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
	model.connected = true
	model.connecting = false
	model.input.SetValue("hi")

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.status != "select a contact from the sidebar first" {
		t.Fatalf("unexpected status: %q", model.status)
	}
	if model.input.Value() != "hi" {
		t.Fatalf("expected input to stay intact, got %q", model.input.Value())
	}
}

func TestCtrlNOpensAndEscClosesAddContactModal(t *testing.T) {
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if !model.addContactOpen {
		t.Fatal("expected add contact modal to open")
	}
	view := model.View()
	if !strings.Contains(view, "Add Contact") {
		t.Fatalf("expected add contact modal in view: %q", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.addContactOpen {
		t.Fatal("expected add contact modal to close")
	}
	if model.status != "add contact cancelled" {
		t.Fatalf("unexpected status: %q", model.status)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(code), Paste: true})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("expected import command")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected import result message")
	}
	_, _ = model.Update(msg)

	if model.addContactOpen {
		t.Fatal("expected add contact modal to close after import")
	}
	if model.recipientMailbox != "bob" {
		t.Fatalf("expected imported contact to become active chat, got %q", model.recipientMailbox)
	}
	if !model.peerVerified {
		t.Fatal("expected imported contact to be verified")
	}
	if model.status != "added verified contact bob" {
		t.Fatalf("unexpected status: %q", model.status)
	}
	view := model.View()
	if !strings.Contains(view, "bob  verified") {
		t.Fatalf("expected imported contact in sidebar: %q", view)
	}
	if !strings.Contains(view, "fingerprint "+bobService.Identity().Fingerprint()+"  verified") {
		t.Fatalf("expected verified fingerprint header: %q", view)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("expected import command")
	}
	_, _ = model.Update(cmd())
	if model.recipientMailbox != "bob" {
		t.Fatalf("expected verbose invite paste to import bob, got %q", model.recipientMailbox)
	}
	if !model.peerVerified {
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(badInvite), Paste: true})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("expected import command")
	}
	_, _ = model.Update(cmd())

	if !model.addContactOpen {
		t.Fatal("expected modal to stay open on error")
	}
	if model.addContactValue != badInvite {
		t.Fatalf("expected bad invite to remain for editing, got %q", model.addContactValue)
	}
	if !strings.Contains(model.addContactError, "decode invite input") {
		t.Fatalf("expected decode error, got %q", model.addContactError)
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
	if !model.connected {
		t.Fatal("expected model to be connected after successful connect")
	}
	if model.connecting {
		t.Fatal("expected connecting state to clear")
	}
	if model.status != "connected to relay, subscribed as alice" {
		t.Fatalf("unexpected status: %q", model.status)
	}
	if model.input.Placeholder != "Select a contact to start chatting" {
		t.Fatalf("unexpected placeholder: %q", model.input.Placeholder)
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
		if strings.Contains(message, "photo sent: photo.png") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sent photo message in history: %+v", model.messages)
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
	model.connected = true
	model.connecting = false
	model.input.SetValue(`/send-photo "` + photoPath + `"`)

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
	model.connected = true
	model.connecting = false
	model.input.SetValue("/send-voice " + voicePath)

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
		t.Fatal("expected voice send to produce envelopes")
	}
	if model.input.Value() != "" {
		t.Fatalf("expected input to clear after send, got %q", model.input.Value())
	}
	found := false
	for _, message := range model.messages {
		if strings.Contains(message, "voice note sent: clip.wav") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sent voice message in history: %+v", model.messages)
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
	model.connected = true
	model.connecting = false
	model.input.SetValue("/send-voice " + strings.ReplaceAll(voicePath, " ", `\ `))

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
	model.connected = true
	model.connecting = false
	model.input.SetValue("/send-file " + filePath)

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
		t.Fatal("expected file send to produce envelopes")
	}
	if model.input.Value() != "" {
		t.Fatalf("expected input to clear after send, got %q", model.input.Value())
	}
	found := false
	for _, message := range model.messages {
		if strings.Contains(message, "file sent: notes.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sent file message in history: %+v", model.messages)
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
	model.connected = true
	model.connecting = false
	model.SetSize(100, 20)
	model.filePickerDir = pickerDir

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if updated != model {
		t.Fatal("expected model to update in place")
	}
	if cmd != nil {
		t.Fatal("expected modal open without async command")
	}
	if !model.filePickerOpen {
		t.Fatal("expected file picker to open")
	}
	if !strings.Contains(model.View(), "Attach File") {
		t.Fatalf("expected modal title in view: %q", model.View())
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected send command from picker selection")
	}
	if model.filePickerOpen {
		t.Fatal("expected file picker to close after selecting a file")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected send result message")
	}
	_, _ = model.Update(msg)
	if len(client.sent) == 0 {
		t.Fatal("expected picker send to produce envelopes")
	}
	if model.status != "selected draft.txt" && !strings.Contains(model.status, "sending file") {
		t.Fatalf("unexpected status after picker send: %q", model.status)
	}
	found := false
	for _, message := range model.messages {
		if strings.Contains(message, "file sent: draft.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected picker file message in history: %+v", model.messages)
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
	model.connected = true
	model.connecting = false
	model.SetSize(100, 20)
	model.filePickerDir = pickerRoot

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !model.filePickerOpen {
		t.Fatal("expected file picker to open")
	}
	if model.selectedFilePickerEntry() == nil || !model.selectedFilePickerEntry().IsDir {
		t.Fatalf("expected first picker entry to be the child directory, got %+v", model.selectedFilePickerEntry())
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.filePickerDir != childDir {
		t.Fatalf("expected picker to enter child dir, got %q", model.filePickerDir)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if model.filePickerDir != pickerRoot {
		t.Fatalf("expected picker to return to root dir, got %q", model.filePickerDir)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.filePickerOpen {
		t.Fatal("expected file picker to close on escape")
	}
	if model.status != "file picker closed" {
		t.Fatalf("unexpected picker close status: %q", model.status)
	}
}

func TestFilePickerVisibleEntriesStayWithinWindow(t *testing.T) {
	model := &Model{}
	for i := 0; i < 20; i++ {
		model.filePickerEntries = append(model.filePickerEntries, filePickerEntry{Name: fmt.Sprintf("entry-%02d", i)})
	}
	model.filePickerSelected = 10

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
		if entry.index == model.filePickerSelected {
			foundSelected = true
			break
		}
	}
	if !foundSelected {
		t.Fatal("expected selected entry to stay visible")
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
	envelopes, err := bobService.TypingEnvelopes("alice", typingStateActive)
	if err != nil {
		t.Fatalf("typing envelopes: %v", err)
	}
	model.handleProtocolMessage(protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &envelopes[0]})
	view := model.View()
	if !strings.Contains(view, "bob is typing") {
		t.Fatalf("expected typing indicator in view: %q", view)
	}

	_, _ = model.Update(typingTickMsg(time.Now().UTC().Add(typingAnimationInterval)))
	view = model.View()
	if !strings.Contains(view, "bob is typing") {
		t.Fatalf("expected animated typing indicator in view: %q", view)
	}
	// Verify spinner actually advanced (frame changed)
	view2 := model.View()
	_, _ = model.Update(typingTickMsg(time.Now().UTC().Add(2 * typingAnimationInterval)))
	view3 := model.View()
	if view2 == view3 {
		t.Fatalf("expected typing indicator to animate between frames, got same view: %q", view3)
	}

	model.peerTypingExpiresAt = time.Now().UTC().Add(-time.Second)
	_, _ = model.Update(typingTickMsg(time.Now().UTC()))
	view = model.View()
	if strings.Contains(view, "bob is typing") {
		t.Fatalf("expected typing indicator to expire: %q", view)
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
	model.connected = true
	model.connecting = false

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
	if model.localTypingSent {
		t.Fatal("expected local typing state to reset")
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
