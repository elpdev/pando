package messaging

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relay"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/transport"
	wsclient "github.com/elpdev/pando/internal/transport/ws"
	"net/http/httptest"
)

func TestHandleIncomingContactUpdateRefreshesStoredDevices(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bob, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new bob identity: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bob.InviteBundle())
	if err != nil {
		t.Fatalf("bob invite to contact: %v", err)
	}
	if err := aliceStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob contact: %v", err)
	}

	pending, err := identity.NewPendingEnrollment("bob", "bob-phone")
	if err != nil {
		t.Fatalf("new pending enrollment: %v", err)
	}
	approval, err := bob.Approve(pending.Request())
	if err != nil {
		t.Fatalf("approve bob enrollment: %v", err)
	}
	bobUpdated, err := pending.Complete(*approval)
	if err != nil {
		t.Fatalf("complete bob enrollment: %v", err)
	}

	batch, err := aliceService.EncryptOutgoing("bob", "hello after update")
	if err != nil {
		t.Fatalf("encrypt outgoing: %v", err)
	}
	if batch == nil || len(batch.Envelopes) == 0 || batch.Envelopes[0].BodyEncoding != BodyEncodingContactUpdate {
		t.Fatalf("expected first outgoing envelope to be a contact update")
	}

	aliceContact, err := identity.ContactFromInvite(aliceService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("alice invite to contact: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	if err := bobStore.SaveContact(aliceContact); err != nil {
		t.Fatalf("save alice contact: %v", err)
	}
	bobService := &Service{store: bobStore, identity: bobUpdated}

	result, err := bobService.HandleIncoming(batch.Envelopes[0])
	if err != nil {
		t.Fatalf("handle incoming contact update: %v", err)
	}
	if result == nil || result.ContactUpdated == nil {
		t.Fatalf("expected contact update result")
	}
	if len(result.ContactUpdated.ActiveDevices()) != 1 {
		t.Fatalf("expected alice to still have one active device, got %d", len(result.ContactUpdated.ActiveDevices()))
	}
}

func TestHandleIncomingSkipsDuplicateEnvelopeIDs(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bob, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new bob identity: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bob.InviteBundle())
	if err != nil {
		t.Fatalf("bob invite to contact: %v", err)
	}
	if err := aliceStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob contact: %v", err)
	}
	batch, err := aliceService.EncryptOutgoing("bob", "hello bob")
	if err != nil {
		t.Fatalf("encrypt outgoing: %v", err)
	}
	chatEnvelope := batch.Envelopes[len(batch.Envelopes)-1]
	chatEnvelope.ID = "dup-1"

	aliceContact, err := identity.ContactFromInvite(aliceService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("alice invite to contact: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	if err := bobStore.SaveIdentity(bob); err != nil {
		t.Fatalf("save bob identity: %v", err)
	}
	if err := bobStore.SaveContact(aliceContact); err != nil {
		t.Fatalf("save alice contact: %v", err)
	}
	bobService := &Service{store: bobStore, identity: bob}

	first, err := bobService.HandleIncoming(chatEnvelope)
	if err != nil {
		t.Fatalf("first handle incoming: %v", err)
	}
	if first == nil || first.Duplicate || first.Body == "" {
		t.Fatalf("expected first delivery to be processed")
	}
	second, err := bobService.HandleIncoming(chatEnvelope)
	if err != nil {
		t.Fatalf("second handle incoming: %v", err)
	}
	if second == nil || !second.Duplicate {
		t.Fatalf("expected second delivery to be marked duplicate")
	}
}

func TestDeliveryAckMarksSentHistoryDelivered(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	bobService, _, err := New(bobStore, "bob")
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

	batch, err := aliceService.EncryptOutgoing("bob", "needs ack")
	if err != nil {
		t.Fatalf("encrypt outgoing: %v", err)
	}
	if batch == nil || batch.MessageID == "" {
		t.Fatalf("expected outgoing batch message id")
	}
	if err := aliceService.SaveSent("bob", batch.MessageID, "needs ack"); err != nil {
		t.Fatalf("save sent: %v", err)
	}
	chatEnvelope := batch.Envelopes[len(batch.Envelopes)-1]
	chatEnvelope.ID = "relay-msg-1"

	result, err := bobService.HandleIncoming(chatEnvelope)
	if err != nil {
		t.Fatalf("handle incoming chat: %v", err)
	}
	if result == nil || len(result.AckEnvelopes) != 1 {
		t.Fatalf("expected one delivery ack envelope")
	}
	ackEnvelope := result.AckEnvelopes[0]
	ackEnvelope.ID = "relay-ack-1"
	ackResult, err := aliceService.HandleIncoming(ackEnvelope)
	if err != nil {
		t.Fatalf("handle delivery ack: %v", err)
	}
	if ackResult == nil || !ackResult.Control {
		t.Fatalf("expected delivery ack to be treated as control")
	}
	history, err := aliceService.History("bob")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(history) != 1 || !history[0].Delivered {
		t.Fatalf("expected sent history to be marked delivered: %+v", history)
	}
}

