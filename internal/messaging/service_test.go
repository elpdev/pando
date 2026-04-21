package messaging

import (
	"context"
	"crypto/ed25519"
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
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
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
	if _, err := pending.Complete(*approval); err != nil {
		t.Fatalf("complete bob enrollment: %v", err)
	}

	batch, err := aliceService.EncryptOutgoing("bob", "hello after update")
	if err != nil {
		t.Fatalf("encrypt outgoing: %v", err)
	}
	if batch == nil || len(batch.Envelopes) == 0 {
		t.Fatalf("expected outgoing envelopes")
	}

	aliceContact, err := identity.ContactFromInvite(aliceService.Identity().InviteBundle())
	if err != nil {
		t.Fatalf("alice invite to contact: %v", err)
	}
	bobStore := store.NewClientStore(t.TempDir())
	if err := bobStore.SaveContact(aliceContact); err != nil {
		t.Fatalf("save alice contact: %v", err)
	}
	bobService := &Service{store: bobStore, identity: bob, incomingAttachments: newIncomingAttachmentAssembler(bobStore, bob)}

	result, err := bobService.HandleIncoming(batch.Envelopes[0])
	if err != nil {
		t.Fatalf("handle incoming contact update: %v", err)
	}
	if result == nil || result.ContactUpdated == nil {
		t.Fatalf("expected contact update result")
	}
	if result.ContactChange != ContactUpdateUnchanged {
		t.Fatalf("expected unchanged contact update, got %q", result.ContactChange)
	}
	if len(result.ContactUpdated.ActiveDevices()) != 1 {
		t.Fatalf("expected alice to still have one active device, got %d", len(result.ContactUpdated.ActiveDevices()))
	}
}

func TestDetectContactUpdateChangeClassifiesAddedDevice(t *testing.T) {
	existing := mustContactFromIdentity(t, mustIdentity(t, "alice"))
	updated := cloneContact(existing)
	updated.Devices = append(updated.Devices, identity.ContactDevice{
		ID:               "alice-phone",
		Mailbox:          "alice-phone",
		SigningPublic:    ed25519.PublicKey("signing-added"),
		EncryptionPublic: []byte("encrypt-added"),
	})

	if got := detectContactUpdateChange(existing, updated); got != ContactUpdateDeviceAdded {
		t.Fatalf("expected device-added change, got %q", got)
	}
}

func TestDetectContactUpdateChangeClassifiesRevokedDevice(t *testing.T) {
	existing := mustContactFromIdentity(t, mustIdentity(t, "alice"))
	updated := cloneContact(existing)
	updated.Devices[0].Revoked = true
	updated.Devices[0].RevokedAt = time.Now().UTC()

	if got := detectContactUpdateChange(existing, updated); got != ContactUpdateDeviceRevoked {
		t.Fatalf("expected device-revoked change, got %q", got)
	}
}

