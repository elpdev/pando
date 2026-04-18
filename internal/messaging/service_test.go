package messaging

import (
	"testing"

	"github.com/elpdev/chatui/internal/identity"
	"github.com/elpdev/chatui/internal/store"
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

	envelopes, err := aliceService.EncryptOutgoing("bob", "hello after update")
	if err != nil {
		t.Fatalf("encrypt outgoing: %v", err)
	}
	if len(envelopes) == 0 || envelopes[0].BodyEncoding != BodyEncodingContactUpdate {
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

	result, err := bobService.HandleIncoming(envelopes[0])
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
	envelopes, err := aliceService.EncryptOutgoing("bob", "hello bob")
	if err != nil {
		t.Fatalf("encrypt outgoing: %v", err)
	}
	chatEnvelope := envelopes[len(envelopes)-1]
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