func TestEncryptOutgoingMissingContactSuggestsImportCommand(t *testing.T) {
	service, _, err := New(store.NewClientStore(t.TempDir()), "alice")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = service.EncryptOutgoing("bob", "hello")
	if err == nil {
		t.Fatal("expected missing contact error")
	}
	if !strings.Contains(err.Error(), "pandoctl add-contact --mailbox <your-mailbox> --paste") {
		t.Fatalf("expected import guidance, got %v", err)
	}
}

func TestPhotoChunkRoundTripStoresAttachment(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobDir := t.TempDir()
	bobStore := store.NewClientStore(bobDir)
	bobService, _, err := New(bobStore, "bob")
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

	photoBytes := mustPhotoBytes(t)
	photoBytes = append(photoBytes, make([]byte, attachmentChunkSizeBytes*2)...)
	photoPath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(photoPath, photoBytes, 0o600); err != nil {
		t.Fatalf("write photo fixture: %v", err)
	}

	batch, displayBody, err := aliceService.PreparePhotoOutgoing("bob", photoPath)
	if err != nil {
		t.Fatalf("prepare photo outgoing: %v", err)
	}
	if batch == nil || len(batch.Envelopes) < 3 {
		t.Fatalf("expected photo batch with chunk envelopes, got %+v", batch)
	}
	if displayBody != "photo sent: photo.png" {
		t.Fatalf("unexpected photo display body: %q", displayBody)
	}

	var finalResult *IncomingResult
	for i := range batch.Envelopes {
		envelope := batch.Envelopes[i]
		envelope.ID = fmt.Sprintf("env-%d", i)
		result, err := bobService.HandleIncoming(envelope)
		if err != nil {
			t.Fatalf("handle incoming envelope %d: %v", i, err)
		}
		if result != nil && !result.Control && result.Body != "" {
			finalResult = result
		}
	}
	if finalResult == nil {
		t.Fatal("expected final photo result")
	}
	if !strings.Contains(finalResult.Body, "photo received: photo.png saved to ") {
		t.Fatalf("unexpected final body: %q", finalResult.Body)
	}
	attachmentPaths, err := filepath.Glob(filepath.Join(bobDir, "attachments", "alice", "*"))
	if err != nil {
		t.Fatalf("glob attachments: %v", err)
	}
	if len(attachmentPaths) != 1 {
		t.Fatalf("expected one stored attachment, got %v", attachmentPaths)
	}
	storedBytes, err := os.ReadFile(attachmentPaths[0])
	if err != nil {
		t.Fatalf("read stored attachment: %v", err)
	}
	if string(storedBytes) != string(photoBytes) {
		t.Fatal("stored attachment bytes did not match original photo")
	}
	if len(finalResult.AckEnvelopes) != 0 {
		t.Fatalf("expected no delivery ack for photo chunks, got %d", len(finalResult.AckEnvelopes))
	}
}