func TestDetectContactUpdateChangeClassifiesRotatedDeviceKeys(t *testing.T) {
	existing := mustContactFromIdentity(t, mustIdentity(t, "alice"))
	updated := cloneContact(existing)
	updated.Devices[0].SigningPublic = ed25519.PublicKey("signing-rotated")
	updated.Devices[0].EncryptionPublic = []byte("encrypt-rotated")

	if got := detectContactUpdateChange(existing, updated); got != ContactUpdateDeviceRotated {
		t.Fatalf("expected device-rotated change, got %q", got)
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
	bobService := &Service{store: bobStore, identity: bob, incomingAttachments: newIncomingAttachmentAssembler(bobStore, bob)}

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
	if err := aliceService.SaveSent("bob", batch.MessageID, "needs ack", nil); err != nil {
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

func TestDefaultRoomRoundTrip(t *testing.T) {
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

	aliceRoom, aliceJoinBatch, err := aliceService.JoinDefaultRoom()
	if err != nil {
		t.Fatalf("alice join room: %v", err)
	}
	if aliceRoom == nil || !aliceRoom.Joined {
		t.Fatalf("expected alice to join room: %+v", aliceRoom)
	}
	for i := range aliceJoinBatch.Envelopes {
		aliceJoinBatch.Envelopes[i].ID = fmt.Sprintf("alice-join-%d", i)
		if _, err := bobService.HandleIncoming(aliceJoinBatch.Envelopes[i]); err != nil {
			t.Fatalf("bob handle alice membership: %v", err)
		}
	}
	bobRoom, bobJoinBatch, err := bobService.JoinDefaultRoom()
	if err != nil {
		t.Fatalf("bob join room: %v", err)
	}
	if bobRoom == nil || !bobRoom.Joined {
		t.Fatalf("expected bob to join room: %+v", bobRoom)
	}
	for i := range bobJoinBatch.Envelopes {
		bobJoinBatch.Envelopes[i].ID = fmt.Sprintf("bob-join-%d", i)
		if _, err := aliceService.HandleIncoming(bobJoinBatch.Envelopes[i]); err != nil {
			t.Fatalf("alice handle bob membership: %v", err)
		}
	}

	batch, err := aliceService.EncryptDefaultRoomOutgoing("hello room")
	if err != nil {
		t.Fatalf("encrypt default room outgoing: %v", err)
	}
	if batch == nil || batch.MessageID == "" || len(batch.Envelopes) == 0 {
		t.Fatalf("expected room batch with encrypted envelopes: %+v", batch)
	}
	for i := range batch.Envelopes {
		batch.Envelopes[i].ID = fmt.Sprintf("room-msg-%d", i)
		result, err := bobService.HandleIncoming(batch.Envelopes[i])
		if err != nil {
			t.Fatalf("bob handle room message: %v", err)
		}
		if result == nil || result.RoomID != DefaultRoomID || result.Body != "hello room" {
			t.Fatalf("unexpected room incoming result: %+v", result)
		}
	}
}

func TestDefaultRoomHistorySyncHonorsRequesterJoinTime(t *testing.T) {
	now := time.Now().UTC().Round(time.Second)
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
	seedRoomSyncContacts(t, aliceStore, aliceService, bobStore, bobService)
	seedRoomSyncState(t, aliceStore, aliceService.Identity(), now.Add(-10*24*time.Hour), now.Add(-24*time.Hour))
	seedRoomSyncState(t, bobStore, bobService.Identity(), now.Add(-10*24*time.Hour), now.Add(-24*time.Hour))
	if err := aliceStore.AppendRoomHistory(aliceService.Identity(), DefaultRoomID, store.RoomMessageRecord{MessageID: "old-join", SenderAccountID: "alice", Body: "before bob joined", Timestamp: now.Add(-48 * time.Hour)}); err != nil {
		t.Fatalf("append old join history: %v", err)
	}
	if err := aliceStore.AppendRoomHistory(aliceService.Identity(), DefaultRoomID, store.RoomMessageRecord{MessageID: "recent", SenderAccountID: "alice", Body: "after bob joined", Timestamp: now.Add(-12 * time.Hour)}); err != nil {
		t.Fatalf("append recent history: %v", err)
	}

	results := syncDefaultRoomHistory(t, aliceService, bobService)
	if len(results) == 0 || results[len(results)-1].RoomSync == nil || !results[len(results)-1].RoomSync.Complete {
		t.Fatalf("expected completed room sync results: %+v", results)
	}
	history, err := bobService.DefaultRoomHistory()
	if err != nil {
		t.Fatalf("load bob room history: %v", err)
	}
	if len(history) != 1 || history[0].MessageID != "recent" {
		t.Fatalf("expected only post-join history, got %+v", history)
	}
}

func TestDefaultRoomHistorySyncHonorsSevenDayLimit(t *testing.T) {
	now := time.Now().UTC().Round(time.Second)
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
	seedRoomSyncContacts(t, aliceStore, aliceService, bobStore, bobService)
	seedRoomSyncState(t, aliceStore, aliceService.Identity(), now.Add(-14*24*time.Hour), now.Add(-10*24*time.Hour))
	seedRoomSyncState(t, bobStore, bobService.Identity(), now.Add(-14*24*time.Hour), now.Add(-10*24*time.Hour))
	if err := aliceStore.AppendRoomHistory(aliceService.Identity(), DefaultRoomID, store.RoomMessageRecord{MessageID: "too-old", SenderAccountID: "alice", Body: "older than seven days", Timestamp: now.Add(-8 * 24 * time.Hour)}); err != nil {
		t.Fatalf("append too old history: %v", err)
	}
	if err := aliceStore.AppendRoomHistory(aliceService.Identity(), DefaultRoomID, store.RoomMessageRecord{MessageID: "within-window", SenderAccountID: "alice", Body: "within seven days", Timestamp: now.Add(-6 * 24 * time.Hour)}); err != nil {
		t.Fatalf("append within window history: %v", err)
	}

	results := syncDefaultRoomHistory(t, aliceService, bobService)
	if len(results) == 0 || results[len(results)-1].RoomSync == nil || !results[len(results)-1].RoomSync.Complete {
		t.Fatalf("expected completed room sync results: %+v", results)
	}
	history, err := bobService.DefaultRoomHistory()
	if err != nil {
		t.Fatalf("load bob room history: %v", err)
	}
	if len(history) != 1 || history[0].MessageID != "within-window" {
		t.Fatalf("expected only seven-day history, got %+v", history)
	}
}

func TestDefaultRoomHistorySyncPreservesExpiresAt(t *testing.T) {
	now := time.Now().UTC().Round(time.Second)
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
	seedRoomSyncContacts(t, aliceStore, aliceService, bobStore, bobService)
	seedRoomSyncState(t, aliceStore, aliceService.Identity(), now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	seedRoomSyncState(t, bobStore, bobService.Identity(), now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	expiresAt := now.Add(12 * time.Hour)
	if err := aliceStore.AppendRoomHistory(aliceService.Identity(), DefaultRoomID, store.RoomMessageRecord{MessageID: "with-ttl", SenderAccountID: "alice", Body: "ephemeral", Timestamp: now.Add(-time.Hour), ExpiresAt: expiresAt}); err != nil {
		t.Fatalf("append history with ttl: %v", err)
	}

	results := syncDefaultRoomHistory(t, aliceService, bobService)
	if len(results) == 0 || results[len(results)-1].RoomSync == nil || !results[len(results)-1].RoomSync.Complete {
		t.Fatalf("expected completed room sync results: %+v", results)
	}
	history, err := bobService.DefaultRoomHistory()
	if err != nil {
		t.Fatalf("load bob room history: %v", err)
	}
	if len(history) != 1 || history[0].MessageID != "with-ttl" {
		t.Fatalf("expected with-ttl record synced, got %+v", history)
	}
	if !history[0].ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected synced ExpiresAt %s, got %s", expiresAt, history[0].ExpiresAt)
	}
}

func TestDefaultRoomHistorySyncDropsAlreadyExpired(t *testing.T) {
	now := time.Now().UTC().Round(time.Second)
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
	seedRoomSyncContacts(t, aliceStore, aliceService, bobStore, bobService)
	seedRoomSyncState(t, aliceStore, aliceService.Identity(), now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	seedRoomSyncState(t, bobStore, bobService.Identity(), now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	// Seed alice with a record whose expiry is already in the past. It will be
	// filtered out on Alice's LoadRoomHistory (inline sweep), so it can't reach
	// Bob's sync payload. Also seed a fresh one to confirm the sync still works.
	if err := aliceStore.AppendRoomHistory(aliceService.Identity(), DefaultRoomID, store.RoomMessageRecord{MessageID: "stale", SenderAccountID: "alice", Body: "stale", Timestamp: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}); err != nil {
		t.Fatalf("append stale history: %v", err)
	}
	if err := aliceStore.AppendRoomHistory(aliceService.Identity(), DefaultRoomID, store.RoomMessageRecord{MessageID: "fresh", SenderAccountID: "alice", Body: "fresh", Timestamp: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("append fresh history: %v", err)
	}

	results := syncDefaultRoomHistory(t, aliceService, bobService)
	if len(results) == 0 || results[len(results)-1].RoomSync == nil || !results[len(results)-1].RoomSync.Complete {
		t.Fatalf("expected completed room sync results: %+v", results)
	}
	history, err := bobService.DefaultRoomHistory()
	if err != nil {
		t.Fatalf("load bob room history: %v", err)
	}
	if len(history) != 1 || history[0].MessageID != "fresh" {
		t.Fatalf("expected only fresh record, got %+v", history)
	}
}

func seedRoomSyncContacts(t *testing.T, aliceStore *store.ClientStore, aliceService *Service, bobStore *store.ClientStore, bobService *Service) {
	t.Helper()
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
}

func seedRoomSyncState(t *testing.T, clientStore *store.ClientStore, id *identity.Identity, aliceJoinedAt, bobJoinedAt time.Time) {
	t.Helper()
	state := &store.RoomState{
		ID:        DefaultRoomID,
		Name:      DefaultRoomName,
		Joined:    true,
		UpdatedAt: bobJoinedAt,
		Members: []store.RoomMember{
			{AccountID: "alice", JoinedAt: aliceJoinedAt},
			{AccountID: "bob", JoinedAt: bobJoinedAt},
		},
	}
	if id.AccountID == "alice" {
		state.JoinedAt = aliceJoinedAt
	} else {
		state.JoinedAt = bobJoinedAt
	}
	if err := clientStore.SaveRoomState(id, state); err != nil {
		t.Fatalf("save room state: %v", err)
	}
}

func syncDefaultRoomHistory(t *testing.T, responder, requester *Service) []*IncomingResult {
	t.Helper()
	batch, requestID, err := requester.RequestDefaultRoomHistory()
	if err != nil {
		t.Fatalf("request default room history: %v", err)
	}
	if requestID == "" || batch == nil || len(batch.Envelopes) == 0 {
		t.Fatalf("expected room history request batch, got requestID=%q batch=%+v", requestID, batch)
	}
	results := make([]*IncomingResult, 0)
	responseID := 0
	for i := range batch.Envelopes {
		batch.Envelopes[i].ID = fmt.Sprintf("room-sync-request-%d", i)
		result, err := responder.HandleIncoming(batch.Envelopes[i])
		if err != nil {
			t.Fatalf("responder handle room history request: %v", err)
		}
		if result == nil {
			continue
		}
		for j := range result.AckEnvelopes {
			result.AckEnvelopes[j].ID = fmt.Sprintf("room-sync-response-%d", responseID)
			responseID++
			chunkResult, err := requester.HandleIncoming(result.AckEnvelopes[j])
			if err != nil {
				t.Fatalf("requester handle room history chunk: %v", err)
			}
			results = append(results, chunkResult)
		}
	}
	return results
}

func TestTypingIndicatorHandledAsTransientControl(t *testing.T) {
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

	envelopes, err := bobService.TypingEnvelopes("alice", TypingStateActive)
	if err != nil {
		t.Fatalf("typing envelopes: %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("expected one typing envelope, got %d", len(envelopes))
	}
	result, err := aliceService.HandleIncoming(envelopes[0])
	if err != nil {
		t.Fatalf("handle typing envelope: %v", err)
	}
	if result == nil || !result.Control {
		t.Fatalf("expected typing envelope to be control result: %+v", result)
	}
	if result.PeerAccountID != "bob" {
		t.Fatalf("expected bob peer account, got %q", result.PeerAccountID)
	}
	if result.TypingState != TypingStateActive {
		t.Fatalf("expected active typing state, got %q", result.TypingState)
	}
	if result.TypingExpiresAt.IsZero() {
		t.Fatal("expected typing expiry")
	}
	if len(result.AckEnvelopes) != 0 {
		t.Fatalf("expected no delivery acks for typing payload, got %d", len(result.AckEnvelopes))
	}
	history, err := aliceService.History("bob")
	if err != nil {
		t.Fatalf("load alice history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected typing indicator to stay out of history: %+v", history)
	}

	idleEnvelopes, err := bobService.TypingEnvelopes("alice", TypingStateIdle)
	if err != nil {
		t.Fatalf("idle typing envelopes: %v", err)
	}
	if len(idleEnvelopes) != 1 {
		t.Fatalf("expected one idle typing envelope, got %d", len(idleEnvelopes))
	}
	idleResult, err := aliceService.HandleIncoming(idleEnvelopes[0])
	if err != nil {
		t.Fatalf("handle idle typing envelope: %v", err)
	}
	if idleResult == nil || idleResult.TypingState != TypingStateIdle {
		t.Fatalf("expected idle typing result: %+v", idleResult)
	}
	if !idleResult.TypingExpiresAt.IsZero() {
		t.Fatalf("expected idle typing expiry to be cleared, got %v", idleResult.TypingExpiresAt)
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
	if !strings.Contains(err.Error(), "pando contact add --mailbox <your-mailbox> --paste") {
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
	if finalResult.Attachment == nil || finalResult.Attachment.Type != AttachmentTypePhoto {
		t.Fatalf("expected photo attachment metadata, got %+v", finalResult.Attachment)
	}
	attachmentPaths, err := filepath.Glob(filepath.Join(bobDir, "attachments", "alice", "*"))
	if err != nil {
		t.Fatalf("glob attachments: %v", err)
	}
	if len(attachmentPaths) != 1 {
		t.Fatalf("expected one stored attachment, got %v", attachmentPaths)
	}
	storedBytes, err := bobStore.ReadAttachment(bobService.Identity(), attachmentPaths[0])
	if err != nil {
		t.Fatalf("read stored attachment: %v", err)
	}
	if string(storedBytes) != string(photoBytes) {
		t.Fatal("stored attachment bytes did not match original photo")
	}
	onDisk, err := os.ReadFile(attachmentPaths[0])
	if err != nil {
		t.Fatalf("read encrypted attachment: %v", err)
	}
	if string(onDisk) == string(photoBytes) {
		t.Fatal("expected photo attachment on disk to remain encrypted")
	}
	if len(finalResult.AckEnvelopes) != 0 {
		t.Fatalf("expected no delivery ack for photo chunks, got %d", len(finalResult.AckEnvelopes))
	}
}

func TestHandleIncomingAttachmentChunkRejectsOversizedTransfer(t *testing.T) {
	service, _, err := New(store.NewClientStore(t.TempDir()), "bob")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, _, err = service.handleIncomingAttachmentChunk("alice", &attachmentChunkPayload{
		AttachmentType: AttachmentTypePhoto,
		AttachmentID:   "photo-1",
		Filename:       "photo.png",
		MIMEType:       "image/png",
		TotalSize:      maxAttachmentSizeBytes + 1,
		ChunkIndex:     0,
		ChunkCount:     1,
		Data:           base64.StdEncoding.EncodeToString([]byte("chunk")),
	})
	if err == nil {
		t.Fatal("expected oversized attachment to be rejected")
	}
}

func TestHandleIncomingAttachmentChunkCleansUpExpiredTransfers(t *testing.T) {
	service, _, err := New(store.NewClientStore(t.TempDir()), "bob")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	service.incomingAttachments = &incomingAttachmentAssembler{identity: service.Identity(), store: service.store, pending: map[string]*incomingAttachment{
		"alice:stale": {
			attachmentType: AttachmentTypePhoto,
			filename:       "stale.png",
			totalSize:      4,
			chunkCount:     1,
			chunks:         make([][]byte, 1),
			updatedAt:      time.Now().UTC().Add(-incomingAttachmentTTL - time.Minute),
		},
	}}
	_, done, err := service.handleIncomingAttachmentChunk("alice", &attachmentChunkPayload{
		AttachmentType: AttachmentTypePhoto,
		AttachmentID:   "fresh",
		Filename:       "photo.png",
		MIMEType:       "image/png",
		TotalSize:      5,
		ChunkIndex:     0,
		ChunkCount:     1,
		Data:           base64.StdEncoding.EncodeToString([]byte("hello")),
	})
	if err != nil {
		t.Fatalf("handle incoming fresh chunk: %v", err)
	}
	if !done {
		t.Fatal("expected single-chunk attachment to complete")
	}
	if _, ok := service.incomingAttachments.pending["alice:stale"]; ok {
		t.Fatal("expected stale transfer to be removed")
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
	aliceClient := wsclient.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", aliceService.Identity())
	defer aliceClient.Close()
	publishDirectoryEntry(t, server, aliceService.Identity())
	if err := aliceClient.Connect(ctx); err != nil {
		t.Fatalf("connect alice client: %v", err)
	}
	bobClient := wsclient.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", bobService.Identity())
	defer bobClient.Close()
	publishDirectoryEntry(t, server, bobService.Identity())
	if err := bobClient.Connect(ctx); err != nil {
		t.Fatalf("connect bob client: %v", err)
	}

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
	saved, err := aliceStore.ReadAttachment(aliceService.Identity(), attachments[0])
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
	if finalResult.Attachment == nil || finalResult.Attachment.Type != AttachmentTypeVoice {
		t.Fatalf("expected voice attachment metadata, got %+v", finalResult.Attachment)
	}
	attachmentPaths, err := filepath.Glob(filepath.Join(bobDir, "attachments", "alice", "*"))
	if err != nil {
		t.Fatalf("glob attachments: %v", err)
	}
	if len(attachmentPaths) != 1 {
		t.Fatalf("expected one stored attachment, got %v", attachmentPaths)
	}
	storedBytes, err := bobStore.ReadAttachment(bobService.Identity(), attachmentPaths[0])
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

func TestPrepareVoiceOutgoingAcceptsM4AExtension(t *testing.T) {
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
	if err := aliceStore.SaveContact(bobContact); err != nil {
		t.Fatalf("save bob contact: %v", err)
	}

	voicePath := filepath.Join(t.TempDir(), "voice memo.m4a")
	if err := os.WriteFile(voicePath, mustM4ABytes(), 0o600); err != nil {
		t.Fatalf("write m4a fixture: %v", err)
	}

	batch, displayBody, err := aliceService.PrepareVoiceOutgoing("bob", voicePath)
	if err != nil {
		t.Fatalf("prepare m4a voice outgoing: %v", err)
	}
	if batch == nil || len(batch.Envelopes) == 0 {
		t.Fatalf("expected outgoing envelopes, got %+v", batch)
	}
	if displayBody != "voice note sent: voice memo.m4a" {
		t.Fatalf("unexpected voice display body: %q", displayBody)
	}
}

func TestFileChunkRoundTripStoresAttachment(t *testing.T) {
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

	fileBytes := []byte("plain text file contents")
	filePath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(filePath, fileBytes, 0o600); err != nil {
		t.Fatalf("write file fixture: %v", err)
	}

	batch, displayBody, err := aliceService.PrepareFileOutgoing("bob", filePath)
	if err != nil {
		t.Fatalf("prepare file outgoing: %v", err)
	}
	if batch == nil || len(batch.Envelopes) < 2 {
		t.Fatalf("expected file batch with chunk envelopes, got %+v", batch)
	}
	if displayBody != "file sent: notes.txt" {
		t.Fatalf("unexpected file display body: %q", displayBody)
	}

	var finalResult *IncomingResult
	for i := range batch.Envelopes {
		envelope := batch.Envelopes[i]
		envelope.ID = fmt.Sprintf("file-env-%d", i)
		result, err := bobService.HandleIncoming(envelope)
		if err != nil {
			t.Fatalf("handle incoming envelope %d: %v", i, err)
		}
		if result != nil && !result.Control && result.Body != "" {
			finalResult = result
		}
	}
	if finalResult == nil {
		t.Fatal("expected final file result")
	}
	if !strings.Contains(finalResult.Body, "file received: notes.txt saved to ") {
		t.Fatalf("unexpected final body: %q", finalResult.Body)
	}
	if finalResult.Attachment == nil || finalResult.Attachment.Type != AttachmentTypeFile {
		t.Fatalf("expected file attachment metadata, got %+v", finalResult.Attachment)
	}
	attachmentPaths, err := filepath.Glob(filepath.Join(bobDir, "attachments", "alice", "*"))
	if err != nil {
		t.Fatalf("glob attachments: %v", err)
	}
	if len(attachmentPaths) != 1 {
		t.Fatalf("expected one stored attachment, got %v", attachmentPaths)
	}
	storedBytes, err := bobStore.ReadAttachment(bobService.Identity(), attachmentPaths[0])
	if err != nil {
		t.Fatalf("read stored attachment: %v", err)
	}
	if string(storedBytes) != string(fileBytes) {
		t.Fatal("stored attachment bytes did not match original file")
	}
	if len(finalResult.AckEnvelopes) != 0 {
		t.Fatalf("expected no delivery ack for file chunks, got %d", len(finalResult.AckEnvelopes))
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
	aliceClient := wsclient.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", aliceService.Identity())
	defer aliceClient.Close()
	publishDirectoryEntry(t, server, aliceService.Identity())
	if err := aliceClient.Connect(ctx); err != nil {
		t.Fatalf("connect alice client: %v", err)
	}
	bobClient := wsclient.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", bobService.Identity())
	defer bobClient.Close()
	publishDirectoryEntry(t, server, bobService.Identity())
	if err := bobClient.Connect(ctx); err != nil {
		t.Fatalf("connect bob client: %v", err)
	}

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

func TestHandleIncomingContactRequestFromUnknownSenderStoresPendingRequest(t *testing.T) {
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
	aliceEntry := mustSignedDirectoryEntry(t, aliceService.Identity(), true)
	bobEntry := mustSignedDirectoryEntry(t, bobService.Identity(), true)
	fakeDirectory := newFakeDirectoryClient(aliceEntry, bobEntry)
	bobService.SetDirectoryClient(fakeDirectory)

	envelopes, request, err := aliceService.ContactRequestEnvelopes(bobEntry, "hello bob")
	if err != nil {
		t.Fatalf("create contact request envelopes: %v", err)
	}
	if err := aliceStore.SaveContactRequest(request); err != nil {
		t.Fatalf("save outgoing request: %v", err)
	}
	for idx, envelope := range envelopes {
		envelope.ID = fmt.Sprintf("request-%d", idx)
		result, err := bobService.HandleIncoming(envelope)
		if err != nil {
			t.Fatalf("handle incoming request: %v", err)
		}
		if result == nil || result.ContactRequest == nil {
			t.Fatalf("expected contact request result, got %+v", result)
		}
	}
	stored, err := bobStore.LoadContactRequest("alice")
	if err != nil {
		t.Fatalf("load incoming request: %v", err)
	}
	if stored.Direction != store.ContactRequestDirectionIncoming || stored.Status != store.ContactRequestStatusPending || stored.Note != "hello bob" {
		t.Fatalf("unexpected stored incoming request: %+v", stored)
	}
	if _, err := bobStore.LoadContact("alice"); err != store.ErrNotFound {
		t.Fatalf("expected no contact import before accept, got %v", err)
	}
}

func TestHandleIncomingContactRequestResponseAcceptsAndImportsContact(t *testing.T) {
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
	aliceEntry := mustSignedDirectoryEntry(t, aliceService.Identity(), true)
	bobEntry := mustSignedDirectoryEntry(t, bobService.Identity(), true)
	fakeDirectory := newFakeDirectoryClient(aliceEntry, bobEntry)
	aliceService.SetDirectoryClient(fakeDirectory)
	bobService.SetDirectoryClient(fakeDirectory)

	requestEnvelopes, outgoingRequest, err := aliceService.ContactRequestEnvelopes(bobEntry, "")
	if err != nil {
		t.Fatalf("create outgoing request envelopes: %v", err)
	}
	if err := aliceStore.SaveContactRequest(outgoingRequest); err != nil {
		t.Fatalf("save outgoing request: %v", err)
	}
	for idx, envelope := range requestEnvelopes {
		envelope.ID = fmt.Sprintf("outgoing-request-%d", idx)
		if _, err := bobService.HandleIncoming(envelope); err != nil {
			t.Fatalf("bob handle incoming request: %v", err)
		}
	}
	incomingRequest, err := bobStore.LoadContactRequest("alice")
	if err != nil {
		t.Fatalf("load bob incoming request: %v", err)
	}
	responseEnvelopes, err := bobService.ContactRequestResponseEnvelopes(incomingRequest.Bundle, contactRequestDecisionAccept)
	if err != nil {
		t.Fatalf("create response envelopes: %v", err)
	}
	for idx, envelope := range responseEnvelopes {
		envelope.ID = fmt.Sprintf("accept-response-%d", idx)
		result, err := aliceService.HandleIncoming(envelope)
		if err != nil {
			t.Fatalf("alice handle incoming response: %v", err)
		}
		if result == nil || result.ContactRequest == nil || result.ContactRequest.Status != store.ContactRequestStatusAccepted {
			t.Fatalf("expected accepted contact request result, got %+v", result)
		}
	}
	contact, err := aliceStore.LoadContact("bob")
	if err != nil {
		t.Fatalf("load accepted contact: %v", err)
	}
	if contact.AccountID != "bob" || !contact.Verified || contact.TrustSource != identity.TrustSourceRelayDirectory {
		t.Fatalf("unexpected imported contact: %+v", contact)
	}
	updatedRequest, err := aliceStore.LoadContactRequest("bob")
	if err != nil {
		t.Fatalf("load updated outgoing request: %v", err)
	}
	if updatedRequest.Status != store.ContactRequestStatusAccepted {
		t.Fatalf("expected accepted request status, got %+v", updatedRequest)
	}
}

func TestHandleIncomingContactRequestResponseRejectsWithoutImportingContact(t *testing.T) {
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
	aliceEntry := mustSignedDirectoryEntry(t, aliceService.Identity(), true)
	bobEntry := mustSignedDirectoryEntry(t, bobService.Identity(), true)
	fakeDirectory := newFakeDirectoryClient(aliceEntry, bobEntry)
	aliceService.SetDirectoryClient(fakeDirectory)
	bobService.SetDirectoryClient(fakeDirectory)

	_, outgoingRequest, err := aliceService.ContactRequestEnvelopes(bobEntry, "")
	if err != nil {
		t.Fatalf("create outgoing request: %v", err)
	}
	if err := aliceStore.SaveContactRequest(outgoingRequest); err != nil {
		t.Fatalf("save outgoing request: %v", err)
	}
	responseEnvelopes, err := bobService.ContactRequestResponseEnvelopes(aliceEntry.Entry.Bundle, contactRequestDecisionReject)
	if err != nil {
		t.Fatalf("create reject response envelopes: %v", err)
	}
	for idx, envelope := range responseEnvelopes {
		envelope.ID = fmt.Sprintf("reject-response-%d", idx)
		result, err := aliceService.HandleIncoming(envelope)
		if err != nil {
			t.Fatalf("alice handle incoming reject response: %v", err)
		}
		if result == nil || result.ContactRequest == nil || result.ContactRequest.Status != store.ContactRequestStatusRejected {
			t.Fatalf("expected rejected contact request result, got %+v", result)
		}
	}
	if _, err := aliceStore.LoadContact("bob"); err != store.ErrNotFound {
		t.Fatalf("expected no contact import after rejection, got %v", err)
	}
}

func mustIdentity(t *testing.T, mailbox string) *identity.Identity {
	t.Helper()
	id, err := identity.New(mailbox)
	if err != nil {
		t.Fatalf("new identity %s: %v", mailbox, err)
	}
	return id
}

func mustContactFromIdentity(t *testing.T, id *identity.Identity) *identity.Contact {
	t.Helper()
	contact, err := identity.ContactFromInvite(id.InviteBundle())
	if err != nil {
		t.Fatalf("contact from invite: %v", err)
	}
	return contact
}

func cloneContact(contact *identity.Contact) *identity.Contact {
	if contact == nil {
		return nil
	}
	clone := &identity.Contact{
		AccountID:            contact.AccountID,
		AccountSigningPublic: append(ed25519.PublicKey(nil), contact.AccountSigningPublic...),
		Devices:              make([]identity.ContactDevice, 0, len(contact.Devices)),
		Verified:             contact.Verified,
		TrustSource:          contact.TrustSource,
	}
	for _, device := range contact.Devices {
		clone.Devices = append(clone.Devices, identity.ContactDevice{
			ID:               device.ID,
			Mailbox:          device.Mailbox,
			SigningPublic:    append(ed25519.PublicKey(nil), device.SigningPublic...),
			EncryptionPublic: append([]byte(nil), device.EncryptionPublic...),
			Revoked:          device.Revoked,
			RevokedAt:        device.RevokedAt,
		})
	}
	return clone
}

func publishDirectoryEntry(t *testing.T, server *httptest.Server, id *identity.Identity) {
	t.Helper()
	client, err := relayapi.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "")
	if err != nil {
		t.Fatalf("new relay api client: %v", err)
	}
	signed, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{Mailbox: id.AccountID, Bundle: id.InviteBundle(), Discoverable: true, PublishedAt: time.Now().UTC(), Version: time.Now().UTC().UnixNano()}, id.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	if _, err := client.PublishDirectoryEntry(*signed); err != nil {
		t.Fatalf("publish directory entry: %v", err)
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

func mustM4ABytes() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x18,
		'f', 't', 'y', 'p',
		'M', '4', 'A', ' ',
		0x00, 0x00, 0x00, 0x00,
		'M', '4', 'A', ' ',
		'i', 's', 'o', 'm',
	}
}

type testWriter struct{}

func (testWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
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

func mustSignedDirectoryEntry(t *testing.T, id *identity.Identity, discoverable bool) *relayapi.SignedDirectoryEntry {
	t.Helper()
	signed, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{Mailbox: id.AccountID, Bundle: id.InviteBundle(), Discoverable: discoverable, PublishedAt: time.Now().UTC(), Version: time.Now().UTC().UnixNano()}, id.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	return signed
}