func TestPhotoTransferOverRelayEndToEnd(t *testing.T) {
	server := httptest.NewServer(relay.NewServer(slog.New(slog.NewTextHandler(testWriter{}, nil)), relay.NewMemoryQueueStore(), relay.Options{}).Handler())
	defer server.Close()

	aliceDir := t.TempDir()
	aliceStore := store.NewClientStore(aliceDir)
	aliceService, _, err := New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobDir := t.TempDir()
	bobStore := store.NewClientStore(bobDir)
	bobService, _, err := New(bobStore, "bob")
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

	photoBytes := mustPhotoBytes(t)
	photoBytes = append(photoBytes, make([]byte, 410178-len(photoBytes))...)
	photoPath := filepath.Join(t.TempDir(), "bender.png")
	if err := os.WriteFile(photoPath, photoBytes, 0o600); err != nil {
		t.Fatalf("write photo fixture: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	aliceClient := wsclient.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", "alice")
	defer aliceClient.Close()
	if err := aliceClient.Connect(ctx); err != nil {
		t.Fatalf("connect alice client: %v", err)
	}
	awaitSubscribeAck(t, aliceClient.Events())
	bobClient := wsclient.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", "bob")
	defer bobClient.Close()
	if err := bobClient.Connect(ctx); err != nil {
		t.Fatalf("connect bob client: %v", err)
	}
	awaitSubscribeAck(t, bobClient.Events())

	batch, _, err := bobService.PreparePhotoOutgoing("alice", photoPath)
	if err != nil {
		t.Fatalf("prepare photo outgoing: %v", err)
	}
	for _, envelope := range batch.Envelopes {
		if err := bobClient.Send(envelope); err != nil {
			t.Fatalf("bob send photo envelope: %v", err)
		}
	}

	var finalResult *IncomingResult
	deadline := time.After(10 * time.Second)
	for finalResult == nil {
		select {
		case event := <-aliceClient.Events():
			if event.Err != nil {
				t.Fatalf("alice event error: %v", event.Err)
			}
			if event.Message == nil || event.Message.Type != "incoming" || event.Message.Incoming == nil {
				continue
			}
			result, err := aliceService.HandleIncoming(*event.Message.Incoming)
			if err != nil {
				t.Fatalf("alice handle incoming: %v", err)
			}
			if result == nil {
				continue
			}
			for _, ack := range result.AckEnvelopes {
				if err := aliceClient.Send(ack); err != nil {
					t.Fatalf("alice send ack: %v", err)
				}
			}
			if !result.Control && strings.Contains(result.Body, "photo received:") {
				finalResult = result
			}
		case event := <-bobClient.Events():
			if event.Err != nil {
				t.Fatalf("bob event error: %v", event.Err)
			}
		case <-deadline:
			t.Fatal("timed out waiting for final photo result")
		}
	}

	attachments, err := filepath.Glob(filepath.Join(aliceDir, "attachments", "bob", "*"))
	if err != nil {
		t.Fatalf("glob alice attachments: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected one alice attachment, got %v", attachments)
	}
	saved, err := os.ReadFile(attachments[0])
	if err != nil {
		t.Fatalf("read saved attachment: %v", err)
	}
	if string(saved) != string(photoBytes) {
		t.Fatal("saved attachment bytes did not match sent bytes")
	}
}

func TestVoiceChunkRoundTripStoresAttachment(t *testing.T) {
	aliceStore := store.NewClientStore(t.TempDir())
	aliceService, _, err := New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobDir := t.TempDir()
	bobStore := store.NewClientStore(bobDir)
	bobService, _, err := New(bobStore, "bob")
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

	voiceBytes := mustVoiceBytes(t)
	voiceBytes = append(voiceBytes, make([]byte, attachmentChunkSizeBytes*2)...)
	voicePath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(voicePath, voiceBytes, 0o600); err != nil {
		t.Fatalf("write voice fixture: %v", err)
	}

	batch, displayBody, err := aliceService.PrepareVoiceOutgoing("bob", voicePath)
	if err != nil {
		t.Fatalf("prepare voice outgoing: %v", err)
	}
	if batch == nil || len(batch.Envelopes) < 3 {
		t.Fatalf("expected voice batch with chunk envelopes, got %+v", batch)
	}
	if displayBody != "voice note sent: clip.wav" {
		t.Fatalf("unexpected voice display body: %q", displayBody)
	}

	var finalResult *IncomingResult
	for i := range batch.Envelopes {
		envelope := batch.Envelopes[i]
		envelope.ID = fmt.Sprintf("voice-env-%d", i)
		result, err := bobService.HandleIncoming(envelope)
		if err != nil {
			t.Fatalf("handle incoming envelope %d: %v", i, err)
		}
		if result != nil && !result.Control && result.Body != "" {
			finalResult = result
		}
	}
	if finalResult == nil {
		t.Fatal("expected final voice result")
	}
	if !strings.Contains(finalResult.Body, "voice note received: clip.wav saved to ") {
		t.Fatalf("unexpected final body: %q", finalResult.Body)
	}
	attachmentPaths, err := filepath.Glob(filepath.Join(bobDir, "attachments", "alice", "*"))
	if err != nil {
		t.Fatalf("glob attachments: %v", err)
	}
	if len(attachmentPaths) != 1 {
		t.Fatalf("expected one stored attachment, got %v", attachmentPaths)
	}
	storedBytes, err := os.ReadFile(attachmentPaths[0])
	if err != nil {
		t.Fatalf("read stored attachment: %v", err)
	}
	if string(storedBytes) != string(voiceBytes) {
		t.Fatal("stored attachment bytes did not match original voice note")
	}
	if len(finalResult.AckEnvelopes) != 0 {
		t.Fatalf("expected no delivery ack for voice chunks, got %d", len(finalResult.AckEnvelopes))
	}
}

func TestBackToBackLargePhotoTransfersStayUnderRateLimit(t *testing.T) {
	server := httptest.NewServer(relay.NewServer(slog.New(slog.NewTextHandler(testWriter{}, nil)), relay.NewMemoryQueueStore(), relay.Options{}).Handler())
	defer server.Close()

	aliceDir := t.TempDir()
	aliceStore := store.NewClientStore(aliceDir)
	aliceService, _, err := New(aliceStore, "alice")
	if err != nil {
		t.Fatalf("new alice service: %v", err)
	}
	bobDir := t.TempDir()
	bobStore := store.NewClientStore(bobDir)
	bobService, _, err := New(bobStore, "bob")
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

	photoBytes := mustPhotoBytes(t)
	photoBytes = append(photoBytes, make([]byte, 410178-len(photoBytes))...)
	alicePhotoPath := filepath.Join(t.TempDir(), "alice.png")
	bobPhotoPath := filepath.Join(t.TempDir(), "bob.png")
	if err := os.WriteFile(alicePhotoPath, photoBytes, 0o600); err != nil {
		t.Fatalf("write alice photo: %v", err)
	}
	if err := os.WriteFile(bobPhotoPath, photoBytes, 0o600); err != nil {
		t.Fatalf("write bob photo: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	aliceClient := wsclient.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", "alice")
	defer aliceClient.Close()
	if err := aliceClient.Connect(ctx); err != nil {
		t.Fatalf("connect alice client: %v", err)
	}
	awaitSubscribeAck(t, aliceClient.Events())
	bobClient := wsclient.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", "bob")
	defer bobClient.Close()
	if err := bobClient.Connect(ctx); err != nil {
		t.Fatalf("connect bob client: %v", err)
	}
	awaitSubscribeAck(t, bobClient.Events())

	sendPhotoAndAwaitReceipt(t, aliceClient, aliceService, bobClient, bobService, "bob", alicePhotoPath)
	sendPhotoAndAwaitReceipt(t, bobClient, bobService, aliceClient, aliceService, "alice", bobPhotoPath)

	if matches, err := filepath.Glob(filepath.Join(aliceDir, "attachments", "bob", "*")); err != nil || len(matches) != 1 {
		t.Fatalf("expected one bob attachment for alice, got %v err=%v", matches, err)
	}
	if matches, err := filepath.Glob(filepath.Join(bobDir, "attachments", "alice", "*")); err != nil || len(matches) != 1 {
		t.Fatalf("expected one alice attachment for bob, got %v err=%v", matches, err)
	}
}

func sendPhotoAndAwaitReceipt(t *testing.T, senderClient *wsclient.Client, senderService *Service, receiverClient *wsclient.Client, receiverService *Service, recipientMailbox, path string) {
	t.Helper()
	batch, _, err := senderService.PreparePhotoOutgoing(recipientMailbox, path)
	if err != nil {
		t.Fatalf("prepare photo outgoing: %v", err)
	}
	for _, envelope := range batch.Envelopes {
		if err := senderClient.Send(envelope); err != nil {
			t.Fatalf("send photo envelope: %v", err)
		}
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event := <-receiverClient.Events():
			if event.Err != nil {
				t.Fatalf("receiver event error: %v", event.Err)
			}
			if event.Message == nil || event.Message.Type != protocol.MessageTypeIncoming || event.Message.Incoming == nil {
				continue
			}
			result, err := receiverService.HandleIncoming(*event.Message.Incoming)
			if err != nil {
				t.Fatalf("receiver handle incoming: %v", err)
			}
			if result != nil && !result.Control && strings.Contains(result.Body, "photo received:") {
				return
			}
		case event := <-senderClient.Events():
			if event.Err != nil {
				t.Fatalf("sender event error: %v", event.Err)
			}
		case <-deadline:
			t.Fatal("timed out waiting for photo receipt")
		}
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

func mustVoiceBytes(t *testing.T) []byte {
	t.Helper()
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

func awaitSubscribeAck(t *testing.T, events <-chan transport.Event) {
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

type testWriter struct{}

func (testWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}
